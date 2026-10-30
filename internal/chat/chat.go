package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"deepweay-chat/internal/billing"
	"deepweay-chat/internal/models"
	"deepweay-chat/internal/pricing"
	"deepweay-chat/internal/provider"
	"deepweay-chat/internal/provider/oai"
	"deepweay-chat/internal/store"
	"deepweay-chat/internal/websearch"
)

const (
	MaxHistory     = 40
	defaultTitle   = "新对话"
	titleGenAt     = 4
	maxAnswerToken = 8192
	DefaultModel   = "grok-4.7"
)

// Deps 是 chat handlers 所需的依赖。
type Deps struct {
	Store      *store.Store
	Pricing    *pricing.Cache
	TitleModel string
	EnvORKey   string // 启动时 env 里的 OPENROUTER_API_KEY，作为 config 未设时兜底
}

func userFromCtx(r *http.Request) *store.User { return store.UserFromCtx(r.Context()) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func errResp(msg string) map[string]string { return map[string]string{"error": msg} }

// limits 从 app_config 读取额度配置（管理员可后台调整），失败回退默认。
func (d *Deps) limits() billing.Limits {
	raw, _ := d.Store.GetConfig(billing.KeyLimits)
	return billing.ParseLimits(raw)
}

// BillingInfoHandler GET /api/billing/info — 用户端「我的额度 / 套餐 / 付款方式」。
func (d *Deps) BillingInfoHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFromCtx(r)
		now := time.Now()
		lim := d.limits()
		isPro := u.ProActive()
		byModel, _ := d.Store.CountMessagesByModelSince(u.ID, billing.DayStart(now))
		total := 0
		for _, n := range byModel {
			total += n
		}
		cfg, _ := d.Store.GetAllConfig()

		plan := "free"
		if u.IsAdmin || u.Unlimited {
			plan = "unlimited"
		} else if isPro {
			plan = "pro"
		}
		proUntil := ""
		if u.ProUntil != nil {
			proUntil = u.ProUntil.Format("2006-01-02")
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"plan":           plan,
			"is_pro":         isPro,
			"pro_until":      proUntil,
			"balance_usd":    u.BalanceUSD,
			"today_messages": total,
			"tiers":          billing.Summarize(byModel, lim, isPro),
			"next_reset":     billing.NextReset(now).UTC().Format(time.RFC3339),
			"soft_nudge_at":  atoiOr(cfg[billing.KeySoftNudgeAt], billing.DefaultSoftNudgeAt),
			"pro_price":      floatOr(cfg[billing.KeyProPrice], billing.DefaultProPrice),
			"min_topup":      floatOr(cfg[billing.KeyMinTopup], billing.DefaultMinTopup),
			"contact_email":  strOr(cfg[billing.KeyContactEmail], billing.DefaultContactEmail),
			"pay": map[string]any{
				"alipay_qr":    cfg[billing.KeyAlipayQR],
				"wechat_qr":    cfg[billing.KeyWechatQR],
				"googlepay_on": cfg[billing.KeyGooglePayOn] == "1",
			},
		})
	}
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
		return n
	}
	return def
}

func floatOr(s string, def float64) float64 {
	if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && f > 0 {
		return f
	}
	return def
}

func strOr(s, def string) string {
	if strings.TrimSpace(s) != "" {
		return s
	}
	return def
}

// GlobalBaseSystemPrompt 是全局底层基座提示词（不可在用户列表更改，保障高浓度、专业周全、有人情味与主动延伸）。
const GlobalBaseSystemPrompt = `你是由 OECE 打造的顶尖全能 AI 专家助手。请遵循以下核心准则与用户交流：
1. 【高浓度输出与专业详尽】：不设任何机械围栏与敷衍限制，充分释放最高阶的认知与推理能力。对用户提出的问题进行条理清晰、层次分明、深入透彻、解释清晰周全的专业解答。遇到代码或工程问题，直接提供规范、高效、具备完整上下文与可执行性的优质代码方案。
2. 【自然真诚，富有人情味】：保持温暖、真诚、善解人意且具备同理心的人格化沟通风格。杜绝冰冷机械的说教，善于将晦涩的技术原理与复杂概念用通俗、生动、易懂的语言娓娓道来。
3. 【主动前瞻与价值延伸】：在精确解答核心问题之余，主动洞察用户的潜在诉求与业务场景，贴心提供关键注意事项、避坑指南、优化建议或下一步可探索的方向。
4. 【优雅排版】：严格使用整洁精美的 Markdown 格式组织内容（善用多级标题、代码块高亮、列表与表格），确保阅读体验清晰舒适。`

func (d *Deps) findMatchingProvider(hints []string) *store.Provider {
	providers, err := d.Store.ListProviders()
	if err != nil || len(providers) == 0 {
		return nil
	}
	for _, h := range hints {
		hl := strings.ToLower(h)
		for _, p := range providers {
			if !p.IsEnabled || strings.TrimSpace(p.BaseURL) == "" || strings.TrimSpace(p.APIKey) == "" {
				continue
			}
			blob := strings.ToLower(p.BaseURL + " " + p.Name + " " + p.Type)
			if strings.Contains(blob, hl) {
				cp := p
				return &cp
			}
		}
	}
	return nil
}

