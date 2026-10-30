package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"log"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"deepweay-chat/internal/admin"
	"deepweay-chat/internal/auth"
	"deepweay-chat/internal/chat"
	"deepweay-chat/internal/pricing"
	"deepweay-chat/internal/store"
	"deepweay-chat/internal/usage"
)

//go:embed all:static
var staticFiles embed.FS

func main() {
	loadEnv(".env")

	// 显式注册：不同平台的系统 mime 表未必含 webp，避免上线后图片被当二进制流下载。
	_ = mime.AddExtensionType(".webp", "image/webp")

	port := envOr("PORT", "8080")
	dbPath := envOr("DB_PATH", "./data/deepweay.db")
	// OpenRouter Key 现在可选：优先用 admin 在后台配置的值，env 仅作首次播种/兜底。
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	titleModel := envOr("TITLE_GEN_MODEL", "google/gemini-2.5-flash-lite")
	adminUser := os.Getenv("ADMIN_USERNAME")
	adminPass := os.Getenv("ADMIN_PASSWORD")

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatalf("create db dir: %v", err)
	}

	st, err := store.InitDB(dbPath)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer st.Close()

	// admin 启动引导（幂等）。
	authSvc := auth.New(st)
	if err := authSvc.EnsureAdmin(adminUser, adminPass); err != nil {
		slog.Error("ensure admin failed", "err", err)
	}

	// 首次启动：若 app_config 未设 OpenRouter Key 而 env 里有，则播种一份（之后以后台配置为准）。
	if existing, _ := st.GetConfig("openrouter_api_key"); existing == "" && apiKey != "" {
		_ = st.SetConfig("openrouter_api_key", apiKey)
	}
	// xAI 直连：env 有 key 时写入「xAI Grok 官方」，不写进前端、不进仓库。
	if xaiKey := strings.TrimSpace(os.Getenv("XAI_API_KEY")); xaiKey != "" {
		if err := st.SetProviderKeyByBaseURL("api.x.ai", xaiKey); err != nil {
			slog.Error("seed xai key failed", "err", err)
		}
	}

	// 用户端默认只开 4 款。可在后台再点一次「仅启用精选」。
	if flag, _ := st.GetConfig(store.FeaturedAppliedKey); flag != "1" {
		if n, err := st.ApplyFeaturedModels(); err != nil {
			slog.Warn("apply featured models failed", "err", err)
		} else {
			_ = st.SetConfig(store.FeaturedAppliedKey, "1")
			slog.Info("featured models enabled", "count", n)
		}
	}
	// 有 xAI key 的部署只上架 Grok 4.7，放在精选播种之后，避免被旧清单盖掉。
	if strings.TrimSpace(os.Getenv("XAI_API_KEY")) != "" {
		if err := st.EnableOnlyModel("grok-4.7", "grok-4.7", "Grok 4.7", "grok"); err != nil {
			slog.Error("enable grok-4.7 failed", "err", err)
		}
	}

	// pricing：用 DB 里上次落库的价格 seed，然后后台拉取 + 每小时刷新。
	seed, _ := st.GetAllPricing()
	priceCache := pricing.New(seed, st.UpsertPricing)
	priceCache.StartAutoRefresh(context.Background())

	chatDeps := &chat.Deps{Store: st, Pricing: priceCache, TitleModel: titleModel, EnvORKey: apiKey}
	adminSvc := admin.New(st, authSvc, priceCache)
	usageSvc := usage.New(st)
	go func() {
		adminSvc.RefreshUsableProviders()
	}()

	r := chi.NewRouter()
	r.Use(chimw.Recoverer)
	r.Use(chimw.Compress(5))
	r.Use(apiAccessLog)

	// ── 公开页面 ────────────────────────────────────────────────────────────
	r.Get("/", servePage("static/index.html"))
	r.Get("/login", servePage("static/login.html"))

	// ── 需登录页面 ──────────────────────────────────────────────────────────
	r.With(authSvc.RequirePage).Get("/chat", servePage("static/chat.html"))
	r.With(authSvc.RequirePage).Get("/settings", servePage("static/settings.html"))
	r.With(authSvc.RequirePage).Get("/billing", servePage("static/billing.html"))
	r.With(authSvc.RequirePage).Get("/dashboard", servePage("static/dashboard.html"))
	r.With(authSvc.RequirePage, authSvc.RequireAdminPage).Get("/admin", servePage("static/admin.html"))

	// ── 静态资源 ──────────────────────────────────────────────────────────────
	// JS/CSS/i18n 无构建、无指纹，用 ETag + 重新验证：重复访问返回 304（近零传输），
	// 部署新内容时 ETag 变化即刻生效（避免 immutable 缓存导致用户拿不到新代码）。
	r.Get("/assets/*", serveAssets)
	r.Get("/i18n/*", serveAssets)
	r.Get("/img/*", serveAssets)
	r.Get("/favicon.svg", serveAssets)
	r.Get("/favicon.png", serveAssets)
	r.Get("/favicon.ico", serveAssets)
	r.Get("/apple-touch-icon.png", serveAssets)

	// ── 公开 API ────────────────────────────────────────────────────────────
	r.Post("/api/login", authSvc.LoginHandler())
	r.Post("/api/register", authSvc.RegisterHandler()) // 开放注册
	r.Get("/api/models", chatDeps.ModelsHandler())

	// ── 需认证 API ──────────────────────────────────────────────────────────
	r.Group(func(r chi.Router) {
		r.Use(authSvc.Middleware)

		r.Get("/api/whoami", authSvc.WhoamiHandler())
		r.Post("/api/logout", authSvc.LogoutHandler())
		r.Post("/api/account/password", authSvc.ChangePasswordHandler())

		r.Get("/api/sessions", chatDeps.SessionsHandler())
		r.Post("/api/sessions", chatDeps.CreateSessionHandler())
		r.Delete("/api/sessions/{id}", chatDeps.DeleteSessionHandler())
		r.Post("/api/sessions/{id}/title", chatDeps.RenameSessionHandler())
		r.Post("/api/sessions/{id}/model", chatDeps.UpdateSessionModelHandler())
		r.Get("/api/sessions/{id}/messages", chatDeps.MessagesHandler())
		r.Post("/api/chat/stream", chatDeps.StreamChatHandler())

		// 助手（Cherry 风格）
		r.Get("/api/assistants", chatDeps.AssistantsHandler())
		r.Get("/api/assistant-templates", chatDeps.AssistantTemplatesHandler())
		r.Post("/api/assistants", chatDeps.CreateAssistantHandler())
		r.Put("/api/assistants/{id}", chatDeps.UpdateAssistantHandler())
		r.Delete("/api/assistants/{id}", chatDeps.DeleteAssistantHandler())

		r.Get("/api/usage", usageSvc.SummaryHandler())
		r.Get("/api/billing/info", chatDeps.BillingInfoHandler())

		// admin-only（客服及以上进后台；密钥/收款仅站长）
		r.Group(func(r chi.Router) {
			r.Use(authSvc.RequireAdmin)
			r.With(authSvc.RequirePerm(store.PermUsersRead)).Get("/api/admin/users", adminSvc.ListUsersHandler())
			r.With(authSvc.RequirePerm(store.PermUsersWrite)).Post("/api/admin/users", adminSvc.CreateUserHandler())
			r.With(authSvc.RequirePerm(store.PermUsersWrite)).Post("/api/admin/users/{id}/disable", adminSvc.ToggleDisableHandler())
			r.With(authSvc.RequirePerm(store.PermUsersWrite)).Post("/api/admin/users/{id}/quota", adminSvc.SetQuotaHandler())
			r.With(authSvc.RequirePerm(store.PermUsersWrite)).Post("/api/admin/users/{id}/balance", adminSvc.AddBalanceHandler())
			r.With(authSvc.RequirePerm(store.PermUsersWrite)).Post("/api/admin/users/{id}/pro", adminSvc.SetProHandler())
			r.With(authSvc.RequirePerm(store.PermUsersWrite)).Post("/api/admin/users/{id}/password", adminSvc.ResetPasswordHandler())
			r.With(authSvc.RequirePerm(store.PermUsersRole)).Post("/api/admin/users/{id}/role", adminSvc.SetRoleHandler())
			r.With(authSvc.RequirePerm(store.PermStatsRead)).Get("/api/admin/stats", adminSvc.StatsHandler())
			r.With(authSvc.RequirePerm(store.PermSystemRead)).Get("/api/admin/system", adminSvc.SystemStatusHandler())
			r.With(authSvc.RequirePerm(store.PermSystemRead)).Get("/api/admin/logs", adminSvc.LogsHandler())
			r.With(authSvc.RequirePerm(store.PermConfigManage)).Get("/api/admin/config", adminSvc.GetConfigHandler())
			r.With(authSvc.RequirePerm(store.PermConfigManage)).Post("/api/admin/config", adminSvc.SetConfigHandler())
			r.With(authSvc.RequirePerm(store.PermConfigManage)).Get("/api/admin/backup", adminSvc.BackupHandler())
			r.With(authSvc.RequirePerm(store.PermConfigManage)).Get("/api/admin/payconfig", adminSvc.GetPayConfigHandler())
			r.With(authSvc.RequirePerm(store.PermConfigManage)).Post("/api/admin/payconfig", adminSvc.SetPayConfigHandler())
			r.With(authSvc.RequirePerm(store.PermStatsRead)).Get("/api/admin/pricing", adminSvc.PricingHandler())

			r.With(authSvc.RequirePerm(store.PermProvidersManage)).Get("/api/admin/providers", adminSvc.ListProvidersHandler())
			r.With(authSvc.RequirePerm(store.PermProvidersManage)).Post("/api/admin/providers", adminSvc.CreateProviderHandler())
			r.With(authSvc.RequirePerm(store.PermProvidersManage)).Put("/api/admin/providers/{id}", adminSvc.UpdateProviderHandler())
			r.With(authSvc.RequirePerm(store.PermProvidersManage)).Post("/api/admin/providers/{id}/toggle", adminSvc.ToggleProviderHandler())
			r.With(authSvc.RequirePerm(store.PermProvidersManage)).Delete("/api/admin/providers/{id}", adminSvc.DeleteProviderHandler())
			r.With(authSvc.RequirePerm(store.PermProvidersManage)).Post("/api/admin/providers/detect", adminSvc.DetectModelsHandler())
			r.With(authSvc.RequirePerm(store.PermProvidersManage)).Post("/api/admin/providers/probe", adminSvc.ProbeProviderHandler())

			r.With(authSvc.RequirePerm(store.PermModelsManage)).Get("/api/admin/models", adminSvc.ListModelsHandler())
			r.With(authSvc.RequirePerm(store.PermModelsManage)).Get("/api/admin/models/diagnose", adminSvc.DiagnoseModelsHandler())
			r.With(authSvc.RequirePerm(store.PermModelsManage)).Post("/api/admin/models/batch-save", adminSvc.BatchSaveModelsHandler())
			r.With(authSvc.RequirePerm(store.PermModelsManage)).Post("/api/admin/models/featured", adminSvc.ApplyFeaturedModelsHandler())
			r.With(authSvc.RequirePerm(store.PermModelsManage)).Put("/api/admin/models/{id}", adminSvc.UpdateModelHandler())
			r.With(authSvc.RequirePerm(store.PermModelsManage)).Post("/api/admin/models/{id}/toggle", adminSvc.ToggleModelHandler())
			r.With(authSvc.RequirePerm(store.PermModelsManage)).Delete("/api/admin/models/{id}", adminSvc.DeleteModelHandler())
			r.With(authSvc.RequirePerm(store.PermModelsManage)).Post("/api/admin/models/reorder", adminSvc.ReorderModelsHandler())
		})
	})

	slog.Info("server starting", "port", port)
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		// WriteTimeout 刻意不设：SSE 流式 + 慢推理模型可能超过 60s。依赖 r.Context() 取消。
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 16,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

