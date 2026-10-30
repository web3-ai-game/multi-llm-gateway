package admin

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "modernc.org/sqlite"

	"github.com/go-chi/chi/v5"

	"deepweay-chat/internal/auth"
	"deepweay-chat/internal/billing"
	"deepweay-chat/internal/chat"
	"deepweay-chat/internal/models"
	"deepweay-chat/internal/pricing"
	"deepweay-chat/internal/store"
)

// Admin 提供 admin-only handlers。
type Admin struct {
	Store   *store.Store
	Auth    *auth.Auth
	Pricing *pricing.Cache
}

func New(st *store.Store, a *auth.Auth, p *pricing.Cache) *Admin {
	return &Admin{Store: st, Auth: a, Pricing: p}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func errResp(msg string) map[string]string { return map[string]string{"error": msg} }

func actorOf(r *http.Request) *store.User { return store.UserFromCtx(r.Context()) }

func (a *Admin) loadTarget(w http.ResponseWriter, r *http.Request) (*store.User, bool) {
	target, err := a.Store.GetUser(chi.URLParam(r, "id"))
	if err != nil || target == nil {
		writeJSON(w, http.StatusNotFound, errResp("用户不存在"))
		return nil, false
	}
	return target, true
}

func denyIfCannotAct(w http.ResponseWriter, actor, target *store.User) bool {
	if !store.CanActOn(actor, target) {
		writeJSON(w, http.StatusForbidden, errResp("不能操作同级或更高权限的账号"))
		return true
	}
	return false
}

// ── 用户管理 ─────────────────────────────────────────────────────────────

func (a *Admin) ListUsersHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := a.Store.ListUsersWithStats(billing.DayStart(time.Now()))
		if err != nil {
			slog.Error("list users", "err", err)
			writeJSON(w, http.StatusInternalServerError, errResp("internal error"))
			return
		}
		writeJSON(w, http.StatusOK, users)
	}
}

func (a *Admin) CreateUserHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username string  `json:"username"`
			Password string  `json:"password"`
			QuotaUSD float64 `json:"quota_usd"`
			Role     string  `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		username := strings.TrimSpace(req.Username)
		if username == "" {
			writeJSON(w, http.StatusBadRequest, errResp("用户名不能为空"))
			return
		}
		if len(req.Password) < auth.MinPasswordLen {
			writeJSON(w, http.StatusBadRequest, errResp("密码至少 6 位"))
			return
		}
		if req.QuotaUSD < 0 {
			req.QuotaUSD = 0
		}
		role := store.NormalizeRole(req.Role)
		if role != store.RoleUser && !store.CanAssignRole(actorOf(r), role) {
			writeJSON(w, http.StatusForbidden, errResp("不能授予该角色"))
			return
		}
		if existing, _ := a.Store.GetUserByUsername(username); existing != nil {
			writeJSON(w, http.StatusConflict, errResp("用户名已存在"))
			return
		}
		u, err := a.Auth.CreateUser(username, req.Password, req.QuotaUSD)
		if err != nil {
			slog.Error("create user", "err", err)
			writeJSON(w, http.StatusInternalServerError, errResp("创建失败"))
			return
		}
		if role != store.RoleUser {
			if err := a.Store.SetUserRole(u.ID, role); err != nil {
				slog.Error("set created user role", "err", err)
				role = store.RoleUser
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": u.ID, "username": u.Username, "role": role})
	}
}

func (a *Admin) ToggleDisableHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		me := actorOf(r)
		target, ok := a.loadTarget(w, r)
		if !ok {
			return
		}
		if target.ID == me.ID {
			writeJSON(w, http.StatusBadRequest, errResp("不能禁用自己"))
			return
		}
		if denyIfCannotAct(w, me, target) {
			return
		}
		if !target.IsDisabled && target.RoleName() == store.RoleOwner {
			if n, _ := a.Store.CountByRole(store.RoleOwner); n <= 1 {
				writeJSON(w, http.StatusBadRequest, errResp("不能禁用唯一站长"))
				return
			}
		}
		if err := a.Store.SetDisabled(target.ID, !target.IsDisabled); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("操作失败"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": target.ID, "is_disabled": !target.IsDisabled})
	}
}

// SetQuotaHandler POST /api/admin/users/{id}/quota {quota_usd, unlimited}
func (a *Admin) SetQuotaHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target, ok := a.loadTarget(w, r)
		if !ok {
			return
		}
		if denyIfCannotAct(w, actorOf(r), target) {
			return
		}
		var req struct {
			QuotaUSD  float64 `json:"quota_usd"`
			Unlimited bool    `json:"unlimited"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		if req.QuotaUSD < 0 {
			req.QuotaUSD = 0
		}
		if err := a.Store.SetUserQuota(target.ID, req.QuotaUSD, req.Unlimited); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("设置失败"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": target.ID, "quota_usd": req.QuotaUSD, "unlimited": req.Unlimited})
	}
}