func openRouterModelID(modelID, rawID, vendor string) string {
	id := strings.TrimSpace(rawID)
	if id == "" {
		id = strings.TrimSpace(modelID)
	}
	if strings.Contains(id, "/") {
		return id
	}
	low := strings.ToLower(id + " " + vendor + " " + modelID)
	switch {
	case strings.Contains(low, "supergrok") || id == "grok-4.6":
		return "x-ai/grok-4.6"
	case strings.HasPrefix(id, "grok") || vendor == "grok":
		return "x-ai/" + strings.TrimPrefix(id, "x-ai/")
	case strings.HasPrefix(id, "gemini") || vendor == "gemini":
		return "google/" + strings.TrimPrefix(strings.TrimPrefix(id, "models/"), "google/")
	default:
		return id
	}
}

func cleanRawModelForProvider(p *store.Provider, modelID, rawID string) string {
	if p == nil {
		return rawID
	}
	base := strings.ToLower(p.BaseURL + " " + p.Name)
	raw := strings.TrimSpace(rawID)
	if raw == "" {
		raw = modelID
	}

	if strings.Contains(base, "api.x.ai") || strings.Contains(base, "xai") || strings.Contains(base, "grok") {
		clean := strings.TrimPrefix(raw, "x-ai/")
		clean = strings.TrimPrefix(clean, "xai/")
		if clean == "supergrok" || clean == "grok-4.6" {
			return "grok-4.6"
		}
		if clean == "grok-4.7" {
			return "grok-4.7"
		}
		if clean == "grok-2" || clean == "grok-4.3" || clean == "grok-3-mini" || clean == "grok-beta" {
			return "grok-4.3"
		}
		if clean == "grok-2-vision" || clean == "grok-2-vision-1212" {
			return "grok-4.6"
		}
		return clean
	}

	if strings.Contains(base, "generativelanguage.googleapis.com") || strings.Contains(base, "gemini") {
		clean := strings.TrimPrefix(raw, "models/")
		clean = strings.TrimPrefix(clean, "google/")
		return clean
	}

	return raw
}

// providerForID 按 model_id 找 DB 模型或注册信息构建 provider，并返回上游实际模型 ID。
func (d *Deps) providerForID(modelID string) (provider.Provider, string, error) {
	rawID := modelID
	vendor := ""

	// 1. 优先查数据库中的自定义模型与关联服务商
	if dbModel, err := d.Store.GetModel(modelID); err == nil && dbModel != nil {
		if dbModel.RawModelID != "" {
			rawID = dbModel.RawModelID
		}
		vendor = dbModel.Vendor
		if dbModel.ProviderID > 0 {
			if p, err := d.Store.GetProvider(dbModel.ProviderID); err == nil && p != nil && p.IsEnabled && strings.TrimSpace(p.APIKey) != "" {
				// 校验该服务商是否符合厂商特征（避免 Grok 模型关联到 Google 服务商导致 404）
				isGrok := vendor == "grok" || strings.Contains(strings.ToLower(modelID), "grok")
				isGemini := vendor == "gemini" || strings.Contains(strings.ToLower(modelID), "gemini")
				pBlob := strings.ToLower(p.BaseURL + " " + p.Name)

				var matchedP *store.Provider = p
				if isGrok && !strings.Contains(pBlob, "x.ai") && !strings.Contains(pBlob, "xai") && !strings.Contains(pBlob, "grok") {
					if alt := d.findMatchingProvider([]string{"api.x.ai", "xai", "grok"}); alt != nil {
						matchedP = alt
					}
				} else if isGemini && (strings.Contains(pBlob, "x.ai") || strings.Contains(pBlob, "xai")) {
					if alt := d.findMatchingProvider([]string{"generativelanguage.googleapis.com", "gemini", "google"}); alt != nil {
						matchedP = alt
					}
				}

				if strings.TrimSpace(matchedP.BaseURL) != "" && strings.TrimSpace(matchedP.APIKey) != "" {
					var extraHeaders map[string]string
					if matchedP.Type == "openrouter" || strings.Contains(strings.ToLower(matchedP.BaseURL), "openrouter") {
						extraHeaders = map[string]string{
							"HTTP-Referer": "https://chat.example.com",
							"X-Title":      "Deepweay AI",
						}
					}
					realModel := cleanRawModelForProvider(matchedP, modelID, rawID)
					return oai.New(matchedP.Name, matchedP.BaseURL, matchedP.APIKey, extraHeaders), realModel, nil
				}
			}
		}
	}

	// 自动探测厂商匹配的服务商
	if strings.Contains(strings.ToLower(modelID), "grok") || vendor == "grok" {
		if p := d.findMatchingProvider([]string{"api.x.ai", "xai", "grok"}); p != nil {
			realModel := cleanRawModelForProvider(p, modelID, rawID)
			return oai.New(p.Name, p.BaseURL, p.APIKey, nil), realModel, nil
		}
	}
	if strings.Contains(strings.ToLower(modelID), "gemini") || vendor == "gemini" {
		if p := d.findMatchingProvider([]string{"generativelanguage.googleapis.com", "gemini", "google"}); p != nil {
			realModel := cleanRawModelForProvider(p, modelID, rawID)
			return oai.New(p.Name, p.BaseURL, p.APIKey, nil), realModel, nil
		}
	}
	// Vultr 常用模型适配
	if strings.Contains(strings.ToLower(modelID), "vultr") || strings.Contains(strings.ToLower(modelID), "qwen") || strings.Contains(strings.ToLower(modelID), "minimax") || strings.Contains(strings.ToLower(modelID), "moonshot") || strings.Contains(strings.ToLower(modelID), "kimi") || strings.Contains(strings.ToLower(modelID), "glm") || strings.Contains(strings.ToLower(modelID), "nemotron") || strings.Contains(strings.ToLower(modelID), "deepseek-v4") {
		if p := d.findMatchingProvider([]string{"vultrinference", "vultr"}); p != nil {
			realModel := cleanRawModelForProvider(p, modelID, rawID)
			return oai.New(p.Name, p.BaseURL, p.APIKey, nil), realModel, nil
		}
	}

	// 2. 兼容旧配置/全局配置
	cfg, _ := d.Store.GetAllConfig()
	sub2Base := strings.TrimSpace(cfg["sub2api_base_url"])
	sub2Key := strings.TrimSpace(cfg["sub2api_api_key"])
	orKey := strings.TrimSpace(cfg["openrouter_api_key"])
	if orKey == "" {
		orKey = d.EnvORKey
	}

	if sub2Base != "" && sub2Key != "" && !strings.Contains(sub2Key, "PLACEHOLDER") {
		return oai.New("sub2api", sub2Base, sub2Key, nil), rawID, nil
	}
	if orKey != "" && !strings.Contains(orKey, "PLACEHOLDER") {
		return oai.NewOpenRouter(orKey), openRouterModelID(modelID, rawID, vendor), nil
	}

	return nil, "", fmt.Errorf("未找到有效 API 渠道（模型: %s）", modelID)
}

