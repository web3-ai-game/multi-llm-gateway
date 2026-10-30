package provider

import "context"

// ContentPart 是一条消息的图文子元素。
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL 图片链接或 base64 data url。
type ImageURL struct {
	URL string `json:"url"`
}

// Message 是一条对话消息（OpenAI 兼容格式，支持多模态图片）。
type Message struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"`
}

// ChatRequest 是一次聊天补全请求。
type ChatRequest struct {
	Model        string
	Messages     []Message
	MaxTokens    *int
	Temperature  *float64
	IncludeUsage bool // 要求上游返回 token 用量（用于花费统计）
	Thinking     bool // 开启深度思考 / 推理模式
	WebSearch    bool // 开启联网搜索
}

// Usage 是上游返回的 token 用量。
type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

// ChatResult 是一次补全的结果。Usage 为 nil 表示上游未返回用量，调用方需自行估算。
type ChatResult struct {
	Content string
	Usage   *Usage
}

// Provider 是可插拔的上游供应商接口。
// MVP 只有 openrouter 一个实现；未来接入 sub2api 只需新增一个实现，
// chat 层与数据库 schema 完全不用改动。
type Provider interface {
	Name() string
	ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResult, error)
	// StreamChatCompletion 以 SSE 流式调用，onDelta 收到每个原始 SSE data 行。
	// 返回累积的完整文本 + 最终用量（若上游提供）。
	StreamChatCompletion(ctx context.Context, req ChatRequest, onDelta func(rawSSEData string)) (*ChatResult, error)
}
