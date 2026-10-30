package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"deepweay-chat/internal/store"
)

const CookieName = "dw_uid"
const MinPasswordLen = 6

var (
	errUserNotFound   = errors.New("user not found")
	errBadOldPassword = errors.New("旧密码错误")
)

// Auth 持有 store 引用并提供认证相关 handler + 中间件。
type Auth struct {
	Store *store.Store
}

func New(st *store.Store) *Auth { return &Auth{Store: st} }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func errResp(msg string) map[string]string { return map[string]string{"error": msg} }

// ── cookie helpers ─────────────────────────────────────────────────────────

func isSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if p := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); p != "" {
		return strings.EqualFold(p, "https") || strings.EqualFold(p, "wss")
	}
	return false
}

func setAuthCookie(w http.ResponseWriter, r *http.Request, userID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    userID,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 30,
	})
}

func clearAuthCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// GenerateUserID 从用户名派生稳定前缀 + 随机后缀。
func GenerateUserID(username string) string {
	safe := make([]byte, 0, 16)
	for _, c := range []byte(username) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			safe = append(safe, c)
		default:
			safe = append(safe, '_')
		}
		if len(safe) >= 16 {
			break
		}
	}
	if len(safe) == 0 {
		safe = []byte("u")
	}
	rnd := make([]byte, 4)
	_, _ = rand.Read(rnd)
	return "u_" + string(safe) + "_" + hex.EncodeToString(rnd)
}

// ── middleware ─────────────────────────────────────────────────────────────

