// Package api 是 HTTP 边界：路由、TLS、统一的错误形状。业务规则不在这里。
package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"memora/internal/auth"
	"memora/internal/config"
	"memora/internal/report"
	"memora/internal/store"
)

type Server struct {
	st   *store.Store
	cfg  *config.Config
	gate *auth.Gate
	gen  *report.Generator
	log  *slog.Logger
	// now 让接口测试能把"现在"钉住；生产用 time.Now。
	now func() time.Time
}

func New(st *store.Store, cfg *config.Config, gate *auth.Gate, gen *report.Generator, log *slog.Logger) *Server {
	return &Server{st: st, cfg: cfg, gate: gate, gen: gen, log: log, now: time.Now}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// 活着探针不带任何业务数据，给 systemd 和本机冒烟测试用，所以不挂口令。
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		s.ok(w, map[string]string{"status": "ok"})
	})

	// 其余一律要口令（FR-07）。Go 1.22 起 ServeMux 支持方法与通配段，
	// 所以这里不需要额外引一个路由框架。
	for _, p := range []struct {
		pattern string
		h       http.HandlerFunc
	}{
		{"GET /v1/notes", s.listNotes},
		{"POST /v1/notes", s.createNote},
		{"GET /v1/notes/{id}", s.getNote},
		{"PATCH /v1/notes/{id}", s.updateNote},
		{"DELETE /v1/notes/{id}", s.deleteNote},
		{"GET /v1/periods/now", s.periodsNow},
		{"GET /v1/reports", s.listReports},
		{"GET /v1/reports/{kind}/{period_start}", s.getReport},
		{"POST /v1/reports/{kind}/{period_start}/generate", s.generateReport},
	} {
		mux.Handle(p.pattern, s.gate.Middleware(p.h))
	}

	return s.withAccessLog(mux)
}

// Run 起 HTTPS 服务直到 ctx 取消，然后优雅收尾。
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:    s.cfg.ListenAddr,
		Handler: s.Handler(),
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		// 公网端口上必须有的几个超时，否则一条挂住的连接就能占住一个 fd。
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		s.log.Info("监听 HTTPS", "addr", s.cfg.ListenAddr)
		err := srv.ListenAndServeTLS(s.cfg.TLSCert, s.cfg.TLSKey)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.log.Info("收到退出信号，关闭中")
		return srv.Shutdown(shutdownCtx)
	}
}

func (s *Server) withAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		// 只记方法、路径、状态、耗时、来源地址。正文和 X-Access-Key 一律不落日志
		// ——ADR-0005 明确记录里允许出现敏感原文，日志不是例外。
		s.log.Info("请求",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"took", time.Since(start).Round(time.Millisecond).String(),
			"remote", remoteHost(r),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func remoteHost(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

type errorPayload struct {
	Error errorObj `json:"error"`
}

type errorObj struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (s *Server) ok(w http.ResponseWriter, v any) {
	s.write(w, http.StatusOK, v)
}

func (s *Server) fail(w http.ResponseWriter, status int, code, message string) {
	s.write(w, status, errorPayload{Error: errorObj{Code: code, Message: message}})
}

func (s *Server) write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// 头已经发出去了，此时编码失败只能记日志，没法再改状态码
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.log.Error("写响应失败", "err", err.Error())
	}
}
