package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 更新分发（ADR-0015）。发版动作是"往一个目录里拷两个文件"，所以这里钉的是
// 那个目录的各种残缺状态各回什么——真机更新时最容易撞上的就是这些。

func writeReleaseDir(t *testing.T, manifest string, apk []byte) string {
	t.Helper()
	dir := t.TempDir()
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(dir, releaseManifestName), []byte(manifest), 0o600); err != nil {
			t.Fatalf("写 %s: %v", releaseManifestName, err)
		}
	}
	if apk != nil {
		if err := os.WriteFile(filepath.Join(dir, releaseAPKName), apk, 0o600); err != nil {
			t.Fatalf("写 %s: %v", releaseAPKName, err)
		}
	}
	return dir
}

func TestReleaseRequiresPassphrase(t *testing.T) {
	h := newHarness(t)
	h.api.cfg.ReleaseDir = writeReleaseDir(t, `{"version_code":2,"version_name":"0.2"}`, []byte("x"))

	for _, path := range []string{"/v1/app/release", "/v1/app/release/file"} {
		res, body := h.do(t, "GET", path, "", false)
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s 无口令 = %d %s，要 401", path, res.StatusCode, body)
		}
	}
}

// 没配目录、目录里没有文件，都是 404 not_released：客户端把这当成"没有可更新的版本"，
// 静默不提示，所以这个码不能换成别的。
func TestReleaseWithoutPublishIsNotFound(t *testing.T) {
	h := newHarness(t)

	res, body := h.do(t, "GET", "/v1/app/release", "", true)
	if res.StatusCode != http.StatusNotFound || !strings.Contains(body, `"not_released"`) {
		t.Errorf("未配目录 = %d %s", res.StatusCode, body)
	}

	h.api.cfg.ReleaseDir = writeReleaseDir(t, "", nil)
	res, body = h.do(t, "GET", "/v1/app/release", "", true)
	if res.StatusCode != http.StatusNotFound || !strings.Contains(body, `"not_released"`) {
		t.Errorf("空目录 = %d %s", res.StatusCode, body)
	}
	res, body = h.do(t, "GET", "/v1/app/release/file", "", true)
	if res.StatusCode != http.StatusNotFound || !strings.Contains(body, `"not_released"`) {
		t.Errorf("没 APK = %d %s", res.StatusCode, body)
	}
}

// 只有 release.json 没有 APK 是"发了一半"，回 500 让部署日志留下痕迹；
// 客户端只看 200，所以这个分支不会打扰使用者。
func TestReleaseManifestWithoutApkIsBad(t *testing.T) {
	h := newHarness(t)
	h.api.cfg.ReleaseDir = writeReleaseDir(t, `{"version_code":2,"version_name":"0.2"}`, nil)

	res, body := h.do(t, "GET", "/v1/app/release", "", true)
	if res.StatusCode != http.StatusInternalServerError || !strings.Contains(body, `"release_bad"`) {
		t.Errorf("半发布 = %d %s", res.StatusCode, body)
	}

	// version_code 缺失或为 0：客户端拿它做唯一比较，绝不能回 200。
	h.api.cfg.ReleaseDir = writeReleaseDir(t, `{"version_name":"0.2"}`, []byte("x"))
	res, body = h.do(t, "GET", "/v1/app/release", "", true)
	if res.StatusCode != http.StatusInternalServerError || !strings.Contains(body, `"release_bad"`) {
		t.Errorf("没有 version_code = %d %s", res.StatusCode, body)
	}
}