// SetRoleHandler POST /api/admin/users/{id}/role {role}
func (a *Admin) SetRoleHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		me := actorOf(r)
		target, ok := a.loadTarget(w, r)
		if !ok {
			return
		}
		if target.ID == me.ID {
			writeJSON(w, http.StatusBadRequest, errResp("不能修改自己的角色"))
			return
		}
		if denyIfCannotAct(w, me, target) {
			return
		}
		var req struct {
			Role string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		role := store.NormalizeRole(req.Role)
		if role == store.RoleOwner {
			writeJSON(w, http.StatusForbidden, errResp("站长角色不可授予，由启动账号持有"))
			return
		}
		if !store.CanAssignRole(me, role) {
			writeJSON(w, http.StatusForbidden, errResp("不能授予该角色"))
			return
		}
		if target.RoleName() == store.RoleOwner {
			if n, _ := a.Store.CountByRole(store.RoleOwner); n <= 1 {
				writeJSON(w, http.StatusBadRequest, errResp("不能降级唯一站长"))
				return
			}
		}
		if err := a.Store.SetUserRole(target.ID, role); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("设置失败"))
			return
		}
		slog.Info("admin set role", "actor", me.ID, "user", target.ID, "role", role)
		writeJSON(w, http.StatusOK, map[string]any{"id": target.ID, "role": role})
	}
}

// ResetPasswordHandler POST /api/admin/users/{id}/password {password}
func (a *Admin) ResetPasswordHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		me := actorOf(r)
		target, ok := a.loadTarget(w, r)
		if !ok {
			return
		}
		if denyIfCannotAct(w, me, target) {
			return
		}
		var req struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		if len(req.Password) < auth.MinPasswordLen {
			writeJSON(w, http.StatusBadRequest, errResp("密码至少 6 位"))
			return
		}
		if err := a.Auth.SetPassword(target.ID, req.Password); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("重置失败"))
			return
		}
		slog.Info("admin reset password", "actor", me.ID, "user", target.ID)
		writeJSON(w, http.StatusOK, map[string]any{"id": target.ID, "status": "ok"})
	}
}

func (a *Admin) StatsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := a.Store.GetSiteStats()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("internal error"))
			return
		}
		writeJSON(w, http.StatusOK, st)
	}
}

// ── Provider 配置（OpenRouter / sub2api key）────────────────────────────────

func mask(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 10 {
		return "••••"
	}
	return s[:6] + "••••" + s[len(s)-4:]
}

// GetConfigHandler GET /api/admin/config — 返回脱敏预览 + 是否已设置。
func (a *Admin) GetConfigHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg, _ := a.Store.GetAllConfig()
		writeJSON(w, http.StatusOK, map[string]any{
			"openrouter_key_set":     cfg["openrouter_api_key"] != "",
			"openrouter_key_preview": mask(cfg["openrouter_api_key"]),
			"sub2api_base_url":       cfg["sub2api_base_url"],
			"sub2api_key_set":        cfg["sub2api_api_key"] != "",
			"sub2api_key_preview":    mask(cfg["sub2api_api_key"]),
		})
	}
}

// ── 商业化：充值 / Pro / 收款配置 / 定价表 ────────────────────────────────

// AddBalanceHandler POST /api/admin/users/{id}/balance {amount}
// 人工核验充值：用户付款后邮件联系，管理员在此加余额。
func (a *Admin) AddBalanceHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target, ok := a.loadTarget(w, r)
		if !ok {
			return
		}
		if denyIfCannotAct(w, actorOf(r), target) {
			return
		}
		id := target.ID
		var req struct {
			Amount float64 `json:"amount"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		if req.Amount == 0 {
			writeJSON(w, http.StatusBadRequest, errResp("金额不能为 0"))
			return
		}
		bal, err := a.Store.AddBalance(id, req.Amount)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("充值失败"))
			return
		}
		slog.Info("admin credited balance", "user", id, "amount", req.Amount, "new_balance", bal)
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "balance_usd": bal})
	}
}

// SetProHandler POST /api/admin/users/{id}/pro {is_pro, days}
// days>0 表示有效期天数；days<=0 且 is_pro=true 表示永久。
func (a *Admin) SetProHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target, ok := a.loadTarget(w, r)
		if !ok {
			return
		}
		if denyIfCannotAct(w, actorOf(r), target) {
			return
		}
		id := target.ID
		var req struct {
			IsPro bool `json:"is_pro"`
			Days  int  `json:"days"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		var until *time.Time
		if req.IsPro && req.Days > 0 {
			t := time.Now().AddDate(0, 0, req.Days)
			until = &t
		}
		if err := a.Store.SetPro(id, req.IsPro, until); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("设置失败"))
			return
		}
		slog.Info("admin set pro", "user", id, "is_pro", req.IsPro, "days", req.Days)
		resp := map[string]any{"id": id, "is_pro": req.IsPro}
		if until != nil {
			resp["pro_until"] = until.Format("2006-01-02")
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// GetPayConfigHandler GET /api/admin/payconfig
func (a *Admin) GetPayConfigHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg, _ := a.Store.GetAllConfig()
		lim := billing.ParseLimits(cfg[billing.KeyLimits])
		writeJSON(w, http.StatusOK, map[string]any{
			"alipay_qr":     cfg[billing.KeyAlipayQR],
			"wechat_qr":     cfg[billing.KeyWechatQR],
			"googlepay_on":  cfg[billing.KeyGooglePayOn] == "1",
			"contact_email": strOr(cfg[billing.KeyContactEmail], billing.DefaultContactEmail),
			"pro_price":     floatOr(cfg[billing.KeyProPrice], billing.DefaultProPrice),
			"min_topup":     floatOr(cfg[billing.KeyMinTopup], billing.DefaultMinTopup),
			"soft_nudge_at": atoiOr(cfg[billing.KeySoftNudgeAt], billing.DefaultSoftNudgeAt),
			"limits":        lim,
		})
	}
}

