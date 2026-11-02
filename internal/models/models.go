package models

import "strings"

// Vendor 是一个厂商家族。
type Vendor struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// 计费档位：决定免费用户每天可用次数，以及是否需要 Pro 会员。
const (
	TierCheap    = "cheap"    // 国产 + flash 类，最便宜，放宽次数
	TierStandard = "standard" // Grok / Gemini Pro / GPT 主流
	TierPremium  = "premium"  // Claude 系列（非 Opus），单价高，次数收紧
	TierPro      = "pro"      // Opus / GPT-5.6 旗舰，仅 Pro 会员可用
)

// Model 是注册表中的一个可选模型。
type Model struct {
	ID          string `json:"id"`       // OpenRouter slug
	Vendor      string `json:"vendor"`   // 所属厂商 ID
	DisplayName string `json:"name"`     // 展示名
	ShortDesc   string `json:"desc"`     // 一句话描述
	Provider    string `json:"provider"` // "openrouter"（未来可为 "sub2api"）
	Tier        string `json:"tier"`     // cheap / standard / premium / pro
}

// Vendors 是首页/选择器展示顺序：高阶旗舰与主流家族。
var Vendors = []Vendor{
	{"grok", "Grok · xAI"},
	{"zai", "GLM · 智谱"},
	{"nvidia", "Nemotron · NVIDIA"},
	{"kimi", "Kimi · 月之暗面"},
	{"minimax", "MiniMax · 名之梦"},
	{"qwen", "Qwen · 通义千问"},
	{"deepseek", "DeepSeek · 深度求索"},
	{"gemini", "Gemini · Google"},
	{"claude", "Claude · Anthropic"},
	{"gpt", "GPT · OpenAI"},
	{"other", "其他 · Custom"},
}

// Registry 是每个厂商精选的主流聊天模型（对着 OpenRouter 实时列表核对，2026-07）。
// 刻意排除图像/音频/codex/deep-research 等非通用聊天变体，面向初学者保持简单。
var Registry = map[string][]Model{
	"grok": {
		{"supergrok", "grok", "Supergrok (Grok 4.6)", "xAI 最强旗舰推理模型，深度思考与逻辑分析，每小时限120条", "openai_compatible", TierStandard},
		{"grok-4.6", "grok", "Grok 4.6", "xAI 官方超强推理旗舰 · 限120条/时", "openai_compatible", TierStandard},
		{"grok-4.3", "grok", "Grok 4.3", "xAI 官方全能极速主力模型", "openai_compatible", TierStandard},
		{"grok-2-vision", "grok", "Grok 2 Vision", "xAI 官方多模态视觉模型，支持看图分析", "openai_compatible", TierStandard},
	},
	"zai": {
		{"zai-org/GLM-5.2-FP8", "zai", "GLM 5.2 (FP8 旗舰)", "智谱最强超级旗舰 · FP8 高精度超强大脑", "openai_compatible", TierStandard},
	},
	"nvidia": {
		{"nvidia/Nemotron-3-Nano-Omni-30B-A3B-Reasoning-BF16", "nvidia", "Nemotron 30B Reasoning", "英伟达 30B 旗舰深度推理模型 · 极强理科数理逻辑", "openai_compatible", TierStandard},
	},
	"kimi": {
		{"moonshotai/Kimi-K2.6", "kimi", "Kimi K2.6", "月之暗面旗舰长文 · 超长上下文与深度理解", "openai_compatible", TierStandard},
	},
	"minimax": {
		{"minimax-m3", "minimax", "MiniMax M3", "名之梦最新旗舰 · 多任务高阶全能大模型", "openai_compatible", TierStandard},
	},
	"qwen": {
		{"qwen3.8-27b", "qwen", "Qwen 3.8 27B", "通义千问 27B 大参数旗舰 · 中文与理科强化", "openai_compatible", TierStandard},
	},
	"deepseek": {
		{"deepseek-v4-flash-0731", "deepseek", "DeepSeek V4 Flash", "深度求索新一代极速模型 · 毫秒级极速响应", "openai_compatible", TierCheap},
		{"deepseek/deepseek-chat", "deepseek", "DeepSeek V3", "经典通用对话 · 成熟稳定高性价比", "openrouter", TierCheap},
	},
	"gemini": {
		{"gemini-3.7-flash", "gemini", "Gemini 3.7 Flash", "Google 最新一代混合推理与多模态极速模型 (推荐)", "openai_compatible", TierCheap},
		{"gemini-3.5-flash", "gemini", "Gemini 3.5 Flash", "Google 极速主力模型，极低延迟高性价比", "openai_compatible", TierCheap},
		{"gemini-2.5-pro", "gemini", "Gemini Pro", "Google 官方深度推理旗舰模型", "openai_compatible", TierStandard},
	},
	"claude": {
		{"anthropic/claude-opus-4.8", "claude", "Claude Opus 4.8", "Anthropic 旗舰，最强推理与写作", "openrouter", TierPro},
		{"anthropic/claude-sonnet-5", "claude", "Claude Sonnet 5", "新一代均衡款，性价比高", "openrouter", TierPremium},
		{"anthropic/claude-sonnet-4.6", "claude", "Claude Sonnet 4.6", "均衡稳定，日常首选", "openrouter", TierPremium},
		{"anthropic/claude-haiku-4.5", "claude", "Claude Haiku 4.5", "轻快便宜，适合高频短对话", "openrouter", TierPremium},
	},
	"gpt": {
		{"openai/gpt-5.6-sol", "gpt", "GPT-5.6 Sol", "OpenAI 旗舰，复杂任务表现顶尖", "openrouter", TierPro},
		{"openai/gpt-5.5", "gpt", "GPT-5.5", "上一代旗舰，通用能力强", "openrouter", TierStandard},
		{"openai/gpt-5.4", "gpt", "GPT-5.4", "均衡之选，速度快", "openrouter", TierStandard},
		{"openai/gpt-5-mini", "gpt", "GPT-5 Mini", "轻量便宜，日常够用", "openrouter", TierCheap},
		{"openai/gpt-4o", "gpt", "GPT-4o", "经典多模态，广泛验证", "openrouter", TierStandard},
	},
}

