package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// 更新分发（ADR-0015）：把服务端一个目录里的 release.json 与 app.apk 只读地暴露出去。
// 发版动作就是把这两个文件拷上去，这里不写盘、不解包、不校验签名。
// 两条路由都挂在口令后面，APK 不是谁都能拉走的。

const (
	releaseManifestName = "release.json"
	releaseAPKName      = "app.apk"
	apkContentType      = "application/vnd.android.package-archive"
)

// releaseInfo 是给客户端比对用的版本元数据：version_code 是唯一的判据，
// version_name 与 notes 只给人看，size 让客户端在下载前先说一句"约多少 MB"。
type releaseInfo struct {
	VersionCode int    `json:"version_code"`
	VersionName string `json:"version_name"`
	Notes       string `json:"notes,omitempty"`
	Size        int64  `json:"size"`
}

// errNoRelease 表示"没配目录或文件还没拷上去"，对外回 404，客户端当成没有新版本，
// 静默不提示。
var errNoRelease = errors.New("没有已发布的版本")

func (s *Server) getReleaseInfo(w http.ResponseWriter, r *http.Request) {
	info, err := s.readRelease()
	switch {
	case errors.Is(err, errNoRelease):
		s.fail(w, http.StatusNotFound, "not_released", "这个服务端还没发布过更新")
	case err != nil:
		s.log.Error("读更新目录失败", "err", err.Error(), "path", r.URL.Path)
		s.fail(w, http.StatusInternalServerError, "release_bad", "服务端更新目录读不出内容")
	default:
		s.ok(w, info)
	}
}

// downloadRelease 流式回 APK 本体。ServeContent 顺带给出 Range、Last-Modified
// 与 Content-Length——手机上刷到一半断了能续，不用重拉十兆。
func (s *Server) downloadRelease(w http.ResponseWriter, r *http.Request) {
	if s.cfg.ReleaseDir == "" {
		s.fail(w, http.StatusNotFound, "not_released", "这个服务端还没发布过更新")
		return
	}

	f, err := os.Open(filepath.Join(s.cfg.ReleaseDir, releaseAPKName))
	if errors.Is(err, os.ErrNotExist) {
		s.fail(w, http.StatusNotFound, "not_released", "还没有上传的 APK")
		return
	}
	if err != nil {
		s.serverError(w, r, "读 APK", err)
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		s.serverError(w, r, "读 APK 属性", err)
		return
	}
	w.Header().Set("Content-Type", apkContentType)
	http.ServeContent(w, r, releaseAPKName, fi.ModTime(), f)
}

// 两个文件名都是常量拼出来的，路径不来自请求，没有穿越的口子。
func (s *Server) readRelease() (*releaseInfo, error) {
	if s.cfg.ReleaseDir == "" {
		return nil, errNoRelease
	}

	raw, err := os.ReadFile(filepath.Join(s.cfg.ReleaseDir, releaseManifestName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, errNoRelease
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", releaseManifestName, err)
	}

	var info releaseInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", releaseManifestName, err)
	}
	if info.VersionCode <= 0 {
		return nil, fmt.Errorf("%s: version_code 要正整数，实得 %d", releaseManifestName, info.VersionCode)
	}

	fi, err := os.Stat(filepath.Join(s.cfg.ReleaseDir, releaseAPKName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s 在但 %s 不在", releaseManifestName, releaseAPKName)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", releaseAPKName, err)
	}
	info.Size = fi.Size()
	return &info, nil
}