// Middleware 是 /api/* 的认证中间件：失败返回 401 JSON。
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := a.userFromCookie(r)
		if u == nil || u.IsDisabled {
			writeJSON(w, http.StatusUnauthorized, errResp("unauthorized"))
			return
		}
		ctx := store.SetUserInCtx(r.Context(), u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequirePage 是 HTML 页面的认证中间件：失败 302 跳登录。
func (a *Auth) RequirePage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := a.userFromCookie(r)
		if u == nil || u.IsDisabled {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusFound)
			return
		}
		ctx := store.SetUserInCtx(r.Context(), u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin 在 Middleware 之后使用：客服及以上可进后台 API。
func (a *Auth) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := store.UserFromCtx(r.Context())
		if u == nil || !u.IsStaffPlus() {
			writeJSON(w, http.StatusForbidden, errResp("forbidden"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequirePerm 要求具备指定后台权限（站长/管理员/客服分层）。
func (a *Auth) RequirePerm(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := store.UserFromCtx(r.Context())
			if u == nil || !u.Can(perm) {
				writeJSON(w, http.StatusForbidden, errResp("权限不足"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAdminPage 保护 admin HTML 页面（在 RequirePage 之后使用）。
func (a *Auth) RequireAdminPage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := store.UserFromCtx(r.Context())
		if u == nil || !u.IsStaffPlus() {
			http.Error(w, "403 forbidden — 需要管理员权限", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Auth) userFromCookie(r *http.Request) *store.User {
	if u := a.userFromBearer(r); u != nil {
		return u
	}
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := a.Store.GetUser(c.Value)
	if err != nil {
		return nil
	}
	return u
}

// userFromBearer 讀 Authorization: Bearer。微信內建瀏覽器不保存跨 Worker cookie。
func (a *Auth) userFromBearer(r *http.Request) *store.User {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(h) < 8 || !strings.EqualFold(h[:7], "Bearer ") {
		return nil
	}
	id := strings.TrimSpace(h[7:])
	if id == "" || strings.ContainsAny(id, " \r\n") {
		return nil
	}
	u, err := a.Store.GetUser(id)
	if err != nil {
		return nil
	}
	return u
}

// ── handlers ───────────────────────────────────────────────────────────────

func (a *Auth) userInfo(u *store.User) map[string]any {
	spent, _ := a.Store.GetUserSpent(u.ID)
	remaining := u.QuotaUSD - spent
	if u.Unlimited {
		remaining = -1 // 前端据此显示“无限”
	}
	role := u.RoleName()
	return map[string]any{
		"user_id":     u.ID,
		"username":    u.Username,
		"token":       u.ID,
		"is_admin":    u.IsStaffPlus(),
		"role":        role,
		"permissions": u.Permissions(),
		"quota_usd":   u.QuotaUSD,
		"unlimited":   u.Unlimited,
		"spent_usd":   spent,
		"remaining":   remaining,
	}
}

// LoginHandler 处理 POST /api/login。邀请制：无注册接口。
func (a *Auth) LoginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		username := strings.TrimSpace(req.Username)
		if username == "" || req.Password == "" {
			writeJSON(w, http.StatusUnauthorized, errResp("用户名或密码错误"))
			return
		}
		u, err := a.Store.GetUserByUsername(username)
		if err != nil || u == nil {
			writeJSON(w, http.StatusUnauthorized, errResp("用户名或密码错误"))
			return
		}
		if u.IsDisabled {
			writeJSON(w, http.StatusForbidden, errResp("该账号已被禁用"))
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
			writeJSON(w, http.StatusUnauthorized, errResp("用户名或密码错误"))
			return
		}
		setAuthCookie(w, r, u.ID)
		writeJSON(w, http.StatusOK, a.userInfo(u))
	}
}

func (a *Auth) LogoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clearAuthCookie(w, r)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (a *Auth) WhoamiHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := store.UserFromCtx(r.Context())
		writeJSON(w, http.StatusOK, a.userInfo(u))
	}
}

// RegisterHandler POST /api/register {username, password} — 开放注册。
// 新用户默认免费档：每日按模型档位分配次数（见 internal/billing）。
func (a *Auth) RegisterHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		username := strings.TrimSpace(req.Username)
		if username == "" || len(username) > 32 {
			writeJSON(w, http.StatusBadRequest, errResp("用户名不能为空且不超过 32 字符"))
			return
		}
		if len(req.Password) < MinPasswordLen {
			writeJSON(w, http.StatusBadRequest, errResp("密码至少 6 位"))
			return
		}
		if existing, _ := a.Store.GetUserByUsername(username); existing != nil {
			writeJSON(w, http.StatusConflict, errResp("该用户名已被注册"))
			return
		}
		u, err := a.CreateUser(username, req.Password, 0)
		if err != nil {
			slog.Error("register failed", "err", err)
			writeJSON(w, http.StatusInternalServerError, errResp("注册失败，请稍后再试"))
			return
		}
		setAuthCookie(w, r, u.ID)
		writeJSON(w, http.StatusOK, a.userInfo(u))
	}
}

// ChangePasswordHandler POST /api/account/password {old_password, new_password}
func (a *Auth) ChangePasswordHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := store.UserFromCtx(r.Context())
		var req struct {
			OldPassword string `json:"old_password"`
			NewPassword string `json:"new_password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("invalid request"))
			return
		}
		if len(req.NewPassword) < MinPasswordLen {
			writeJSON(w, http.StatusBadRequest, errResp("新密码至少 6 位"))
			return
		}
		if err := a.ChangePassword(u.ID, req.OldPassword, req.NewPassword); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp("旧密码错误"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// EnsureAdmin 在启动时幂等创建/校准管理员账号。
// 用户不存在则创建并置 is_admin；已存在则只强制 is_admin=1，不覆盖已有密码。
func (a *Auth) EnsureAdmin(username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil
	}
	existing, err := a.Store.GetUserByUsername(username)
	if err != nil {
		return err
	}
	if existing != nil {
		if password != "" {
			hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return err
			}
			if err := a.Store.UpdatePassword(existing.ID, string(hash)); err != nil {
				return err
			}
		}
		if existing.RoleName() != store.RoleOwner {
			return a.Store.SetAdmin(username, true)
		}
		return nil
	}
	if password == "" {
		slog.Warn("ADMIN_USERNAME set but user missing and no ADMIN_PASSWORD provided; skipping admin creation", "username", username)
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	id := GenerateUserID(username)
	// 管理员：无限配额。
	if err := a.Store.CreateUserWithPassword(id, username, string(hash), store.RoleOwner, 0, true); err != nil {
		return err
	}
	slog.Info("admin account created", "username", username, "id", id)
	return nil
}

// CreateUser 供注册与 admin 面板创建新用户，quotaUSD 为初始配额（默认 100.0 USD）。
func (a *Auth) CreateUser(username, password string, quotaUSD float64) (*store.User, error) {
	username = strings.TrimSpace(username)
	if quotaUSD <= 0 {
		quotaUSD = 100.0
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	id := GenerateUserID(username)
	if err := a.Store.CreateUserWithPassword(id, username, string(hash), store.RoleUser, quotaUSD, false); err != nil {
		return nil, err
	}
	return a.Store.GetUser(id)
}

// ChangePassword 校验旧密码后更新为新密码。
func (a *Auth) ChangePassword(userID, oldPassword, newPassword string) error {
	u, err := a.Store.GetUser(userID)
	if err != nil || u == nil {
		return errUserNotFound
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(oldPassword)) != nil {
		return errBadOldPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return a.Store.UpdatePassword(userID, string(hash))
}

// SetPassword 管理员重置密码，不校验旧密码。
func (a *Auth) SetPassword(userID, newPassword string) error {
	if len(newPassword) < MinPasswordLen {
		return errBadOldPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return a.Store.UpdatePassword(userID, string(hash))
}
