// Package mock streams documentation replies for seven vendors.
// It never opens a network connection and never reads an API key.
package mock

import (
	"context"
	"fmt"
	"strings"
	"time"

	"deepweay-chat/internal/provider"
)

// Vendors is the public registry shown by the demo.
var Vendors = []string{"xAI", "Anthropic", "OpenAI", "Google", "DeepSeek", "MiniMax", "Moonshot"}

// Provider implements provider.Provider with a local SSE replay.
type Provider struct {
	Vendor string
	now    func() time.Time
}

// New returns a mock upstream for one vendor label.
func New(vendor string) *Provider {
	return &Provider{Vendor: vendor, now: time.Now}
}

func (p *Provider) Name() string { return "mock:" + p.Vendor }

// ChatCompletion returns the full sample without streaming.
func (p *Provider) ChatCompletion(ctx context.Context, req provider.ChatRequest) (*provider.ChatResult, error) {
	text := p.script(req)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &provider.ChatResult{
		Content: text,
		Usage:   &provider.Usage{PromptTokens: 48, CompletionTokens: tokenEstimate(text)},
	}, nil
}

// StreamChatCompletion emits OpenAI-shaped SSE data lines, then a usage line.
func (p *Provider) StreamChatCompletion(ctx context.Context, req provider.ChatRequest, onDelta func(rawSSEData string)) (*provider.ChatResult, error) {
	text := p.script(req)
	var sent strings.Builder
	for _, word := range strings.Fields(text) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if sent.Len() > 0 {
			sent.WriteByte(' ')
		}
		sent.WriteString(word)
		if onDelta != nil {
			onDelta(fmt.Sprintf(`{"choices":[{"delta":{"content":"%s "}}]}`, escape(word)))
		}
	}
	usage := &provider.Usage{PromptTokens: 48, CompletionTokens: tokenEstimate(text)}
	if onDelta != nil {
		onDelta(fmt.Sprintf(`{"choices":[],"usage":{"prompt_tokens":%d,"completion_tokens":%d}}`, usage.PromptTokens, usage.CompletionTokens))
	}
	return &provider.ChatResult{Content: sent.String(), Usage: usage}, nil
}

func (p *Provider) script(req provider.ChatRequest) string {
	prompt := ""
	if n := len(req.Messages); n > 0 {
		prompt = req.Messages[n-1].Content
	}
	return fmt.Sprintf("%s sample. model=%s. No upstream call. Prompt excerpt: %s", p.Vendor, or(req.Model, "demo"), clip(prompt, 80))
}

func or(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(empty)"
	}
	if len(s) > n {
		return s[:n]
	}
	return s
}

func escape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func tokenEstimate(s string) int {
	n := len(strings.Fields(s))
	if n == 0 {
		return 1
	}
	return n
}