// SetPayConfigHandler POST /api/admin/payconfig — 图片为 data URI；空串表示不改动。
func (a *Admin) SetPayConfigHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			AlipayQR     *string         `json:"alipay_qr"`
			WechatQR     *string         `json:"wechat_qr"`
			GooglePayOn  *bool           `json:"googlepay_on"`
			ContactEmail *string         `json:"contact_email"`
			ProPrice     *float64        `json:"pro_price"`
			MinTopup     *float64        `json:"min_topup"`
			SoftNudgeAt  *int            `json:"soft_nudge_at"`
			Limits       *billing.Limits `json:"limits"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		// 收款码：允许传空字符串来「清空」，用 nil 表示不改动
		if req.AlipayQR != nil {
			_ = a.Store.SetConfig(billing.KeyAlipayQR, *req.AlipayQR)
		}
		if req.WechatQR != nil {
			_ = a.Store.SetConfig(billing.KeyWechatQR, *req.WechatQR)
		}
		if req.GooglePayOn != nil {
			v := "0"
			if *req.GooglePayOn {
				v = "1"
			}
			_ = a.Store.SetConfig(billing.KeyGooglePayOn, v)
		}
		if req.ContactEmail != nil && strings.TrimSpace(*req.ContactEmail) != "" {
			_ = a.Store.SetConfig(billing.KeyContactEmail, strings.TrimSpace(*req.ContactEmail))
		}
		if req.ProPrice != nil && *req.ProPrice > 0 {
			_ = a.Store.SetConfig(billing.KeyProPrice, strconv.FormatFloat(*req.ProPrice, 'f', 2, 64))
		}
		if req.MinTopup != nil && *req.MinTopup > 0 {
			_ = a.Store.SetConfig(billing.KeyMinTopup, strconv.FormatFloat(*req.MinTopup, 'f', 2, 64))
		}
		if req.SoftNudgeAt != nil && *req.SoftNudgeAt > 0 {
			_ = a.Store.SetConfig(billing.KeySoftNudgeAt, strconv.Itoa(*req.SoftNudgeAt))
		}
		if req.Limits != nil {
			if b, err := json.Marshal(*req.Limits); err == nil {
				_ = a.Store.SetConfig(billing.KeyLimits, string(b))
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// PricingHandler GET /api/admin/pricing — 只列出我们支持的模型 + OpenRouter 实时单价。
func (a *Admin) PricingHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type row struct {
			ID         string  `json:"id"`
			Name       string  `json:"name"`
			Vendor     string  `json:"vendor"`
			Tier       string  `json:"tier"`
			PromptM    float64 `json:"prompt_per_m"`     // 每百万 prompt token 美元
			Completion float64 `json:"completion_per_m"` // 每百万 completion token 美元
			Known      bool    `json:"known"`            // 是否拿到实时价（否则为兜底价）
		}
		out := []row{}
		for _, v := range models.Vendors {
			for _, m := range models.Registry[v.ID] {
				p, known := a.Pricing.Lookup(m.ID)
				out = append(out, row{
					ID: m.ID, Name: m.DisplayName, Vendor: v.DisplayName, Tier: m.Tier,
					PromptM: p.PromptPerToken * 1e6, Completion: p.CompletionPerToken * 1e6,
					Known: known,
				})
			}
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func strOr(s, def string) string {
	if strings.TrimSpace(s) != "" {
		return s
	}
	return def
}

func floatOr(s string, def float64) float64 {
	if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && f > 0 {
		return f
	}
	return def
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
		return n
	}
	return def
}

// SetConfigHandler POST /api/admin/config — 只更新非空字段（空=保留原值）。
func (a *Admin) SetConfigHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OpenRouterKey *string `json:"openrouter_api_key"`
			Sub2APIBase   *string `json:"sub2api_base_url"`
			Sub2APIKey    *string `json:"sub2api_api_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		// base_url 允许清空；key 为空字符串表示“不改动”。
		if req.OpenRouterKey != nil && strings.TrimSpace(*req.OpenRouterKey) != "" {
			_ = a.Store.SetConfig("openrouter_api_key", strings.TrimSpace(*req.OpenRouterKey))
		}
		if req.Sub2APIBase != nil {
			_ = a.Store.SetConfig("sub2api_base_url", strings.TrimSpace(*req.Sub2APIBase))
		}
		if req.Sub2APIKey != nil && strings.TrimSpace(*req.Sub2APIKey) != "" {
			_ = a.Store.SetConfig("sub2api_api_key", strings.TrimSpace(*req.Sub2APIKey))
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ── 服务商管理 (Cherry Studio 风格多 Provider) ────────────────────────────────

func (a *Admin) ListProvidersHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := a.Store.ListProviders()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("list providers: "+err.Error()))
			return
		}
		type respItem struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			Type       string `json:"type"`
			BaseURL    string `json:"base_url"`
			APIKey     string `json:"api_key"`
			KeyPreview string `json:"key_preview"`
			IsEnabled  bool   `json:"is_enabled"`
			ModelCount int    `json:"model_count"`
			CreatedAt  string `json:"created_at"`
		}
		out := make([]respItem, 0, len(list))
		for _, p := range list {
			out = append(out, respItem{
				ID:         p.ID,
				Name:       p.Name,
				Type:       p.Type,
				BaseURL:    p.BaseURL,
				APIKey:     p.APIKey,
				KeyPreview: mask(p.APIKey),
				IsEnabled:  p.IsEnabled,
				ModelCount: p.ModelCount,
				CreatedAt:  p.CreatedAt.Format("2006-01-02 15:04"),
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func (a *Admin) CreateProviderHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name      string `json:"name"`
			Type      string `json:"type"`
			BaseURL   string `json:"base_url"`
			APIKey    string `json:"api_key"`
			IsEnabled *bool  `json:"is_enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		req.BaseURL = strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
		if req.Name == "" || req.BaseURL == "" {
			writeJSON(w, http.StatusBadRequest, errResp("服务商名称与 Base URL 不能为空"))
			return
		}
		en := true
		if req.IsEnabled != nil {
			en = *req.IsEnabled
		}
		ptype := strings.TrimSpace(req.Type)
		if ptype == "" {
			if strings.Contains(strings.ToLower(req.BaseURL), "openrouter") {
				ptype = "openrouter"
			} else {
				ptype = "openai_compatible"
			}
		}
		p := &store.Provider{
			Name:      req.Name,
			Type:      ptype,
			BaseURL:   req.BaseURL,
			APIKey:    strings.TrimSpace(req.APIKey),
			IsEnabled: en,
		}
		id, err := a.Store.CreateProvider(p)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("create provider: "+err.Error()))
			return
		}
		p.ID = id
		probe := a.probeAndActivate(p)
		writeJSON(w, http.StatusOK, providerJSON(p, &probe))
	}
}

func (a *Admin) UpdateProviderHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid id"))
			return
		}
		var req struct {
			Name      string `json:"name"`
			Type      string `json:"type"`
			BaseURL   string `json:"base_url"`
			APIKey    string `json:"api_key"`
			IsEnabled *bool  `json:"is_enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		existing, err := a.Store.GetProvider(id)
		if err != nil || existing == nil {
			writeJSON(w, http.StatusNotFound, errResp("provider not found"))
			return
		}
		if strings.TrimSpace(req.Name) != "" {
			existing.Name = strings.TrimSpace(req.Name)
		}
		if strings.TrimSpace(req.BaseURL) != "" {
			existing.BaseURL = strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
		}
		if strings.TrimSpace(req.Type) != "" {
			existing.Type = strings.TrimSpace(req.Type)
		} else if existing.Type == "" {
			if strings.Contains(strings.ToLower(existing.BaseURL), "openrouter") {
				existing.Type = "openrouter"
			} else {
				existing.Type = "openai_compatible"
			}
		}
		if req.APIKey != "" {
			existing.APIKey = strings.TrimSpace(req.APIKey)
		}
		if req.IsEnabled != nil {
			existing.IsEnabled = *req.IsEnabled
		}
		if err := a.Store.UpdateProvider(existing); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("update provider: "+err.Error()))
			return
		}
		probe := a.probeAndActivate(existing)
		writeJSON(w, http.StatusOK, providerJSON(existing, &probe))
	}
}