// ── models meta（公开选择器数据）──────────────────────────────────────────

type vendorMeta struct {
	ID          string         `json:"id"`
	DisplayName string         `json:"display_name"`
	Models      []models.Model `json:"models"`
}

func (d *Deps) ModelsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbModels, err := d.Store.ListUsableModels()
		if err != nil {
			slog.Warn("list usable models failed", "err", err)
		}
		if len(dbModels) > 0 {
			byVendor := make(map[string][]models.Model)
			for _, m := range dbModels {
				v := m.Vendor
				if v == "" || v == "other" {
					v = models.InferVendor(m.ID)
				}
				byVendor[v] = append(byVendor[v], models.Model{
					ID:          m.ID,
					Vendor:      v,
					DisplayName: m.Name,
					ShortDesc:   m.Description,
					Provider:    m.ProviderName,
					Tier:        m.Tier,
				})
			}

			out := make([]vendorMeta, 0, len(models.Vendors))
			for _, v := range models.Vendors {
				if list, ok := byVendor[v.ID]; ok && len(list) > 0 {
					out = append(out, vendorMeta{ID: v.ID, DisplayName: v.DisplayName, Models: list})
				}
			}
			for vid, list := range byVendor {
				found := false
				for _, v := range models.Vendors {
					if v.ID == vid {
						found = true
						break
					}
				}
				if !found && len(list) > 0 {
					out = append(out, vendorMeta{ID: vid, DisplayName: vid, Models: list})
				}
			}
			writeJSON(w, http.StatusOK, out)
			return
		}

		writeJSON(w, http.StatusOK, []vendorMeta{})
	}
}

// ── assistants（Cherry 风格助手）───────────────────────────────────────────

// AssistantTemplate 预置助手模板
type AssistantTemplate struct {
	Name         string  `json:"name"`
	Emoji        string  `json:"emoji"`
	SystemPrompt string  `json:"system_prompt"`
	ModelID      string  `json:"model_id"`
	Temperature  float64 `json:"temperature"`
	IsDefault    bool    `json:"is_default"`
}

