// Package config 加载服务端配置。
//
// 来源优先级：环境变量覆盖文件。systemd 用 EnvironmentFile 注入，本机调试用
// -config 指一个 KEY=VALUE 文件。刻意不给敏感项设默认值——缺了就启动失败，
// 不允许带着半套配置蒙跑（design §3）。
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr     string
	DBPath         string
	TLSCert        string
	TLSKey         string
	PassphraseHash string

	LocalTZ     *time.Location
	LocalTZName string

	MaxNoteChars int

	// 以下三项在报表切片落地前不参与校验，缺了不影响记录读写。
	DeepSeekBaseURL string
	DeepSeekModel   string
	DeepSeekAPIKey  string

	// 两个定时时刻仍未定（见 NOW 待确认），先按原样收下，等调度代码来解析。
	WeeklyAt  string
	MonthlyAt string
}

// Load 读取配置。path 为空则只从环境变量取。
func Load(path string) (*Config, error) {
	values := map[string]string{}
	if path != "" {
		fileValues, err := readEnvFile(path)
		if err != nil {
			return nil, fmt.Errorf("config: 读 %s: %w", path, err)
		}
		values = fileValues
	}
	// 环境变量覆盖文件内容
	for k := range envKeys {
		if v, ok := os.LookupEnv(k); ok {
			values[k] = v
		}
	}

	c := &Config{}
	var missing []string
	for key, dst := range envKeys {
		v := strings.TrimSpace(values[key])
		if v == "" {
			if dst.required {
				missing = append(missing, key)
			}
			continue
		}
		if err := dst.set(c, v); err != nil {
			return nil, fmt.Errorf("config: %s: %w", key, err)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("config: 缺少必需配置项 %s", strings.Join(sortedKeys(missing), "、"))
	}

	if c.MaxNoteChars == 0 {
		c.MaxNoteChars = 2000
	}
	return c, nil
}

type binding struct {
	required bool
	set      func(*Config, string) error
}

var envKeys = map[string]binding{
	"MEMORA_LISTEN_ADDR": {true, func(c *Config, v string) error { c.ListenAddr = v; return nil }},
	"MEMORA_DB_PATH":     {true, func(c *Config, v string) error { c.DBPath = v; return nil }},
	"MEMORA_TLS_CERT":    {true, func(c *Config, v string) error { c.TLSCert = v; return nil }},
	"MEMORA_TLS_KEY":     {true, func(c *Config, v string) error { c.TLSKey = v; return nil }},
	"MEMORA_PASS_HASH":   {true, func(c *Config, v string) error { c.PassphraseHash = v; return nil }},
	"MEMORA_LOCAL_TZ":    {true, func(c *Config, v string) error { return c.setTZ(v) }},
	"MEMORA_MAX_NOTE_CHARS": {false, func(c *Config, v string) error {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return fmt.Errorf("要正整数，实得 %q", v)
		}
		c.MaxNoteChars = n
		return nil
	}},
	"MEMORA_LLM_BASE_URL": {false, func(c *Config, v string) error { c.DeepSeekBaseURL = v; return nil }},
	"MEMORA_LLM_MODEL":    {false, func(c *Config, v string) error { c.DeepSeekModel = v; return nil }},
	"MEMORA_LLM_API_KEY":  {false, func(c *Config, v string) error { c.DeepSeekAPIKey = v; return nil }},
	"MEMORA_WEEKLY_AT":    {false, func(c *Config, v string) error { c.WeeklyAt = v; return nil }},
	"MEMORA_MONTHLY_AT":   {false, func(c *Config, v string) error { c.MonthlyAt = v; return nil }},
}

// setTZ 接受 "+08:00"、"UTC+8"、"UTC-5" 这类固定偏移，也接受 IANA 名字。
// 支持固定偏移是为了让服务不依赖宿主机装了 tzdata。
func (c *Config) setTZ(v string) error {
	c.LocalTZName = v
	if loc, err := time.LoadLocation(v); err == nil && (v == "UTC" || loc.String() == v) {
		c.LocalTZ = loc
		return nil
	}
	normalized := strings.ToUpper(strings.TrimSpace(v))
	normalized = strings.TrimPrefix(normalized, "UTC")
	if normalized == "" {
		c.LocalTZ = time.UTC
		return nil
	}
	if !strings.Contains(normalized, ":") {
		// "UTC+8" 这种小时写法
		hours, err := strconv.Atoi(normalized)
		if err != nil {
			return fmt.Errorf("无法解析时区 %q", v)
		}
		c.LocalTZ = time.FixedZone(v, hours*3600)
		return nil
	}
	parsed, err := time.Parse("-07:00", normalized)
	if err != nil {
		return fmt.Errorf("无法解析时区 %q: %w", v, err)
	}
	_, offset := parsed.Zone()
	c.LocalTZ = time.FixedZone(v, offset)
	return nil
}

func readEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for lineNo := 1; sc.Scan(); lineNo++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("第 %d 行不是 KEY=VALUE", lineNo)
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		out[key] = val
	}
	return out, sc.Err()
}

func sortedKeys(keys []string) []string {
	out := append([]string(nil), keys...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