func (a *Admin) ToggleProviderHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid id"))
			return
		}
		var req struct {
			IsEnabled bool `json:"is_enabled"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if err := a.Store.ToggleProvider(id, req.IsEnabled); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("toggle provider: "+err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "is_enabled": req.IsEnabled})
	}
}

func (a *Admin) DeleteProviderHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid id"))
			return
		}
		if err := a.Store.DeleteProvider(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("delete provider: "+err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ── 模型自动探测 ────────────────────────────────────────────────────────────

type DetectedModel struct {
	ID          string `json:"id"`
	RawModelID  string `json:"raw_model_id"`
	Name        string `json:"name"`
	Vendor      string `json:"vendor"`
	Tier        string `json:"tier"`
	Description string `json:"description"`
}

func (a *Admin) DetectModelsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ProviderID *int64 `json:"provider_id"`
			BaseURL    string `json:"base_url"`
			APIKey     string `json:"api_key"`
			Type       string `json:"type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		baseURL := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
		apiKey := strings.TrimSpace(req.APIKey)
		var pID int64
		if req.ProviderID != nil && *req.ProviderID > 0 {
			pID = *req.ProviderID
			if p, err := a.Store.GetProvider(pID); err == nil && p != nil {
				if baseURL == "" {
					baseURL = strings.TrimRight(p.BaseURL, "/")
				}
				if apiKey == "" {
					apiKey = p.APIKey
				}
				if req.Type == "" {
					req.Type = p.Type
				}
			}
		}
		if baseURL == "" {
			writeJSON(w, http.StatusBadRequest, errResp("Base URL 不能为空"))
			return
		}

		detected, err := detectUpstreamModels(baseURL, apiKey, req.Type)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("探测失败: "+err.Error()))
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status":      "ok",
			"provider_id": pID,
			"count":       len(detected),
			"models":      detected,
		})
	}
}

