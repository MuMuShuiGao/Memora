package report

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Completer 是"发一段提示词，拿回一段纯文本"这件事的抽象。报表逻辑只认这个接口，
// 所以测试里换一个假实现就够了，不必真打外部模型（ADR-0003：生成只在服务端）。
type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// ErrNotConfigured 表示 base URL / model / key 三项没配齐。
// 报表切片之前这三项是可选的（design §3），所以缺了要能说清缺在哪，而不是含糊失败。
var ErrNotConfigured = fmt.Errorf("report: 模型未配置，需要 MEMORA_LLM_BASE_URL、MEMORA_LLM_MODEL、MEMORA_LLM_API_KEY")

// openAICompleter 打一个兼容 OpenAI 的对话补全接口。DeepSeek 用的就是这个形状。
type openAICompleter struct {
	baseURL string
	model   string
	apiKey  string
	http    *http.Client
}

// 单次调用的默认上限。design §6 写的是 60s——一次完整周报的生成本来就比聊天慢。
const defaultCallTimeout = 60 * time.Second

func newOpenAICompleter(baseURL, model, apiKey string, timeout time.Duration) *openAICompleter {
	if timeout <= 0 {
		timeout = defaultCallTimeout
	}
	return &openAICompleter{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: timeout},
	}
}

// NewCompleter 供 main 在配置齐全时构造真客户端。
func NewCompleter(baseURL, model, apiKey string) (Completer, error) {
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(model) == "" || strings.TrimSpace(apiKey) == "" {
		return nil, ErrNotConfigured
	}
	return newOpenAICompleter(baseURL, model, apiKey, defaultCallTimeout), nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	// 报表要的是稳定归纳，不是创意发挥；温度压低，长度给足。
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
	Stream      bool    `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
		Content string      // 少数兼容实现把正文平铺在这里
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (c *openAICompleter) Complete(ctx context.Context, system, user string) (string, error) {
	payload, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0.2,
		MaxTokens:   4000,
	})
	if err != nil {
		return "", fmt.Errorf("report: 组装请求: %w", err)
	}

	url := c.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("report: 构造请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	res, err := c.http.Do(req)
	if err != nil {
		// err 里可能带 URL，但不带 key；日志可以放心打。
		return "", fmt.Errorf("report: 调 %s: %w", url, err)
	}
	defer res.Body.Close()

	// 模型返回上限给个硬闸：正常一份周报几 KB，超过 1MB 一定是出了问题。
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("report: 读响应: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("report: 模型接口 HTTP %d: %s", res.StatusCode, snippet(raw))
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("report: 解析响应: %w", err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("report: 模型返回错误 %s: %s", parsed.Error.Type, parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("report: 模型没返回任何候选")
	}
	text := parsed.Choices[0].Message.Content
	if strings.TrimSpace(text) == "" {
		text = parsed.Choices[0].Content
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("report: 模型返回空正文")
	}
	return strings.TrimSpace(text), nil
}

// snippet 只取响应体前 200 字，避免把整段模型输出灌进日志和 fail_reason。
func snippet(b []byte) string {
	s := strings.ReplaceAll(string(b), "\n", " ")
	if r := []rune(s); len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}
