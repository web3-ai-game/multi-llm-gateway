// Package oai 是通用 OpenAI 兼容 chat completions 客户端。
// OpenRouter 与 sub2api 都是 OpenAI 兼容，只是 base URL / 附加 header 不同，
// 用同一套代码参数化即可，为将来接更多同类中转留口子。
package oai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"deepweay-chat/internal/provider"
)

// Client 实现 provider.Provider。
type Client struct {
	name         string
	baseURL      string // 形如 https://openrouter.ai/api/v1
	apiKey       string
	extraHeaders map[string]string
	httpClient   *http.Client
}

// New 构建通用 OpenAI 兼容客户端。baseURL 不含末尾 /chat/completions。
func New(name, baseURL, apiKey string, extraHeaders map[string]string) *Client {
	return &Client{
		name:         name,
		baseURL:      strings.TrimRight(baseURL, "/"),
		apiKey:       apiKey,
		extraHeaders: extraHeaders,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

// NewOpenRouter 是 OpenRouter 的便捷构造（带 attribution header）。
func NewOpenRouter(apiKey string) *Client {
	return New("openrouter", "https://openrouter.ai/api/v1", apiKey, map[string]string{
		"HTTP-Referer": "https://chat.example.com",
		"X-Title":      "Deepweay AI",
	})
}

func (c *Client) Name() string { return c.name }

func (c *Client) url() string { return c.baseURL + "/chat/completions" }

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	for k, v := range c.extraHeaders {
		req.Header.Set(k, v)
	}
}

type reasoningOpt struct {
	Effort string `json:"effort,omitempty"`
}

type pluginOpt struct {
	ID string `json:"id"`
}

type oaiMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func buildOAIMessages(msgs []provider.Message) []oaiMessage {
	out := make([]oaiMessage, 0, len(msgs))
	for _, m := range msgs {
		if len(m.Images) == 0 {
			out = append(out, oaiMessage{Role: m.Role, Content: m.Content})
			continue
		}
		parts := make([]provider.ContentPart, 0, len(m.Images)+1)
		if m.Content != "" {
			parts = append(parts, provider.ContentPart{Type: "text", Text: m.Content})
		}
		for _, img := range m.Images {
			parts = append(parts, provider.ContentPart{
				Type:     "image_url",
				ImageURL: &provider.ImageURL{URL: img},
			})
		}
		out = append(out, oaiMessage{Role: m.Role, Content: parts})
	}
	return out
}

type reqBody struct {
	Model            string         `json:"model"`
	Messages         []oaiMessage   `json:"messages"`
	MaxTokens        *int           `json:"max_tokens,omitempty"`
	Temperature      *float64       `json:"temperature,omitempty"`
	Stream           bool           `json:"stream,omitempty"`
	Usage            *usageOpt      `json:"usage,omitempty"`
	StreamOpts       *streamOptions `json:"stream_options,omitempty"`
	Reasoning        *reasoningOpt  `json:"reasoning,omitempty"`
	IncludeReasoning *bool          `json:"include_reasoning,omitempty"`
	ReasoningEffort  string         `json:"reasoning_effort,omitempty"`
	Plugins          []pluginOpt    `json:"plugins,omitempty"`
}

func (c *Client) blob() string {
	return strings.ToLower(c.baseURL + " " + c.name)
}

func (c *Client) isOpenRouter() bool {
	return c.name == "openrouter" || strings.Contains(c.blob(), "openrouter")
}

func (c *Client) isXAI() bool {
	b := c.blob()
	return strings.Contains(b, "api.x.ai") || strings.Contains(b, "xai") || strings.Contains(b, "grok")
}

func applyProviderExtras(c *Client, body *reqBody, r provider.ChatRequest) {
	if c.isOpenRouter() {
		if r.IncludeUsage {
			body.Usage = &usageOpt{Include: true}
			if body.Stream {
				body.StreamOpts = &streamOptions{IncludeUsage: true}
			}
		}
		if r.Thinking {
			t := true
			body.IncludeReasoning = &t
			body.Reasoning = &reasoningOpt{Effort: "high"}
		}
		if r.WebSearch {
			body.Plugins = []pluginOpt{{ID: "web"}}
		}
		return
	}
	if r.IncludeUsage && body.Stream {
		body.StreamOpts = &streamOptions{IncludeUsage: true}
	}
	if r.Thinking && c.isXAI() {
		body.ReasoningEffort = "high"
	}
}

type usageOpt struct {
	Include bool `json:"include"`
}
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}
type usagePayload struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func friendlyErr(status int, raw []byte) error {
	rawStr := string(raw)
	if strings.Contains(rawStr, "prepayment credits are depleted") || strings.Contains(rawStr, "credits are depleted") {
		return fmt.Errorf("Google AI Studio 预付费额度已耗尽 (Prepayment credits depleted)，请至 ai.studio 充值或管理结算")
	}
	if strings.Contains(rawStr, "quota") || strings.Contains(rawStr, "Quota") {
		return fmt.Errorf("上游 API 额度超限 (Quota Exceeded)，请检查账户额度或更换 API Key")
	}
	switch status {
	case http.StatusUnauthorized: // 401
		return fmt.Errorf("上游认证失败（API Key 未配置或无效，请检查配置）")
	case http.StatusPaymentRequired: // 402
		return fmt.Errorf("上游余额不足，请充值后再试")
	case http.StatusNotFound: // 404
		return fmt.Errorf("上游端点或模型未找到（404 Not Found）")
	case http.StatusUnprocessableEntity: // 422
		return fmt.Errorf("上游参数无法处理（422 Unprocessable Entity: %s）", truncate(rawStr, 120))
	case http.StatusTooManyRequests: // 429
		return fmt.Errorf("请求过于频繁或上游额度耗尽（429 Too Many Requests），请稍后再试")
	case http.StatusBadRequest: // 400
		return fmt.Errorf("模型请求无效（%s）", truncate(rawStr, 150))
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout: // 500, 502, 503, 504
		return fmt.Errorf("上游服务暂时不可用（HTTP %d: %s）", status, truncate(rawStr, 150))
	default:
		return fmt.Errorf("上游返回 %d: %s", status, truncate(rawStr, 200))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func (c *Client) ChatCompletion(ctx context.Context, r provider.ChatRequest) (*provider.ChatResult, error) {
	body := reqBody{Model: r.Model, Messages: buildOAIMessages(r.Messages), MaxTokens: r.MaxTokens, Temperature: r.Temperature}
	applyProviderExtras(c, &body, r)
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(), bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	c.setHeaders(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, friendlyErr(resp.StatusCode, raw)
	}
	var cr struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				Reasoning        string `json:"reasoning"`
			} `json:"message"`
		} `json:"choices"`
		Usage *usagePayload `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &cr); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if cr.Error != nil {
		return nil, fmt.Errorf("%s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return nil, fmt.Errorf("empty response from model")
	}
	content := cr.Choices[0].Message.Content
	reasoning := cr.Choices[0].Message.ReasoningContent
	if reasoning == "" {
		reasoning = cr.Choices[0].Message.Reasoning
	}
	if reasoning != "" && !strings.Contains(content, "<think>") {
		if content != "" {
			content = "<think>\n" + reasoning + "\n</think>\n\n" + content
		} else {
			content = "<think>\n" + reasoning + "\n</think>"
		}
	}
	if strings.TrimSpace(content) == "" && strings.TrimSpace(reasoning) == "" {
		return nil, fmt.Errorf("empty response from model")
	}
	res := &provider.ChatResult{Content: content}
	if cr.Usage != nil {
		res.Usage = &provider.Usage{PromptTokens: cr.Usage.PromptTokens, CompletionTokens: cr.Usage.CompletionTokens}
	}
	return res, nil
}

func (c *Client) StreamChatCompletion(ctx context.Context, r provider.ChatRequest, onDelta func(rawSSEData string)) (*provider.ChatResult, error) {
	body := reqBody{Model: r.Model, Messages: buildOAIMessages(r.Messages), MaxTokens: r.MaxTokens, Temperature: r.Temperature, Stream: true}
	applyProviderExtras(c, &body, r)
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal stream request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(), bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("build stream request: %w", err)
	}
	c.setHeaders(req)
	streamTransport := &http.Transport{
		ResponseHeaderTimeout: 8 * time.Second,
	}
	streamClient := &http.Client{Transport: streamTransport, Timeout: 0}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stream http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, friendlyErr(resp.StatusCode, raw)
	}

	var full strings.Builder
	var reasoningFull strings.Builder
	var usage *provider.Usage
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					Reasoning        string `json:"reasoning"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *usagePayload `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err == nil {
			if len(chunk.Choices) > 0 {
				d := chunk.Choices[0].Delta
				if d.Content != "" {
					full.WriteString(d.Content)
				}
				rText := d.ReasoningContent
				if rText == "" {
					rText = d.Reasoning
				}
				if rText != "" {
					reasoningFull.WriteString(rText)
				}
			}
			if chunk.Usage != nil {
				usage = &provider.Usage{PromptTokens: chunk.Usage.PromptTokens, CompletionTokens: chunk.Usage.CompletionTokens}
			}
		}
		if onDelta != nil {
			onDelta(data)
		}
	}
	if err := scanner.Err(); err != nil {
		return &provider.ChatResult{Content: full.String(), Usage: usage}, fmt.Errorf("stream scan: %w", err)
	}

	resContent := full.String()
	if reasoningFull.Len() > 0 && !strings.Contains(resContent, "<think>") {
		if resContent != "" {
			resContent = "<think>\n" + reasoningFull.String() + "\n</think>\n\n" + resContent
		} else {
			resContent = "<think>\n" + reasoningFull.String() + "\n</think>"
		}
	}

	if strings.TrimSpace(resContent) == "" {
		return nil, fmt.Errorf("empty stream response from model")
	}

	return &provider.ChatResult{Content: resContent, Usage: usage}, nil
}