func (a *Admin) ProbeProviderHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ProviderID *int64 `json:"provider_id"`
			BaseURL    string `json:"base_url"`
			APIKey     string `json:"api_key"`
			Type       string `json:"type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		baseURL := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
		apiKey := strings.TrimSpace(req.APIKey)
		pType := strings.TrimSpace(req.Type)
		var p *store.Provider
		if req.ProviderID != nil && *req.ProviderID > 0 {
			if got, err := a.Store.GetProvider(*req.ProviderID); err == nil && got != nil {
				p = got
				if baseURL == "" {
					baseURL = strings.TrimRight(p.BaseURL, "/")
				}
				if apiKey == "" {
					apiKey = p.APIKey
				}
				if pType == "" {
					pType = p.Type
				}
			}
		}
		if p == nil {
			p = &store.Provider{BaseURL: baseURL, APIKey: apiKey, Type: pType, IsEnabled: true}
		} else {
			p.BaseURL = baseURL
			p.APIKey = apiKey
			p.Type = pType
		}
		probe := a.probeAndActivate(p)
		writeJSON(w, http.StatusOK, probe)
	}
}

func detectUpstreamModels(baseURL, apiKey, pType string) ([]DetectedModel, error) {
	url := baseURL + "/models"
	httpReq, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if pType == "openrouter" || strings.Contains(strings.ToLower(baseURL), "openrouter") {
		httpReq.Header.Set("HTTP-Referer", "https://chat.example.com")
		httpReq.Header.Set("X-Title", "Deepweay AI")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("网络请求错误: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", friendlyUpstreamErr(resp.StatusCode, string(body)))
	}

	var raw struct {
		Data []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"data"`
		Models []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
			Description string `json:"description"`
		} `json:"models"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}

	var out []DetectedModel
	if len(raw.Data) > 0 {
		for _, item := range raw.Data {
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			displayName := item.Name
			if strings.TrimSpace(displayName) == "" {
				displayName = models.InferDisplayName(item.ID)
			}
			vendor := models.InferVendor(item.ID)
			tier := models.InferTier(item.ID)
			out = append(out, DetectedModel{
				ID:          item.ID,
				RawModelID:  item.ID,
				Name:        displayName,
				Vendor:      vendor,
				Tier:        tier,
				Description: item.Description,
			})
		}
	} else if len(raw.Models) > 0 {
		for _, item := range raw.Models {
			id := strings.TrimPrefix(item.Name, "models/")
			displayName := item.DisplayName
			if strings.TrimSpace(displayName) == "" {
				displayName = models.InferDisplayName(id)
			}
			vendor := models.InferVendor(id)
			tier := models.InferTier(id)
			out = append(out, DetectedModel{
				ID:          id,
				RawModelID:  id,
				Name:        displayName,
				Vendor:      vendor,
				Tier:        tier,
				Description: item.Description,
			})
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("未探测到有效模型列表，返回内容: %s", string(body))
	}

	return out, nil
}

// ── 模型管理 (Models Management) ───────────────────────────────────────────

func (a *Admin) ListModelsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := a.Store.ListAllModels()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("list models: "+err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, list)
	}
}

func (a *Admin) BatchSaveModelsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ProviderID int64             `json:"provider_id"`
			Models     []store.ModelItem `json:"models"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		if len(req.Models) == 0 {
			writeJSON(w, http.StatusBadRequest, errResp("模型列表不能为空"))
			return
		}
		for i := range req.Models {
			if req.ProviderID > 0 && req.Models[i].ProviderID == 0 {
				req.Models[i].ProviderID = req.ProviderID
			}
			if req.Models[i].RawModelID == "" {
				req.Models[i].RawModelID = req.Models[i].ID
			}
			if req.Models[i].Name == "" {
				req.Models[i].Name = models.InferDisplayName(req.Models[i].ID)
			}
			if req.Models[i].Vendor == "" {
				req.Models[i].Vendor = models.InferVendor(req.Models[i].ID)
			}
			if req.Models[i].Tier == "" {
				req.Models[i].Tier = models.InferTier(req.Models[i].ID)
			}
		}
		if err := a.Store.BatchSaveModels(req.Models); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("save models: "+err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "saved_count": len(req.Models)})
	}
}

