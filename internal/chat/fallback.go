package chat

import (
	"strings"

	"deepweay-chat/internal/models"
	"deepweay-chat/internal/store"
)

// SpecificFallbackRules 定义常见模型的首选降级模型映射
var specificFallbackMap = map[string][]string{
	// Grok 家族
	"supergrok":     {"grok-4.6", "grok-4.3", "deepseek-v4-flash-0731", "qwen3.8-27b", "minimax-m3"},
	"grok-4.6":      {"grok-4.3", "supergrok", "deepseek-v4-flash-0731", "qwen3.8-27b", "minimax-m3"},
	"grok-4.3":      {"supergrok", "grok-4.6", "deepseek-v4-flash-0731", "qwen3.8-27b"},
	"grok-2":        {"grok-4.3", "grok-4.6", "deepseek-v4-flash-0731"},
	"grok-3-mini":   {"grok-4.3", "deepseek-v4-flash-0731", "qwen3.8-27b"},
	"grok-2-vision": {"grok-4.6", "supergrok", "grok-4.3"},

	// 国产与主力推理
	"moonshotai/Kimi-K2.6":                              {"qwen3.8-27b", "deepseek-v4-flash-0731", "minimax-m3", "grok-4.3", "supergrok"},
	"zai-org/GLM-5.2-FP8":                               {"qwen3.8-27b", "deepseek-v4-flash-0731", "minimax-m3", "grok-4.3"},
	"nvidia/Nemotron-Cascade-2-30B-A3B":                 {"qwen3.8-27b", "minimax-m3", "deepseek-v4-flash-0731", "grok-4.3"},
	"nvidia/Nemotron-3-Nano-Omni-30B-A3B-Reasoning-BF16": {"qwen3.8-27b", "minimax-m3", "deepseek-v4-flash-0731", "grok-4.3"},
	"minimax-m3":                                        {"deepseek-v4-flash-0731", "qwen3.8-27b", "grok-4.3", "supergrok"},
	"qwen3.8-27b":                                       {"deepseek-v4-flash-0731", "minimax-m3", "grok-4.3", "supergrok"},
	"deepseek-v4-flash-0731":                            {"qwen3.8-27b", "minimax-m3", "grok-4.3", "supergrok"},
	"deepseek/deepseek-chat":                            {"deepseek-v4-flash-0731", "qwen3.8-27b", "grok-4.3"},
	"deepseek-chat":                                     {"deepseek-v4-flash-0731", "qwen3.8-27b", "grok-4.3"},

	// Gemini / Claude / GPT 等如果无专属 Provider，自动降级至最强稳定模型
	"gemini-3.7-flash":          {"grok-4.3", "deepseek-v4-flash-0731", "qwen3.8-27b", "supergrok"},
	"gemini-3.5-flash":          {"grok-4.3", "deepseek-v4-flash-0731", "qwen3.8-27b"},
	"gemini-2.5-pro":            {"supergrok", "grok-4.6", "grok-4.3", "qwen3.8-27b"},
	"anthropic/claude-opus-4.8": {"supergrok", "grok-4.6", "grok-4.3", "qwen3.8-27b"},
	"anthropic/claude-sonnet-5": {"supergrok", "grok-4.6", "grok-4.3", "qwen3.8-27b"},
	"anthropic/claude-sonnet-4.6": {"supergrok", "grok-4.6", "grok-4.3", "qwen3.8-27b"},
	"openai/gpt-5.6-sol":        {"supergrok", "grok-4.6", "grok-4.3", "qwen3.8-27b"},
	"openai/gpt-4o":             {"supergrok", "grok-4.3", "deepseek-v4-flash-0731", "qwen3.8-27b"},
}

// GlobalAnchorFallbackModels 全局高可用兜底顺序
var GlobalAnchorFallbackModels = []string{
	"grok-4.3",
	"supergrok",
	"grok-4.6",
	"deepseek-v4-flash-0731",
	"qwen3.8-27b",
	"minimax-m3",
}

// FallbackCandidates 根据当前请求的模型 ID 和数据库可用模型列表，计算有序的降级备选模型列表。
func FallbackCandidates(targetModelID string, usableModels []store.ModelItem) []string {
	targetCanon := models.CanonicalID(targetModelID)
	seen := make(map[string]bool)
	seen[strings.ToLower(targetModelID)] = true
	seen[strings.ToLower(targetCanon)] = true

	var candidates []string
	addCandidate := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		low := strings.ToLower(id)
		canon := models.CanonicalID(id)
		canonLow := strings.ToLower(canon)
		if seen[low] || seen[canonLow] {
			return
		}
		seen[low] = true
		seen[canonLow] = true
		candidates = append(candidates, id)
	}

	// 1. 查特定规则映射
	if list, ok := specificFallbackMap[targetCanon]; ok {
		for _, m := range list {
			addCandidate(m)
		}
	}
	if list, ok := specificFallbackMap[targetModelID]; ok {
		for _, m := range list {
			addCandidate(m)
		}
	}

	// 2. 查同厂商的其他可用模型
	vendor := models.VendorOf(targetCanon)
	if vendor == "" {
		vendor = models.InferVendor(targetModelID)
	}
	for _, um := range usableModels {
		if !um.IsEnabled {
			continue
		}
		if um.Vendor == vendor || models.InferVendor(um.ID) == vendor {
			addCandidate(um.ID)
		}
	}

	// 3. 查当前所有已启用且有 Key 的可用模型
	for _, um := range usableModels {
		if um.IsEnabled {
			addCandidate(um.ID)
		}
	}

	// 4. 加入全局锚点模型
	for _, gm := range GlobalAnchorFallbackModels {
		addCandidate(gm)
	}

	return candidates
}

// FormatDowngradeNotice 生成友好的降级提示卡片（Markdown 格式）
func FormatDowngradeNotice(origName, fallbackName, reason string) string {
	reasonText := ""
	if strings.TrimSpace(reason) != "" {
		reasonText = "（" + strings.TrimSpace(reason) + "）"
	}
	return "> 💡 **模型自动降级提示**：原选择模型「" + origName + "」" + reasonText + "暂时无法响应，已自动无缝切换至高可用稳定模型「" + fallbackName + "」为您继续解答：\n\n"
}