// TierOf 返回模型档位（未知模型按 standard 处理，偏保守）。
func TierOf(modelID string) string {
	if m, ok := byID[modelID]; ok && m.Tier != "" {
		return m.Tier
	}
	return TierStandard
}

// byID 是启动时构建的 model_id → Model 索引。
var byID = func() map[string]Model {
	m := make(map[string]Model)
	for _, list := range Registry {
		for _, item := range list {
			m[item.ID] = item
		}
	}
	return m
}()

// IsValid 判断某个 model_id 是否在注册表中（防止用户传入任意/昂贵模型）。
func IsValid(modelID string) bool {
	_, ok := byID[modelID]
	return ok
}

// VendorOf 返回某个 model_id 所属的厂商 ID（未知返回空串）。
func VendorOf(modelID string) string {
	if m, ok := byID[modelID]; ok {
		return m.Vendor
	}
	return ""
}

// InferVendor 根据 model ID 智能推断归属厂商。
func InferVendor(modelID string) string {
	low := strings.ToLower(modelID)
	switch {
	case strings.Contains(low, "grok") || strings.Contains(low, "xai") || strings.Contains(low, "x-ai") || strings.Contains(low, "supergrok"):
		return "grok"
	case strings.Contains(low, "glm") || strings.Contains(low, "zai"):
		return "zai"
	case strings.Contains(low, "nemotron") || strings.Contains(low, "nvidia"):
		return "nvidia"
	case strings.Contains(low, "kimi") || strings.Contains(low, "moonshot"):
		return "kimi"
	case strings.Contains(low, "minimax") || strings.Contains(low, "abab"):
		return "minimax"
	case strings.Contains(low, "qwen"):
		return "qwen"
	case strings.Contains(low, "claude") || strings.Contains(low, "anthropic"):
		return "claude"
	case strings.Contains(low, "gpt") || strings.Contains(low, "openai") || strings.Contains(low, "chatgpt") || strings.HasPrefix(low, "o1") || strings.HasPrefix(low, "o3") || strings.HasPrefix(low, "o4"):
		return "gpt"
	case strings.Contains(low, "gemini") || strings.Contains(low, "google"):
		return "gemini"
	case strings.Contains(low, "deepseek") || strings.Contains(low, "r1") || strings.Contains(low, "v3") || strings.Contains(low, "v4"):
		return "deepseek"
	default:
		return "other"
	}
}

// InferTier 根据 model ID 智能推断计费档位。
func InferTier(modelID string) string {
	low := strings.ToLower(modelID)
	switch {
	case strings.Contains(low, "opus") || strings.Contains(low, "gpt-5.6") || strings.Contains(low, "o1-pro"):
		return TierPro
	case strings.Contains(low, "sonnet") || strings.Contains(low, "haiku") || strings.Contains(low, "o1") || strings.Contains(low, "o3") || strings.Contains(low, "supergrok") || strings.Contains(low, "fp8") || strings.Contains(low, "reasoning"):
		return TierStandard
	case strings.Contains(low, "flash") || strings.Contains(low, "mini") || strings.Contains(low, "lite") || strings.Contains(low, "nano"):
		return TierCheap
	default:
		return TierStandard
	}
}

// List 返回指定厂商的模型列表。
func List(vendor string) []Model {
	return Registry[vendor]
}

// Get 按 ID 查模型。
func Get(modelID string) (Model, bool) {
	m, ok := byID[modelID]
	return m, ok
}

// FeaturedModel 是首页与默认用户端推荐上架的模型配置。
type FeaturedModel struct {
	ID          string   // 首选写入 DB 的 id
	RawID       string   // 上游真实 API 调用的模型 ID（如 grok-4.6、models/gemini-2.5-flash）
	Aliases     []string // 已存在库里时优先复用这些 id / raw_model_id
	Vendor      string
	Name        string
	Style       string // 极速 / 推理 / 视觉 / 旗舰
	Price       string // 展示用单价
	Tier        string
	Hints       []string // 用来匹配服务商 URL/名称
}