func (a *Admin) UpdateModelHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req struct {
			ProviderID  int64  `json:"provider_id"`
			RawModelID  string `json:"raw_model_id"`
			Name        string `json:"name"`
			Vendor      string `json:"vendor"`
			Tier        string `json:"tier"`
			Description string `json:"description"`
			IsEnabled   bool   `json:"is_enabled"`
			SortOrder   int    `json:"sort_order"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		m := &store.ModelItem{
			ID:          id,
			ProviderID:  req.ProviderID,
			RawModelID:  req.RawModelID,
			Name:        req.Name,
			Vendor:      req.Vendor,
			Tier:        req.Tier,
			Description: req.Description,
			IsEnabled:   req.IsEnabled,
			SortOrder:   req.SortOrder,
		}
		if err := a.Store.SaveModel(m); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("update model: "+err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, m)
	}
}

func (a *Admin) ToggleModelHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req struct {
			IsEnabled bool `json:"is_enabled"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if err := a.Store.ToggleModel(id, req.IsEnabled); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("toggle model: "+err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "id": id, "is_enabled": req.IsEnabled})
	}
}

// ReorderModelsHandler 接收拖拽排序后的 id 列表，按数组下标写入 sort_order。
// 用于 admin 模型表的 HTML5 drag-and-drop 重排。
func (a *Admin) ReorderModelsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		if len(req.IDs) == 0 {
			writeJSON(w, http.StatusBadRequest, errResp("ids 不能为空"))
			return
		}
		if err := a.Store.ReorderModels(req.IDs); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("reorder models: "+err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "count": len(req.IDs)})
	}
}

func friendlyUpstreamErr(status int, body string) string {
	low := strings.ToLower(body)
	if status == http.StatusForbidden && (strings.Contains(low, "credit") || strings.Contains(low, "spending limit") || strings.Contains(low, "permission-denied")) {
		return "上游额度已用尽或达到月消费上限，探测被拒绝。API Key 已可单独保存；请点「仅启用精选 8 款」上架模型，或更换仍有额度的 Key。探测不是必须步骤。"
	}
	if status == http.StatusUnauthorized {
		return "API Key 无效或未授权，请检查后重新保存。"
	}
	msg := strings.TrimSpace(body)
	if len(msg) > 280 {
		msg = msg[:280] + "…"
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	return fmt.Sprintf("探测失败 HTTP %d：%s", status, msg)
}

func (a *Admin) ApplyFeaturedModelsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n, err := a.Store.ApplyFeaturedModels()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("启用精选模型失败: "+err.Error()))
			return
		}
		_ = a.Store.SetConfig(store.FeaturedAppliedKey, "1")
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "enabled": n})
	}
}

func (a *Admin) DeleteModelHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := a.Store.DeleteModel(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("delete model: "+err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ModelDiagnosis 是单个模型的诊断结果
type ModelDiagnosis struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Vendor       string   `json:"vendor"`
	Tier         string   `json:"tier"`
	ProviderID   int64    `json:"provider_id"`
	ProviderName string   `json:"provider_name"`
	RawModelID   string   `json:"raw_model_id"`
	IsEnabled    bool     `json:"is_enabled"`
	OK           bool     `json:"ok"`
	LatencyMS    int64    `json:"latency_ms"`
	Message      string   `json:"message"`
	Fallbacks    []string `json:"fallbacks"`
}

// DiagnoseModelsHandler GET /api/admin/models/diagnose — 并发测试所有模型的实际连通性
func (a *Admin) DiagnoseModelsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		allModels, err := a.Store.ListAllModels()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("list models: "+err.Error()))
			return
		}
		providers, _ := a.Store.ListProviders()
		pMap := make(map[int64]store.Provider)
		for _, p := range providers {
			pMap[p.ID] = p
		}

		usableModels, _ := a.Store.ListUsableModels()

		results := make([]ModelDiagnosis, len(allModels))
		var wg sync.WaitGroup
		sem := make(chan struct{}, 6) // 并发度限制为 6

		for i, m := range allModels {
			wg.Add(1)
			go func(idx int, item store.ModelItem) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				d := ModelDiagnosis{
					ID:           item.ID,
					Name:         item.Name,
					Vendor:       item.Vendor,
					Tier:         item.Tier,
					ProviderID:   item.ProviderID,
					ProviderName: item.ProviderName,
					RawModelID:   item.RawModelID,
					IsEnabled:    item.IsEnabled,
					Fallbacks:    chat.FallbackCandidates(item.ID, usableModels),
				}

				p, ok := pMap[item.ProviderID]
				if !ok || !p.IsEnabled || strings.TrimSpace(p.APIKey) == "" || strings.TrimSpace(p.BaseURL) == "" {
					d.OK = false
					d.Message = "未配置有效服务商或 Key 未填"
					results[idx] = d
					return
				}

				start := time.Now()
				rawModel := item.RawModelID
				if rawModel == "" {
					rawModel = item.ID
				}
				pBlob := strings.ToLower(p.BaseURL + " " + p.Name)
				if strings.Contains(pBlob, "x.ai") || strings.Contains(pBlob, "xai") || strings.Contains(pBlob, "grok") {
					rawModel = strings.TrimPrefix(rawModel, "x-ai/")
					rawModel = strings.TrimPrefix(rawModel, "xai/")
					if rawModel == "supergrok" || rawModel == "grok-4.6" {
						rawModel = "grok-4.6"
					} else if rawModel == "grok-2" || rawModel == "grok-4.3" || rawModel == "grok-3-mini" {
						rawModel = "grok-4.3"
					} else if rawModel == "grok-2-vision" {
						rawModel = "grok-4.6"
					}
				}
				err := probeChatCompletion(p.BaseURL, p.APIKey, p.Type, rawModel)
				d.LatencyMS = time.Since(start).Milliseconds()
				if err == nil {
					d.OK = true
					d.Message = "连通正常"
				} else {
					d.OK = false
					d.Message = err.Error()
				}
				results[idx] = d
			}(i, m)
		}
		wg.Wait()

		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"count":  len(results),
			"models": results,
		})
	}
}