// ── 静态服务 ────────────────────────────────────────────────────────────────

type embeddedFile struct {
	data []byte
	etag string
}

var (
	embedOnce sync.Once
	embedded  = map[string]embeddedFile{}
)

func prewarm() {
	_ = fs.WalkDir(staticFiles, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, err := staticFiles.ReadFile(p)
		if err != nil {
			return nil
		}
		buf := make([]byte, len(data))
		copy(buf, data)
		sum := sha256.Sum256(buf)
		embedded[p] = embeddedFile{data: buf, etag: `"` + hex.EncodeToString(sum[:8]) + `"`}
		return nil
	})
}

func readEmbedded(name string) (embeddedFile, bool) {
	embedOnce.Do(prewarm)
	e, ok := embedded[name]
	return e, ok
}

func servePage(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		e, ok := readEmbedded(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		w.Header().Set("Content-Length", strconv.Itoa(len(e.data)))
		_, _ = w.Write(e.data)
	}
}

// serveAssets 服务 static/ 下任意文件（按 URL 路径），content-type 由扩展名推断。
// 用 ETag + no-cache 重新验证：内容不变返回 304，内容变化立即生效。
func serveAssets(w http.ResponseWriter, r *http.Request) {
	clean := path.Clean(r.URL.Path)
	name := "static" + clean
	e, ok := readEmbedded(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("ETag", e.etag)
	w.Header().Set("Cache-Control", "no-cache")
	if match := r.Header.Get("If-None-Match"); match == e.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	ct := mime.TypeByExtension(filepath.Ext(name))
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(e.data)))
	_, _ = w.Write(e.data)
}

// ── access log ──────────────────────────────────────────────────────────────

func apiAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			start := time.Now()
			next.ServeHTTP(w, r)
			slog.Info("api", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start).String())
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ── env ─────────────────────────────────────────────────────────────────────

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// loadEnv 读取 .env（KEY=VALUE 每行），已存在的环境变量不覆盖。
func loadEnv(pth string) {
	f, err := os.Open(pth)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
}