func TestReleaseInfoAndDownload(t *testing.T) {
	h := newHarness(t)
	h.api.cfg.ReleaseDir = writeReleaseDir(t,
		`{"version_code":7,"version_name":"0.7","notes":"待办能拖了"}`,
		[]byte("APKBYTES-0123456789"))

	res, body := h.do(t, "GET", "/v1/app/release", "", true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("元数据 = %d %s", res.StatusCode, body)
	}
	var info struct {
		VersionCode int    `json:"version_code"`
		VersionName string `json:"version_name"`
		Notes       string `json:"notes"`
		Size        int64  `json:"size"`
	}
	if err := json.Unmarshal([]byte(body), &info); err != nil {
		t.Fatalf("解析元数据 %s: %v", body, err)
	}
	// size 是 stat 出来的，不是发布时手填的：19 = len("APKBYTES-0123456789")
	if info.VersionCode != 7 || info.VersionName != "0.7" || info.Notes != "待办能拖了" || info.Size != 19 {
		t.Errorf("元数据 = %+v", info)
	}

	res, body = h.do(t, "GET", "/v1/app/release/file", "", true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("下载 = %d %s", res.StatusCode, body)
	}
	if got := res.Header.Get("Content-Type"); got != apkContentType {
		t.Errorf("Content-Type = %q", got)
	}
	if got := res.Header.Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("Accept-Ranges = %q，断点续传靠它", got)
	}
	if body != "APKBYTES-0123456789" {
		t.Errorf("正文 = %q", body)
	}
}

// 慢速拉包必须能拉到尾。HTTP/2 会把全局 WriteTimeout 当成单条流的写期限，
// 真机上十兆的包走蜂窝网整一分钟就被 RST_STREAM(INTERNAL_ERROR) 掐断（OkHttp
// 报 "stream was reset: INTERNAL_ERROR"）。这里把写超时压到 300ms、客户端开头
// 一秒只读六十来 KB，让服务端在流控窗口满后卡住，复现当时的处境。
func TestReleaseDownloadSurvivesSlowClient(t *testing.T) {
	h := newHarness(t)
	// 客户端默认给单条流 4MB 窗口，包得比它大，服务端才会真卡在写上。
	payload := bytes.Repeat([]byte("x"), 5<<20)
	h.api.cfg.ReleaseDir = writeReleaseDir(t, `{"version_code":2}`, payload)

	ts := httptest.NewUnstartedServer(h.api.Handler())
	ts.EnableHTTP2 = true
	ts.Config.WriteTimeout = 300 * time.Millisecond
	ts.StartTLS()
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL+"/v1/app/release/file", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Access-Key", testPass)
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close()
	if res.ProtoMajor != 2 {
		t.Fatalf("这条要走在 HTTP/2 上才测得到，实得 %s", res.Proto)
	}

	buf := make([]byte, 2048)
	got := 0
	start := time.Now()
	for {
		n, rerr := res.Body.Read(buf)
		got += n
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			t.Fatalf("读了 %d 字节后被掐断: %v", got, rerr)
		}
		if time.Since(start) < time.Second {
			time.Sleep(30 * time.Millisecond)
		}
	}
	if got != len(payload) {
		t.Errorf("收到 %d 字节，要 %d", got, len(payload))
	}
}

// 手机上下载断了要能续，所以 Range 必须真生效——这一条是整包里唯一验证它的地方。
func TestReleaseDownloadSupportsRange(t *testing.T) {
	h := newHarness(t)
	h.api.cfg.ReleaseDir = writeReleaseDir(t, `{"version_code":2}`, []byte("APKBYTES-0123456789"))

	req, err := http.NewRequest("GET", h.srv.URL+"/v1/app/release/file", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Access-Key", testPass)
	req.Header.Set("Range", "bytes=10-14")
	res, err := h.http.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)

	if res.StatusCode != http.StatusPartialContent {
		t.Errorf("Range = %d %s，要 206", res.StatusCode, raw)
	}
	if string(raw) != "12345" {
		t.Errorf("分段正文 = %q，要 12345", raw)
	}
	if got := res.Header.Get("Content-Range"); !strings.HasPrefix(got, "bytes 10-14/19") {
		t.Errorf("Content-Range = %q", got)
	}
}