// ── VPS 服务器状态监控 ───────────────────────────────────────────────────────

type SystemStatus struct {
	CPUPercent    float64 `json:"cpu_percent"`
	CPUCount      int     `json:"cpu_count"`
	MemTotalMB    uint64  `json:"mem_total_mb"`
	MemUsedMB     uint64  `json:"mem_used_mb"`
	MemPercent    float64 `json:"mem_percent"`
	DiskTotalGB   float64 `json:"disk_total_gb"`
	DiskUsedGB    float64 `json:"disk_used_gb"`
	DiskPercent   float64 `json:"disk_percent"`
	UptimeStr     string  `json:"uptime_str"`
	UptimeSeconds int64   `json:"uptime_seconds"`
	LoadAvg       string  `json:"load_avg"`
	AppAllocMB    float64 `json:"app_alloc_mb"`
	AppGoroutines int     `json:"app_goroutines"`
	OS            string  `json:"os"`
	Arch          string  `json:"arch"`
}

var (
	prevCPUTotal uint64
	prevCPUIdle  uint64
	cpuLock      sync.Mutex
)

func (a *Admin) SystemStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := SystemStatus{
			CPUCount:      runtime.NumCPU(),
			OS:            runtime.GOOS,
			Arch:          runtime.GOARCH,
			AppGoroutines: runtime.NumGoroutine(),
		}

		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		s.AppAllocMB = mathRound(float64(m.Alloc)/1024/1024, 2)

		// 1. CPU
		s.CPUPercent = getCPUPercent()

		// 2. Memory
		s.MemTotalMB, s.MemUsedMB, s.MemPercent = getMemInfo()

		// 3. Disk
		s.DiskTotalGB, s.DiskUsedGB, s.DiskPercent = getDiskInfo()

		// 4. Uptime
		s.UptimeSeconds, s.UptimeStr = getUptime()

		// 5. LoadAvg
		s.LoadAvg = getLoadAvg()

		writeJSON(w, http.StatusOK, s)
	}
}

// LogsHandler 返回最近 N 行服务日志（默认 200，最多 2000）。
// 优先用 journalctl（systemd），否则尝试常见日志文件路径，最后退回空列表。
func (a *Admin) LogsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lines := 200
		if v := r.URL.Query().Get("lines"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 2000 {
				lines = n
			}
		}
		out, source := fetchRecentLogs(lines)
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"source": source,
			"lines":  out,
			"count":  len(out),
		})
	}
}

func fetchRecentLogs(n int) ([]string, string) {
	if runtime.GOOS == "linux" {
		if data, err := exec.Command("journalctl", "-u", "deepweay-chat", "--no-pager", "-n", strconv.Itoa(n), "-o", "short").Output(); err == nil {
			txt := strings.TrimRight(string(data), "\n")
			if txt != "" {
				return strings.Split(txt, "\n"), "journalctl"
			}
		}
	}
	candidates := []string{
		"/var/log/deepweay-chat.err.log",
		"/var/log/deepweay-chat.out.log",
		"/tmp/deepweay-chat.log",
	}
	for _, p := range candidates {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		defer f.Close()
		data, err := io.ReadAll(f)
		if err != nil {
			continue
		}
		txt := strings.TrimRight(string(data), "\n")
		if txt == "" {
			return []string{}, p
		}
		all := strings.Split(txt, "\n")
		if len(all) > n {
			all = all[len(all)-n:]
		}
		return all, p
	}
	return []string{}, ""
}