var DefaultAssistantTemplates = []AssistantTemplate{
	{
		Name:         "multi-llm-chat 智能助手",
		Emoji:        "AI",
		SystemPrompt: "你是一个乐于助人、知识渊博的 AI 助手。请用清晰、友好的语气回答用户的所有问题。",
		ModelID:      DefaultModel,
		Temperature:  0.7,
		IsDefault:    true,
	},
	{
		Name:         "全栈代码架构师",
		Emoji:        "Code",
		SystemPrompt: "你是一位资深全栈软件工程师和系统架构师。擅长 Go、Python、TypeScript/JavaScript、Rust、Docker 以及分布式系统设计。请提供规范、健壮、模块化、带详细注释并符合最佳实践的代码与重构建议。",
		ModelID:      DefaultModel,
		Temperature:  0.2,
	},
	{
		Name:         "长文润色与写作专家",
		Emoji:        "Write",
		SystemPrompt: "你是一位精通中英文写作与文学创作的资深主编。擅长文章润色、结构重构、语法纠错、学术论文润色以及营销软文写作。请保持行文流畅、用词精准、逻辑清晰。",
		ModelID:      DefaultModel,
		Temperature:  0.7,
	},
	{
		Name:         "深度逻辑与头脑风暴",
		Emoji:        "Think",
		SystemPrompt: "你是一位精通第一性原理的思维导师与头脑风暴专家。善于从多维度拆解复杂问题，提供批判性思考、创新方案以及清晰的逻辑推导流程。",
		ModelID:      DefaultModel,
		Temperature:  0.8,
	},
	{
		Name:         "多语言同传翻译官",
		Emoji:        "Trans",
		SystemPrompt: "你是一位专业的高级同传翻译官。精通中文、英语、日语、德语、法语等多国语言。请在翻译时做到信、达、雅，准确传达原文语气与文化内涵，并提供地道的对照翻译。",
		ModelID:      DefaultModel,
		Temperature:  0.3,
	},
	{
		Name:         "商业策划与文案顾问",
		Emoji:        "Biz",
		SystemPrompt: "你是一位拥有顶级咨询公司经验的商业策略顾问与文案操盘手。擅长撰写商业计划书、市场调研分析、用户画像挖掘、品牌宣传文案与增长策略。",
		ModelID:      DefaultModel,
		Temperature:  0.6,
	},
	{
		Name:         "技术面试官 & 职业导师",
		Emoji:        "Career",
		SystemPrompt: "你是一位硅谷大厂资深技术面试官与职业发展导师。能够根据用户目标岗位进行模拟技术面试、深入考察算法与系统设计、并给出中肯专业的简历修改建议。",
		ModelID:      DefaultModel,
		Temperature:  0.5,
	},
	{
		Name:         "创意灵感与故事构建师",
		Emoji:        "Creative",
		SystemPrompt: "你是一位富有想象力的世界观架构师与小说剧本创作者。擅长构建引人入胜的故事情节、丰满的角色设定、跌宕起伏的冲突与唯美的场景描写。",
		ModelID:      DefaultModel,
		Temperature:  0.9,
	},
}

// ensureDefaultAssistant 确保用户拥有一套精选预置助手。
func (d *Deps) ensureDefaultAssistant(userID string) {
	n, err := d.Store.CountAssistants(userID)
	if err != nil || n > 0 {
		return
	}
	for _, tpl := range DefaultAssistantTemplates {
		_, _ = d.Store.CreateAssistant(&store.Assistant{
			UserID:       userID,
			Name:         tpl.Name,
			Emoji:        tpl.Emoji,
			SystemPrompt: tpl.SystemPrompt,
			ModelID:      tpl.ModelID,
			Temperature:  tpl.Temperature,
			IsDefault:    tpl.IsDefault,
		})
	}
}

func (d *Deps) AssistantTemplatesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, DefaultAssistantTemplates)
	}
}

func (d *Deps) AssistantsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFromCtx(r)
		d.ensureDefaultAssistant(u.ID)
		list, err := d.Store.GetAssistants(u.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("internal error"))
			return
		}
		writeJSON(w, http.StatusOK, list)
	}
}

type assistantReq struct {
	Name         string  `json:"name"`
	Emoji        string  `json:"emoji"`
	SystemPrompt string  `json:"system_prompt"`
	ModelID      string  `json:"model_id"`
	Temperature  float64 `json:"temperature"`
}

func (r assistantReq) normalize() assistantReq {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		r.Name = "新助手"
	}
	if r.Emoji == "" {
		r.Emoji = "AI"
	}
	if strings.TrimSpace(r.ModelID) == "" {
		r.ModelID = DefaultModel
	}
	if r.Temperature < 0 || r.Temperature > 2 {
		r.Temperature = 0.7
	}
	return r
}

func (d *Deps) CreateAssistantHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFromCtx(r)
		var req assistantReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		req = req.normalize()
		id, err := d.Store.CreateAssistant(&store.Assistant{
			UserID: u.ID, Name: req.Name, Emoji: req.Emoji, SystemPrompt: req.SystemPrompt,
			ModelID: req.ModelID, Temperature: req.Temperature,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("创建失败"))
			return
		}
		a, _ := d.Store.GetAssistant(id, u.ID)
		writeJSON(w, http.StatusOK, a)
	}
}

