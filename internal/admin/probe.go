package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"deepweay-chat/internal/models"
	"deepweay-chat/internal/store"
)

type ProbeResult struct {
	OK         bool   `json:"ok"`
	ModelsOK   bool   `json:"models_ok"`
	ChatOK     bool   `json:"chat_ok"`
	Model      string `json:"model,omitempty"`
	ModelCount int    `json:"model_count"`
	Activated  int    `json:"activated"`
	Message    string `json:"message"`
	LatencyMS  int64  `json:"latency_ms"`
}

func guessProbeModel(baseURL, pType string, listed []DetectedModel) string {
	for _, m := range listed {
		id := strings.TrimSpace(m.RawModelID)
		if id == "" {
			id = strings.TrimSpace(m.ID)
		}
		low := strings.ToLower(id)
		if strings.Contains(low, "embed") || strings.Contains(low, "tts") || strings.Contains(low, "whisper") || strings.Contains(low, "image") || strings.Contains(low, "moderation") {
			continue
		}
		if id != "" {
			return id
		}
	}
	blob := strings.ToLower(baseURL + " " + pType)
	switch {
	case strings.Contains(blob, "vultr") || strings.Contains(blob, "vultrinference"):
		return "deepseek-v4-flash-0731"
	case strings.Contains(blob, "x.ai") || strings.Contains(blob, "xai") || strings.Contains(blob, "grok"):
		return "grok-4.3"
	case strings.Contains(blob, "generativelanguage") || strings.Contains(blob, "gemini"):
		return "gemini-2.5-flash"
	case strings.Contains(blob, "deepseek"):
		return "deepseek-chat"
	case strings.Contains(blob, "openrouter"):
		return "openai/gpt-4o-mini"
	case strings.Contains(blob, "openai.com"):
		return "gpt-4o-mini"
	default:
		if len(models.Featured) > 0 && models.Featured[0].RawID != "" {
			return models.Featured[0].RawID
		}
		return "grok-4.3"
	}
}

func probeChatCompletion(baseURL, apiKey, pType, model string) error {
	payload, _ := json.Marshal(map[string]any{
		"model":      model,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		"max_tokens": 1,
	})
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(baseURL, "/")+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if pType == "openrouter" || strings.Contains(strings.ToLower(baseURL), "openrouter") {
		req.Header.Set("HTTP-Referer", "https://chat.example.com")
		req.Header.Set("X-Title", "Deepweay AI")
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("网络错误: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%s", friendlyUpstreamErr(resp.StatusCode, string(body)))
	}
	// 400 多数是模型 ID 不对，但 Key 已经打通。
	if resp.StatusCode == http.StatusBadRequest {
		return fmt.Errorf("auth_ok_model_mismatch: %s", friendlyUpstreamErr(resp.StatusCode, string(body)))
	}
	return fmt.Errorf("%s", friendlyUpstreamErr(resp.StatusCode, string(body)))
}

func ProbeUpstream(baseURL, apiKey, pType string) ProbeResult {
	start := time.Now()
	out := ProbeResult{}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	apiKey = strings.TrimSpace(apiKey)
	if baseURL == "" {
		out.Message = "Base URL 不能为空"
		return out
	}
	if apiKey == "" {
		out.Message = "未填写 API Key，无法探测"
		out.LatencyMS = time.Since(start).Milliseconds()
		return out
	}

	listed, listErr := detectUpstreamModels(baseURL, apiKey, pType)
	if listErr == nil {
		out.ModelsOK = true
		out.ModelCount = len(listed)
	}

	model := guessProbeModel(baseURL, pType, listed)
	out.Model = model
	chatErr := probeChatCompletion(baseURL, apiKey, pType, model)
	switch {
	case chatErr == nil:
		out.ChatOK = true
	case strings.HasPrefix(chatErr.Error(), "auth_ok_model_mismatch:"):
		out.ChatOK = false
		if out.ModelsOK {
			out.Message = "Key 有效（/models 通），试聊模型 " + model + " 被拒，可再选具体模型"
		} else {
			out.Message = "Key 大概率有效，但试聊模型 " + model + " 无效"
		}
	default:
		if !out.ModelsOK {
			out.Message = "探测失败: "
			if listErr != nil {
				out.Message += listErr.Error()
			} else {
				out.Message += chatErr.Error()
			}
		} else {
			out.Message = "模型列表可读，但试聊失败: " + chatErr.Error()
		}
	}

	// 只有真正能聊天才算生效。/models 通但额度耗尽（429）不能上架。
	out.OK = out.ChatOK
	if !out.OK && out.ModelsOK && strings.Contains(out.Message, "Key 有效") {
		out.OK = true
	}
	if out.OK && out.Message == "" {
		if out.ChatOK && out.ModelsOK {
			out.Message = fmt.Sprintf("连通正常：可读 %d 个模型，试聊 %s 成功", out.ModelCount, model)
		} else if out.ChatOK {
			out.Message = "试聊成功（" + model + "），/models 未返回列表也不影响使用"
		} else {
			out.Message = fmt.Sprintf("Key 有效，已读到 %d 个模型", out.ModelCount)
		}
	}
	out.LatencyMS = time.Since(start).Milliseconds()
	return out
}

func (a *Admin) probeAndActivate(p *store.Provider) ProbeResult {
	pr := ProbeUpstream(p.BaseURL, p.APIKey, p.Type)
	if os.Getenv("XAI_API_KEY") != "" {
		return pr
	}
	if pr.OK && p.IsEnabled {
		if n, err := a.Store.EnableFeaturedForProvider(p); err == nil {
			pr.Activated = n
			if n > 0 {
				pr.Message += fmt.Sprintf("；已上架本渠道精选 %d 款", n)
			}
		}
	} else if p.ID > 0 {
		_ = a.Store.SetProviderModelsEnabled(p.ID, false)
		pr.Message += "；已从用户下架该渠道模型"
	}
	return pr
}

func (a *Admin) RefreshUsableProviders() {
	list, err := a.Store.ListProviders()
	if err != nil {
		return
	}
	for i := range list {
		p := &list[i]
		if !p.IsEnabled || strings.TrimSpace(p.APIKey) == "" {
			continue
		}
		pr := a.probeAndActivate(p)
		slog.Info("provider probe", "name", p.Name, "ok", pr.OK, "msg", pr.Message, "ms", pr.LatencyMS)
	}
}

func providerJSON(p *store.Provider, probe *ProbeResult) map[string]any {
	out := map[string]any{
		"id":          p.ID,
		"name":        p.Name,
		"type":        p.Type,
		"base_url":    p.BaseURL,
		"api_key":     p.APIKey,
		"is_enabled":  p.IsEnabled,
		"created_at":  p.CreatedAt,
		"updated_at":  p.UpdatedAt,
		"model_count": p.ModelCount,
	}
	if probe != nil {
		out["probe"] = probe
	}
	return out
}