// BackupHandler 把当前 SQLite 库复制成一份临时快照，清掉 chat_messages（隐私），
// 然后以 application/octet-stream 流式回传，Content-Disposition 带时间戳。
func (a *Admin) BackupHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		srcPath := a.Store.DBPath()
		if srcPath == "" {
			writeJSON(w, http.StatusInternalServerError, errResp("数据库路径未知"))
			return
		}
		tmp, err := os.CreateTemp("", "deepweay-backup-*.db")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("创建临时文件失败: "+err.Error()))
			return
		}
		tmpPath := tmp.Name()
		tmp.Close()
		defer os.Remove(tmpPath)

		// 先 cp 一份（SQLite + WAL 下安全），再单独打开清表。
		if err := copyFile(srcPath, tmpPath); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("复制数据库失败: "+err.Error()))
			return
		}
		// 顺手把同目录的 -wal / -shm 也拷过来，避免备份不完整。
		for _, suf := range []string{"-wal", "-shm"} {
			if _, err := os.Stat(srcPath + suf); err == nil {
				_ = copyFile(srcPath+suf, tmpPath+suf)
				defer os.Remove(tmpPath + suf)
			}
		}

		// 在副本上清空 chat_messages 并 VACUUM 回收空间。
		if err := stripChatMessages(tmpPath); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("清理聊天记录失败: "+err.Error()))
			return
		}

		f, err := os.Open(tmpPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("打开备份失败: "+err.Error()))
			return
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp("stat 备份失败: "+err.Error()))
			return
		}

		ts := time.Now().UTC().Format("20060102-150405")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="deepweay-backup-`+ts+`.db"`)
		w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		buf := make([]byte, 64*1024)
		for {
			n, rerr := f.Read(buf)
			if n > 0 {
				if _, werr := w.Write(buf[:n]); werr != nil {
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				return
			}
		}
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func stripChatMessages(path string) error {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM chat_messages`); err != nil {
		return err
	}
	if _, err := db.Exec(`VACUUM`); err != nil {
		return err
	}
	return nil
}

func getCPUPercent() float64 {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[0] == "cpu" {
			var total, idle uint64
			for i, valStr := range fields[1:] {
				val, _ := strconv.ParseUint(valStr, 10, 64)
				total += val
				if i == 3 { // idle is 4th field (index 3)
					idle = val
				}
			}

			cpuLock.Lock()
			defer cpuLock.Unlock()

			if prevCPUTotal > 0 && total > prevCPUTotal {
				totalDiff := float64(total - prevCPUTotal)
				idleDiff := float64(idle - prevCPUIdle)
				pct := (1.0 - idleDiff/totalDiff) * 100.0
				prevCPUTotal = total
				prevCPUIdle = idle
				if pct < 0 {
					return 0
				}
				if pct > 100 {
					return 100
				}
				return mathRound(pct, 1)
			}
			prevCPUTotal = total
			prevCPUIdle = idle
		}
	}
	return 0
}

func getMemInfo() (totalMB uint64, usedMB uint64, pct float64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, 0
	}
	defer f.Close()

	var memTotal, memAvailable uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			vStr := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(parts[1]), "kB"))
			v, _ := strconv.ParseUint(strings.Fields(vStr)[0], 10, 64)
			if k == "MemTotal" {
				memTotal = v
			} else if k == "MemAvailable" {
				memAvailable = v
			}
		}
	}

	if memTotal > 0 {
		totalMB = memTotal / 1024
		if memAvailable > 0 && memTotal >= memAvailable {
			usedMB = (memTotal - memAvailable) / 1024
		} else {
			usedMB = totalMB / 2
		}
		pct = mathRound(float64(usedMB)/float64(totalMB)*100, 1)
	}
	return totalMB, usedMB, pct
}

func getDiskInfo() (totalGB, usedGB, pct float64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return 0, 0, 0
	}
	totalBytes := stat.Blocks * uint64(stat.Bsize)
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	if totalBytes > 0 && totalBytes >= freeBytes {
		usedBytes := totalBytes - freeBytes
		totalGB = mathRound(float64(totalBytes)/1024/1024/1024, 1)
		usedGB = mathRound(float64(usedBytes)/1024/1024/1024, 1)
		pct = mathRound(usedGB/totalGB*100, 1)
	}
	return totalGB, usedGB, pct
}

func getUptime() (int64, string) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, "—"
	}
	parts := strings.Fields(string(data))
	if len(parts) == 0 {
		return 0, "—"
	}
	secFloat, _ := strconv.ParseFloat(parts[0], 64)
	sec := int64(secFloat)
	days := sec / 86400
	hours := (sec % 86400) / 3600
	mins := (sec % 3600) / 60
	var out string
	if days > 0 {
		out = strconv.FormatInt(days, 10) + "天 " + strconv.FormatInt(hours, 10) + "小时 " + strconv.FormatInt(mins, 10) + "分"
	} else if hours > 0 {
		out = strconv.FormatInt(hours, 10) + "小时 " + strconv.FormatInt(mins, 10) + "分"
	} else {
		out = strconv.FormatInt(mins, 10) + "分钟"
	}
	return sec, out
}

func getLoadAvg() string {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return "—"
	}
	parts := strings.Fields(string(data))
	if len(parts) >= 3 {
		return parts[0] + " " + parts[1] + " " + parts[2]
	}
	return "—"
}

func mathRound(val float64, precision int) float64 {
	p := 1.0
	for i := 0; i < precision; i++ {
		p *= 10.0
	}
	return float64(int(val*p+0.5)) / p
}