func (d *Deps) UpdateAssistantHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFromCtx(r)
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid id"))
			return
		}
		existing, err := d.Store.GetAssistant(id, u.ID)
		if err != nil || existing == nil {
			writeJSON(w, http.StatusNotFound, errResp("助手不存在"))
			return
		}
		var req assistantReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		req = req.normalize()
		existing.Name = req.Name
		existing.Emoji = req.Emoji
		existing.SystemPrompt = req.SystemPrompt
		existing.ModelID = req.ModelID
		existing.Temperature = req.Temperature
		if err := d.Store.UpdateAssistant(existing); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("更新失败"))
			return
		}
		writeJSON(w, http.StatusOK, existing)
	}
}

func (d *Deps) DeleteAssistantHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFromCtx(r)
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid id"))
			return
		}
		if err := d.Store.DeleteAssistant(id, u.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("删除失败（默认助手不可删除）"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ── sessions ───────────────────────────────────────────────────────────────

type sessionResp struct {
	ID          int64  `json:"id"`
	Vendor      string `json:"vendor"`
	ModelID     string `json:"model_id"`
	ModelName   string `json:"model_name"`
	AssistantID int64  `json:"assistant_id"`
	Title       string `json:"title"`
	MsgCount    int    `json:"msg_count"`
	UpdatedAt   string `json:"updated_at"`
}

func (d *Deps) modelDisplayName(modelID string) string {
	if dbm, err := d.Store.GetModel(modelID); err == nil && dbm != nil && strings.TrimSpace(dbm.Name) != "" {
		return dbm.Name
	}
	if m, ok := models.Get(modelID); ok {
		return m.DisplayName
	}
	return modelID
}

func (d *Deps) toSessionResp(s store.ChatSession) sessionResp {
	return sessionResp{
		ID: s.ID, Vendor: s.Vendor, ModelID: s.ModelID, ModelName: d.modelDisplayName(s.ModelID),
		AssistantID: s.AssistantID, Title: s.Title, MsgCount: s.MsgCount,
		UpdatedAt: s.UpdatedAt.Format("2006-01-02 15:04"),
	}
}

func (d *Deps) SessionsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFromCtx(r)
		sessions, err := d.Store.GetChatSessions(u.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("internal error"))
			return
		}
		out := make([]sessionResp, 0, len(sessions))
		for _, s := range sessions {
			out = append(out, d.toSessionResp(s))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// CreateSessionHandler 从助手创建会话（携带模型 + 系统提示词快照）。
func (d *Deps) CreateSessionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFromCtx(r)
		var req struct {
			AssistantID int64  `json:"assistant_id"`
			ModelID     string `json:"model_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		d.ensureDefaultAssistant(u.ID)
		var a *store.Assistant
		if req.AssistantID > 0 {
			a, _ = d.Store.GetAssistant(req.AssistantID, u.ID)
		}
		if a == nil {
			// 回退到用户的第一个助手（默认助手）
			list, _ := d.Store.GetAssistants(u.ID)
			if len(list) == 0 {
				writeJSON(w, http.StatusBadRequest, errResp("请先创建助手"))
				return
			}
			a = &list[0]
		}
		wantModel := models.CanonicalID(strings.TrimSpace(req.ModelID))
		if wantModel == "" {
			wantModel = models.CanonicalID(a.ModelID)
		}
		var vendor, modelID, provName string
		if dbm, err := d.Store.GetModel(wantModel); err == nil && dbm != nil {
			vendor = dbm.Vendor
			modelID = dbm.ID
			provName = dbm.ProviderName
		} else if m, ok := models.Get(wantModel); ok {
			vendor = m.Vendor
			modelID = m.ID
			provName = m.Provider
		} else {
			vendor = models.InferVendor(wantModel)
			modelID = wantModel
			provName = "custom"
		}
		id, err := d.Store.CreateChatSession(u.ID, vendor, modelID, provName, a.ID, a.SystemPrompt, defaultTitle)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("internal error"))
			return
		}
		sess, _ := d.Store.GetChatSession(id, u.ID)
		writeJSON(w, http.StatusOK, d.toSessionResp(*sess))
	}
}

// RenameSessionHandler POST /api/sessions/{id}/title {title}
func (d *Deps) RenameSessionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFromCtx(r)
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid id"))
			return
		}
		sess, err := d.Store.GetChatSession(id, u.ID)
		if err != nil || sess == nil {
			writeJSON(w, http.StatusNotFound, errResp("session not found"))
			return
		}
		var req struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		title := strings.TrimSpace(req.Title)
		if title == "" {
			writeJSON(w, http.StatusBadRequest, errResp("标题不能为空"))
			return
		}
		if err := d.Store.UpdateSessionTitle(id, truncateRunes(title, 40)); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("重命名失败"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "title": truncateRunes(title, 40)})
	}
}

// UpdateSessionModelHandler POST /api/sessions/{id}/model {model_id}
func (d *Deps) UpdateSessionModelHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFromCtx(r)
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid id"))
			return
		}
		sess, err := d.Store.GetChatSession(id, u.ID)
		if err != nil || sess == nil {
			writeJSON(w, http.StatusNotFound, errResp("session not found"))
			return
		}
		var req struct {
			ModelID string `json:"model_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		wantModel := models.CanonicalID(strings.TrimSpace(req.ModelID))
		if wantModel == "" {
			writeJSON(w, http.StatusBadRequest, errResp("model_id is required"))
			return
		}
		var vendor, modelID, provName string
		if dbm, err := d.Store.GetModel(wantModel); err == nil && dbm != nil {
			vendor = dbm.Vendor
			modelID = dbm.ID
			provName = dbm.ProviderName
		} else if m, ok := models.Get(wantModel); ok {
			vendor = m.Vendor
			modelID = m.ID
			provName = m.Provider
		} else {
			vendor = models.InferVendor(wantModel)
			modelID = wantModel
			provName = "custom"
		}
		if err := d.Store.UpdateSessionModel(id, u.ID, vendor, modelID, provName); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("更新模型失败"))
			return
		}
		sess.Vendor = vendor
		sess.ModelID = modelID
		sess.Provider = provName
		writeJSON(w, http.StatusOK, d.toSessionResp(*sess))
	}
}

