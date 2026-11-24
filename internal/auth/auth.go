// Package auth 只做一件事：校验访问口令，并对失败者限速。
//
// 服务端只存口令的 bcrypt 哈希（ADR-0009 连带决定）。这台机器实测正在被公网
// 持续扫描，而这个口令是整个服务的唯一防线（ADR-0002），所以限速不是可选加固，
// 是让"暴力猜口令"在经济上不成立的手段。
package auth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	maxFailures   = 5           // 连续失败到达这个数就开始拒绝
	baseCooldown  = time.Minute // 退避起点，之后按 2 的幂增长
	maxCooldown   = time.Hour   // 封顶，避免把恢复期变成永久自锁
	trackerExpiry = time.Hour   // 这么久没动静的来源，记录可以扔掉
	maxTrackers   = 4096        // 表大小上限，防扫描器把内存吃掉
)

// ErrLocked 表示该来源正处于退避期。
var ErrLocked = errors.New("auth: 失败过多，暂时拒绝")

type tracker struct {
	failures int
	until    time.Time
	lastSeen time.Time
}

type Gate struct {
	hash  []byte
	dummy []byte // 让"没带头"和"头不对"花同样的时间，别用耗时差异泄露信息

	mu       sync.Mutex
	trackers map[string]*tracker
	now      func() time.Time
}

func NewGate(passhash string) (*Gate, error) {
	if err := bcrypt.CompareHashAndPassword([]byte(passhash), []byte("")); err == nil {
		return nil, errors.New("auth: 口令哈希居然是空口令，拒绝启动")
	}
	// 校验存的哈希本身格式合法，否则永远比不中，表现是"口令怎么都不对"
	if _, err := bcrypt.Cost([]byte(passhash)); err != nil {
		return nil, fmt.Errorf("auth: MEMORA_PASS_HASH 不是合法的 bcrypt 哈希: %w", err)
	}

	dummy, err := bcrypt.GenerateFromPassword([]byte(randomSecret()), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("auth: 初始化: %w", err)
	}
	return &Gate{
		hash:     []byte(passhash),
		dummy:    dummy,
		trackers: map[string]*tracker{},
		now:      time.Now,
	}, nil
}

func randomSecret() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("auth: 读不到随机源: " + err.Error())
	}
	return fmt.Sprintf("%x", b)
}

// Verify 比对口令。bcrypt 本身是定长工作量，不需要再手工做常量时间比较。
func (g *Gate) Verify(key string) bool {
	target := g.hash
	if key == "" {
		target = g.dummy // 空口令也烧掉同样的 CPU，不给计时侧信道留口子
	}
	return bcrypt.CompareHashAndPassword(target, []byte(key)) == nil
}

// Allow 判断某来源现在能不能继续尝试认证。返回 false 时第二个值是建议的
// Retry-After 秒数。
func (g *Gate) Allow(remote string) (bool, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	tr, ok := g.trackers[remote]
	if !ok {
		return true, 0
	}
	if wait := tr.until.Sub(g.now()); wait > 0 {
		return false, int(wait.Seconds()) + 1
	}
	return true, 0
}

func (g *Gate) RecordFailure(remote string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pruneLocked()
	tr := g.trackers[remote]
	if tr == nil {
		tr = &tracker{}
		g.trackers[remote] = tr
	}
	tr.lastSeen = g.now()
	tr.failures++
	if tr.failures >= maxFailures {
		exp := tr.failures - maxFailures
		cooldown := baseCooldown << min(exp, 6)
		if cooldown > maxCooldown {
			cooldown = maxCooldown
		}
		tr.until = g.now().Add(cooldown)
	}
}

func (g *Gate) RecordSuccess(remote string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.trackers, remote)
}

// pruneLocked 顺手清掉长期不活跃的来源。扫描器会不断换来源，不清就会一直涨。
func (g *Gate) pruneLocked() {
	if len(g.trackers) < maxTrackers {
		return
	}
	cutoff := g.now().Add(-trackerExpiry)
	for k, tr := range g.trackers {
		if tr.lastSeen.Before(cutoff) && tr.until.Before(cutoff) {
			delete(g.trackers, k)
		}
	}
}

// Middleware 把口令校验挂在任意 handler 外面。
//
// 注意：来源地址取的是 TCP 对端。当前是客户端直连 TLS（design §3），所以这个
// 地址就是手机地址；将来若在前面挂了反向代理，必须改成取可信代理转发的头，
// 否则限速会对着一台代理机生效，把所有用户一起锁掉。
func (g *Gate) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remote := hostOf(r)
		if ok, retryAfter := g.Allow(remote); !ok {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			writeJSONError(w, http.StatusTooManyRequests, "rate_limited", "尝试过多")
			return
		}
		if !g.Verify(r.Header.Get("X-Access-Key")) {
			g.RecordFailure(remote)
			// 401 且不返回任何业务数据：这是 FR-07 的验收线
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "口令不对")
			return
		}
		g.RecordSuccess(remote)
		next.ServeHTTP(w, r)
	})
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":{"code":%q,"message":%q}}`, code, message)
}

func hostOf(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
