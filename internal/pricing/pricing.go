package pricing

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const modelsURL = "https://openrouter.ai/api/v1/models"

// Price 是单个模型的每 token 价格（USD）。
type Price struct {
	PromptPerToken     float64
	CompletionPerToken float64
}

// fallback 是硬编码兜底价（对着 OpenRouter 实时列表核对，2026-07，单位 USD/token）。
// 当在线拉取失败且 DB 缓存也没有时使用。仅覆盖注册表内精选模型。
var fallback = map[string]Price{
	// grok
	"x-ai/grok-4.5":  {0.000002, 0.000006},
	"x-ai/grok-4.3":  {0.00000125, 0.0000025},
	"x-ai/grok-4.20": {0.00000125, 0.0000025},
	// claude
	"anthropic/claude-opus-4.8":  {0.000005, 0.000025},
	"anthropic/claude-sonnet-5":  {0.000002, 0.00001},
	"anthropic/claude-sonnet-4.6": {0.000003, 0.000015},
	"anthropic/claude-haiku-4.5":  {0.000001, 0.000005},
	// gpt
	"openai/gpt-5.6-sol": {0.000005, 0.00003},
	"openai/gpt-5.5":     {0.000005, 0.00003},
	"openai/gpt-5.4":     {0.0000025, 0.000015},
	"openai/gpt-5-mini":  {0.00000025, 0.000002},
	"openai/gpt-4o":      {0.0000025, 0.00001},
	// gemini
	"google/gemini-3.1-pro-preview": {0.000002, 0.000012},
	"google/gemini-3.6-flash":       {0.0000015, 0.0000075},
	"google/gemini-3.5-flash":       {0.0000015, 0.000009},
	"google/gemini-2.5-pro":         {0.00000125, 0.00001},
	"google/gemini-2.5-flash":       {0.0000003, 0.0000025},
	"google/gemini-2.5-flash-lite":  {0.0000001, 0.0000004},
	// deepseek
	"deepseek/deepseek-v4-pro":   {0.000000435, 0.00000087},
	"deepseek/deepseek-v4-flash": {0.000000098, 0.000000196},
	"deepseek/deepseek-v3.2":     {0.000000269, 0.0000004},
	"deepseek/deepseek-r1-0528":  {0.0000005, 0.00000215},
	"deepseek/deepseek-chat":     {0.0000002002, 0.0000008001},
	// minimax
	"minimax/minimax-m3":   {0.0000003, 0.0000012},
	"minimax/minimax-m2.7": {0.00000025, 0.000001},
	"minimax/minimax-m2.5": {0.00000015, 0.0000009},
	"minimax/minimax-m2.1": {0.0000003, 0.0000012},
	"minimax/minimax-m1":   {0.00000055, 0.0000022},
	// kimi
	"moonshotai/kimi-k3":          {0.000003, 0.000015},
	"moonshotai/kimi-k2.6":        {0.000000684, 0.00000342},
	"moonshotai/kimi-k2.5":        {0.00000057, 0.00000285},
	"moonshotai/kimi-k2-thinking": {0.0000006, 0.0000025},
	"moonshotai/kimi-k2":          {0.00000057, 0.0000023},
}

// PersistFn 用于把在线拉取到的价格落库（避免重启后退回兜底表）。
type PersistFn func(modelID string, prompt, completion float64) error

// Cache 是线程安全的价格缓存。
type Cache struct {
	mu      sync.RWMutex
	live    map[string]Price
	persist PersistFn
}

// New 创建缓存。seed 是从 DB 读到的上次落库价格（可为 nil）。
func New(seed map[string][2]float64, persist PersistFn) *Cache {
	live := make(map[string]Price)
	for id, p := range seed {
		live[id] = Price{PromptPerToken: p[0], CompletionPerToken: p[1]}
	}
	return &Cache{live: live, persist: persist}
}

// Lookup 按 model_id 查价：优先在线缓存 → 兜底表 → 未知返回 ok=false。
func (c *Cache) Lookup(modelID string) (Price, bool) {
	c.mu.RLock()
	p, ok := c.live[modelID]
	c.mu.RUnlock()
	if ok {
		return p, true
	}
	if p, ok := fallback[modelID]; ok {
		return p, true
	}
	return Price{}, false
}

// Cost 计算一次补全的花费（USD）。未知模型返回 0。
func (c *Cache) Cost(modelID string, promptTokens, completionTokens int) float64 {
	p, ok := c.Lookup(modelID)
	if !ok {
		return 0
	}
	return float64(promptTokens)*p.PromptPerToken + float64(completionTokens)*p.CompletionPerToken
}

// RefreshFromOpenRouter 拉取实时价格并更新缓存 + 落库。
func (c *Cache) RefreshFromOpenRouter(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var payload struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}

	next := make(map[string]Price)
	for _, m := range payload.Data {
		pp := parseFloat(m.Pricing.Prompt)
		cp := parseFloat(m.Pricing.Completion)
		next[m.ID] = Price{PromptPerToken: pp, CompletionPerToken: cp}
	}

	c.mu.Lock()
	c.live = next
	c.mu.Unlock()

	// 只把我们精选注册表内的模型落库（其余不必占用 DB）。
	if c.persist != nil {
		for id, p := range next {
			if _, isFallback := fallback[id]; isFallback {
				_ = c.persist(id, p.PromptPerToken, p.CompletionPerToken)
			}
		}
	}
	slog.Info("pricing refreshed from openrouter", "models", len(next))
	return nil
}

// StartAutoRefresh 启动时拉一次 + 每小时刷新。
func (c *Cache) StartAutoRefresh(ctx context.Context) {
	go func() {
		if err := c.RefreshFromOpenRouter(ctx); err != nil {
			slog.Warn("initial pricing refresh failed, using DB/fallback", "err", err)
		}
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := c.RefreshFromOpenRouter(ctx); err != nil {
					slog.Warn("pricing refresh failed", "err", err)
				}
			}
		}
	}()
}

func parseFloat(s string) float64 {
	if s == "" {
		return 0
	}
	var f float64
	// OpenRouter 价格是字符串形式的小数，用 json.Unmarshal 借道解析最省事。
	if err := json.Unmarshal([]byte(s), &f); err != nil {
		return 0
	}
	return f
}