// Featured 用户端聊天下拉精选：6 款第三方 + 3 款 xAI，按价格从低到高排。
var Featured = []FeaturedModel{
	{
		ID: "minimax-m3", RawID: "minimax-m3", Aliases: []string{"minimax-m3", "minimax/minimax-m3"},
		Vendor: "minimax", Name: "MiniMax M3", Style: "国产旗舰 · 综合能力强",
		Price: "极便宜", Tier: TierCheap, Hints: []string{"vultr", "vultrinference"},
	},
	{
		ID: "moonshotai/Kimi-K2.6", RawID: "moonshotai/Kimi-K2.6", Aliases: []string{"moonshotai/Kimi-K2.6", "kimi-k2.6", "kimi"},
		Vendor: "kimi", Name: "Kimi K2.6", Style: "长文出色 · 中文首选",
		Price: "极便宜", Tier: TierCheap, Hints: []string{"vultr", "vultrinference"},
	},
	{
		ID: "deepseek-v4-flash-0731", RawID: "deepseek-v4-flash-0731", Aliases: []string{"deepseek-v4-flash-0731", "deepseek-flash", "deepseek-v4"},
		Vendor: "deepseek", Name: "DeepSeek V4 Flash", Style: "极致便宜 · 速度飞快",
		Price: "极便宜", Tier: TierCheap, Hints: []string{"vultr", "vultrinference"},
	},
	{
		ID: "qwen3.8-27b", RawID: "qwen3.8-27b", Aliases: []string{"qwen3.8-27b", "qwen-27b", "qwen"},
		Vendor: "other", Name: "Qwen 3.8 27B", Style: "阿里通义千问 · 均衡稳定",
		Price: "便宜", Tier: TierCheap, Hints: []string{"vultr", "vultrinference"},
	},
	{
		ID: "zai-org/GLM-5.2-FP8", RawID: "zai-org/GLM-5.2-FP8", Aliases: []string{"zai-org/GLM-5.2-FP8", "glm-5.2", "glm-5.2-fp8"},
		Vendor: "other", Name: "GLM 5.2 FP8", Style: "智谱 GLM · 多语言强",
		Price: "便宜", Tier: TierCheap, Hints: []string{"vultr", "vultrinference"},
	},
	{
		ID: "nvidia/Nemotron-Cascade-2-30B-A3B", RawID: "nvidia/Nemotron-Cascade-2-30B-A3B", Aliases: []string{"nvidia/Nemotron-Cascade-2-30B-A3B", "nemotron-cascade", "nemotron-cascade-30b-a3b"},
		Vendor: "other", Name: "Nemotron Cascade 30B A3B", Style: "NVIDIA MoE 推理专精",
		Price: "中等", Tier: TierStandard, Hints: []string{"vultr", "vultrinference"},
	},
	{
		ID: "grok-4.3", RawID: "grok-4.3", Aliases: []string{"grok-4.3", "x-ai/grok-4.3", "grok-2", "grok-3-mini"},
		Vendor: "grok", Name: "Grok 4.3", Style: "xAI 全能极速主力",
		Price: "$0.80→$2.00 /百万token", Tier: TierStandard, Hints: []string{"api.x.ai", "xai", "grok"},
	},
	{
		ID: "grok-4.7", RawID: "grok-4.7", Aliases: []string{"grok-4.7", "x-ai/grok-4.7"},
		Vendor: "grok", Name: "Grok 4.7", Style: "xAI 最新旗舰",
		Price: "xAI 官方直连", Tier: TierStandard, Hints: []string{"api.x.ai", "xai", "grok"},
	},
	{
		ID: "supergrok", RawID: "grok-4.6", Aliases: []string{"supergrok", "grok-4.6-super", "grok-4"},
		Vendor: "grok", Name: "Supergrok", Style: "旗舰推理 · 限120条/时",
		Price: "$2.00→$6.00 /百万token", Tier: TierStandard, Hints: []string{"api.x.ai", "xai", "grok"},
	},
}

// CanonicalID 把别名/上游 ID 收成站点内部 id（如 x-ai/grok-4.3 → grok-4.3）。
func CanonicalID(modelID string) string {
	id := strings.TrimSpace(modelID)
	if id == "" {
		return id
	}
	low := strings.ToLower(id)
	for _, f := range Featured {
		if strings.EqualFold(f.ID, id) || strings.EqualFold(f.RawID, id) {
			return f.ID
		}
		for _, a := range f.Aliases {
			if strings.EqualFold(a, id) {
				return f.ID
			}
		}
	}
	if strings.HasPrefix(low, "x-ai/") {
		return strings.TrimPrefix(low, "x-ai/")
	}
	if strings.HasPrefix(low, "models/") {
		return strings.TrimPrefix(low, "models/")
	}
	return id
}

// FeaturedDescription 生成后台/前台展示用的一句话。
func FeaturedDescription(f FeaturedModel) string {
	return f.Style + " · " + f.Price + " · 日常好用"
}

// InferDisplayName 美化展示名称。
func InferDisplayName(modelID string) string {
	s := modelID
	if idx := strings.LastIndex(s, "/"); idx != -1 {
		s = s[idx+1:]
	}
	parts := strings.Split(s, "-")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