func (d *Deps) DeleteSessionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFromCtx(r)
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid id"))
			return
		}
		if err := d.Store.DeleteChatSession(id, u.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("delete failed"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

type messageResp struct {
	ID        int64   `json:"id"`
	Role      string  `json:"role"`
	Content   string  `json:"content"`
	CostUSD   float64 `json:"cost_usd"`
	Estimated bool    `json:"estimated"`
	CreatedAt string  `json:"created_at"`
}

func (d *Deps) MessagesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFromCtx(r)
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid id"))
			return
		}
		sess, err := d.Store.GetChatSession(id, u.ID)
		if err != nil || sess == nil {
			writeJSON(w, http.StatusNotFound, errResp("session not found"))
			return
		}
		msgs, err := d.Store.GetMessages(id, u.ID, 200)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("internal error"))
			return
		}
		out := make([]messageResp, 0, len(msgs))
		for _, m := range msgs {
			out = append(out, messageResp{
				ID: m.ID, Role: m.Role, Content: m.Content,
				CostUSD: m.CostUSD, Estimated: m.IsEstimatedCost,
				CreatedAt: m.CreatedAt.Format("2006-01-02 15:04"),
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// ── streaming chat ─────────────────────────────────────────────────────────

func (d *Deps) StreamChatHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFromCtx(r)
		var req struct {
			SessionID int64    `json:"session_id"`
			Content   string   `json:"content"`
			Images    []string `json:"images"`
			WebSearch bool     `json:"web_search"`
			Thinking  bool     `json:"thinking"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		content := strings.TrimSpace(req.Content)
		if content == "" && len(req.Images) == 0 {
			writeJSON(w, http.StatusBadRequest, errResp("content is empty"))
			return
		}
		if content == "" && len(req.Images) > 0 {
			content = "请分析这张图片的内容。"
		}
		sess, err := d.Store.GetChatSession(req.SessionID, u.ID)
		if err != nil || sess == nil {
			writeJSON(w, http.StatusNotFound, errResp("session not found"))
			return
		}

		// 每日额度 / Pro 门禁（管理员与 unlimited 账号跳过）。
		if !u.Unlimited && !u.IsAdmin {
			if limit, pats, label := hourlyRateLimit(sess.ModelID); limit > 0 {
				oneHourAgo := time.Now().Add(-1 * time.Hour)
				countLimit, _ := d.Store.CountModelsMessagesSince(u.ID, pats, oneHourAgo)
				if countLimit >= limit {
					writeJSON(w, http.StatusTooManyRequests, map[string]any{
						"error": fmt.Sprintf("%s 每小时限 %d 条，过去 1 小时已达上限，请稍后再试或换一个模型。", label, limit),
					})
					return
				}
			}

			lim := d.limits()
			now := time.Now()
			byModel, _ := d.Store.CountMessagesByModelSince(u.ID, billing.DayStart(now))
			dec := billing.Check(sess.ModelID, u.ProActive(), byModel, lim, now)
			if !dec.Allowed {
				writeJSON(w, http.StatusPaymentRequired, map[string]any{
					"error":      dec.Reason,
					"needs_pro":  dec.NeedsPro,
					"tier":       dec.Tier,
					"used":       dec.Used,
					"limit":      dec.Limit,
					"next_reset": dec.NextReset.UTC().Format(time.RFC3339),
					"paywall":    true,
				})
				return
			}
		}

		// 助手温度（快照 system_prompt 存在 session 上；温度实时取助手，删则默认）。
		temp := 0.7
		if sess.AssistantID > 0 {
			if a, _ := d.Store.GetAssistant(sess.AssistantID, u.ID); a != nil {
				temp = a.Temperature
			}
		}

		savedContent := content
		for _, img := range req.Images {
			if strings.HasPrefix(img, "data:") || strings.HasPrefix(img, "http") {
				savedContent += fmt.Sprintf("\n\n![image](%s)", img)
			}
		}

		if err := d.Store.SaveMessage(&store.ChatMessage{
			SessionID: sess.ID, UserID: u.ID, Role: "user", Content: savedContent, ModelID: sess.ModelID,
		}); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("save failed"))
			return
		}
		_ = d.Store.IncrSessionMsgCount(sess.ID)
		if sess.Title == defaultTitle {
			if t := titleFromUser(content); t != defaultTitle {
				_ = d.Store.UpdateSessionTitle(sess.ID, t)
				sess.Title = t
			}
		}

		// 联网搜索支持
		var searchContext string
		if req.WebSearch {
			if sRes, sErr := websearch.Search(r.Context(), content); sErr == nil && len(sRes) > 0 {
				searchContext = websearch.FormatPrompt(sRes)
			}
		}

		history, _ := d.Store.GetMessages(sess.ID, u.ID, MaxHistory)
		chatMsgs := make([]provider.Message, 0, len(history)+2)

		var sysParts []string
		sysParts = append(sysParts, GlobalBaseSystemPrompt)
		if strings.TrimSpace(sess.SystemPrompt) != "" {
			sysParts = append(sysParts, "【当前助手专属角色设定】：\n"+sess.SystemPrompt)
		}
		if req.Thinking {
			sysParts = append(sysParts, "【深度思考模式已开启】：在正式回答用户问题之前，请先进行严谨、系统、多步的深入推导与思考。请将所有的推演与思考过程严格写在 <think> 与 </think> 标签之间，思考完毕后再输出清晰规范的最终回答。")
		}
		if searchContext != "" {
			sysParts = append(sysParts, searchContext)
		}
		sysPrompt := strings.Join(sysParts, "\n\n")

		if sysPrompt != "" {
			chatMsgs = append(chatMsgs, provider.Message{Role: "system", Content: sysPrompt})
		}
		// 历史消息
		for i, mm := range history {
			// 如果是最后一条消息（刚才存入的 user 消息），把当前 req.Images 放进 message
			if i == len(history)-1 && mm.Role == "user" {
				chatMsgs = append(chatMsgs, provider.Message{Role: mm.Role, Content: content, Images: req.Images})
			} else {
				chatMsgs = append(chatMsgs, provider.Message{Role: mm.Role, Content: mm.Content})
			}
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		flusher, canFlush := w.(http.Flusher)

		metaJSON, _ := json.Marshal(map[string]any{"session_id": sess.ID})
		fmt.Fprintf(w, "event: meta\ndata: %s\n\n", metaJSON)
		if canFlush {
			flusher.Flush()
		}

		// 构建降级备选链：首选原模型，失败时按优先级自动降级
		usableModels, _ := d.Store.ListUsableModels()
		candidates := append([]string{sess.ModelID}, FallbackCandidates(sess.ModelID, usableModels)...)

		maxTok := maxAnswerToken
		var finalResult *provider.ChatResult
		var actualModelID string = sess.ModelID
		var downgradeNoticeSent bool = false
		var lastErr error

		for _, curModelID := range candidates {
			curProv, curRawModelID, provErr := d.providerForID(curModelID)
			if provErr != nil {
				lastErr = provErr
				continue
			}

			curReq := provider.ChatRequest{
				Model: curRawModelID, Messages: chatMsgs, MaxTokens: &maxTok,
				Temperature: &temp, IncludeUsage: true,
				Thinking: req.Thinking, WebSearch: req.WebSearch,
			}

			// 如果不是用户初始请求的模型，说明触发了自动降级，向用户下发友好降级卡片提示
			if curModelID != sess.ModelID && !downgradeNoticeSent {
				origName := d.modelDisplayName(sess.ModelID)
				fbName := d.modelDisplayName(curModelID)
				reasonStr := ""
				if lastErr != nil {
					reasonStr = lastErr.Error()
				}
				notice := FormatDowngradeNotice(origName, fbName, reasonStr)
				synth, _ := json.Marshal(map[string]any{
					"choices": []map[string]any{{"delta": map[string]any{"content": notice}}},
				})
				fmt.Fprintf(w, "data: %s\n\n", synth)
				if canFlush {
					flusher.Flush()
				}
				downgradeNoticeSent = true
			}

			var chunksSent int
			res, streamErr := curProv.StreamChatCompletion(r.Context(), curReq, func(rawData string) {
				chunksSent++
				fmt.Fprintf(w, "data: %s\n\n", rawData)
				if canFlush {
					flusher.Flush()
				}
			})

			if streamErr != nil {
				slog.Warn("model stream attempt failed", "model", curModelID, "chunks", chunksSent, "err", streamErr)
				// 若未输出任何内容，且不是超时或认证/404错误，可尝试一次非流式补全
				errStr := strings.ToLower(streamErr.Error())
				if chunksSent == 0 && !strings.Contains(errStr, "timeout") && !strings.Contains(errStr, "deadline") && !strings.Contains(errStr, "404") && !strings.Contains(errStr, "401") {
					res, streamErr = curProv.ChatCompletion(r.Context(), curReq)
					if streamErr == nil && res != nil && strings.TrimSpace(res.Content) != "" {
						synth, _ := json.Marshal(map[string]any{
							"choices": []map[string]any{{"delta": map[string]any{"content": res.Content}}},
						})
						fmt.Fprintf(w, "data: %s\n\n", synth)
						if canFlush {
							flusher.Flush()
						}
					}
				}
			}

			if streamErr == nil && res != nil && (strings.TrimSpace(res.Content) != "" || chunksSent > 0) {
				finalResult = res
				actualModelID = curModelID
				break
			}

			lastErr = streamErr
		}

		if finalResult == nil {
			slog.Error("all model fallbacks failed", "orig_model", sess.ModelID, "last_err", lastErr)
			errText := "所有可用模型均暂时无法连通或响应异常"
			if lastErr != nil {
				errText += "：" + lastErr.Error()
			}
			escaped, _ := json.Marshal(errText)
			fmt.Fprintf(w, "event: error\ndata: {\"error\":%s}\n\n", escaped)
			if canFlush {
				flusher.Flush()
			}
			return
		}

		fmt.Fprintf(w, "data: [DONE]\n\n")
		if canFlush {
			flusher.Flush()
		}

		am := &store.ChatMessage{
			SessionID: sess.ID,
			UserID:    u.ID,
			Role:      "assistant",
			Content:   finalResult.Content,
			ModelID:   actualModelID,
		}
		if finalResult.Usage != nil {
			am.PromptTokens = finalResult.Usage.PromptTokens
			am.CompletionTokens = finalResult.Usage.CompletionTokens
			am.CostUSD = d.Pricing.Cost(actualModelID, am.PromptTokens, am.CompletionTokens)
		} else {
			est := estimateTokens(chatMsgs)
			comp := len([]rune(finalResult.Content)) / 2
			am.PromptTokens = est
			am.CompletionTokens = comp
			am.CostUSD = d.Pricing.Cost(actualModelID, est, comp)
			am.IsEstimatedCost = true
		}
		if err := d.Store.SaveMessage(am); err != nil {
			slog.Error("save assistant message", "err", err)
		}
		_ = d.Store.IncrSessionMsgCount(sess.ID)
		_ = d.Store.TouchSession(sess.ID)

		if sess.Title == defaultTitle || sess.MsgCount <= 2 {
			d.generateTitleAsync(sess.ID, content, finalResult.Content)
		}
	}
}

func hourlyRateLimit(modelID string) (limit int, patterns []string, label string) {
	id := strings.ToLower(models.CanonicalID(modelID) + " " + modelID)
	switch {
	case strings.Contains(id, "supergrok") || strings.Contains(id, "grok-4.6") || strings.Contains(id, "grok-4.20"):
		return 600, []string{"supergrok", "grok-4.6", "grok-4.20"}, "Supergrok"
	case strings.Contains(id, "grok"):
		return 1200, []string{"grok-4.3", "grok-2"}, "Grok"
	case strings.Contains(id, "gemini") && strings.Contains(id, "pro"):
		return 600, []string{"gemini-2.5-pro", "gemini-pro"}, "Gemini Pro"
	case strings.Contains(id, "gemini"):
		return 1200, []string{"gemini-3.7-flash", "gemini-3.5-flash", "gemini-2.5-flash"}, "Gemini Flash"
	default:
		return 0, nil, ""
	}
}

func estimateTokens(msgs []provider.Message) int {
	total := 0
	for _, m := range msgs {
		total += len([]rune(m.Content)) / 2
	}
	return total
}

func (d *Deps) generateTitleAsync(sessionID int64, userMsg, aiReply string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		prov, rawTitleModel, err := d.providerForID(d.TitleModel)
		if err != nil {
			return
		}
		prompt := fmt.Sprintf(
			"根据下面的对话，起一个不超过 12 个字的简短标题，只返回标题本身，不要标点或引号。\n\n用户：%s\n助手：%s",
			truncateRunes(userMsg, 200), truncateRunes(aiReply, 200))
		maxTok := 32
		temp := 0.3
		res, err := prov.ChatCompletion(ctx, provider.ChatRequest{
			Model: rawTitleModel, Messages: []provider.Message{{Role: "user", Content: prompt}},
			MaxTokens: &maxTok, Temperature: &temp,
		})
		if err != nil {
			return
		}
		title := strings.TrimSpace(res.Content)
		title = strings.Trim(title, "\"'“”「」。.")
		if title == "" {
			return
		}
		_ = d.Store.UpdateSessionTitle(sessionID, truncateRunes(title, 20))
	}()
}

func titleFromUser(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if s == "" {
		return defaultTitle
	}
	return truncateRunes(s, 18)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
