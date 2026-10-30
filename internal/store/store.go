package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"deepweay-chat/internal/models"

	_ "modernc.org/sqlite"
)

// UserCtxKey is the shared context key for injecting the authenticated user.
type UserCtxKey struct{}

func SetUserInCtx(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, UserCtxKey{}, u)
}

func UserFromCtx(ctx context.Context) *User {
	u, _ := ctx.Value(UserCtxKey{}).(*User)
	return u
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id            TEXT PRIMARY KEY,
	username      TEXT UNIQUE NOT NULL,
	password_hash TEXT NOT NULL,
	is_admin      INTEGER NOT NULL DEFAULT 0,
	role          TEXT NOT NULL DEFAULT 'user',
	is_disabled   INTEGER NOT NULL DEFAULT 0,
	quota_usd     REAL NOT NULL DEFAULT 1.0,
	unlimited     INTEGER NOT NULL DEFAULT 0,
	is_pro        INTEGER NOT NULL DEFAULT 0,
	pro_until     DATETIME,
	balance_usd   REAL NOT NULL DEFAULT 0,
	created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS app_config (
	key        TEXT PRIMARY KEY,
	value      TEXT NOT NULL DEFAULT '',
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS assistants (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id       TEXT NOT NULL,
	name          TEXT NOT NULL DEFAULT '默认助手',
	emoji         TEXT NOT NULL DEFAULT 'AI',
	system_prompt TEXT NOT NULL DEFAULT '',
	model_id      TEXT NOT NULL,
	temperature   REAL NOT NULL DEFAULT 0.7,
	is_default    INTEGER NOT NULL DEFAULT 0,
	created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS chat_sessions (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id       TEXT NOT NULL,
	vendor        TEXT NOT NULL,
	model_id      TEXT NOT NULL,
	provider      TEXT NOT NULL DEFAULT 'openrouter',
	assistant_id  INTEGER NOT NULL DEFAULT 0,
	system_prompt TEXT NOT NULL DEFAULT '',
	title         TEXT NOT NULL DEFAULT '新对话',
	msg_count     INTEGER NOT NULL DEFAULT 0,
	created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS chat_messages (
	id                 INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id         INTEGER NOT NULL,
	user_id            TEXT NOT NULL,
	role               TEXT NOT NULL,
	content            TEXT NOT NULL,
	model_id           TEXT,
	prompt_tokens      INTEGER NOT NULL DEFAULT 0,
	completion_tokens  INTEGER NOT NULL DEFAULT 0,
	cost_usd           REAL NOT NULL DEFAULT 0,
	is_estimated_cost  INTEGER NOT NULL DEFAULT 0,
	created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (session_id) REFERENCES chat_sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS model_pricing (
	model_id                  TEXT PRIMARY KEY,
	prompt_usd_per_token      REAL NOT NULL,
	completion_usd_per_token  REAL NOT NULL,
	updated_at                DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS providers (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	name        TEXT NOT NULL,
	type        TEXT NOT NULL DEFAULT 'openai_compatible',
	base_url    TEXT NOT NULL,
	api_key     TEXT NOT NULL DEFAULT '',
	is_enabled  INTEGER NOT NULL DEFAULT 1,
	created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS models (
	id          TEXT PRIMARY KEY,
	provider_id INTEGER NOT NULL DEFAULT 0,
	raw_model_id TEXT NOT NULL DEFAULT '',
	name        TEXT NOT NULL,
	vendor      TEXT NOT NULL DEFAULT 'other',
	tier        TEXT NOT NULL DEFAULT 'standard',
	description TEXT NOT NULL DEFAULT '',
	is_enabled  INTEGER NOT NULL DEFAULT 1,
	sort_order  INTEGER NOT NULL DEFAULT 0,
	created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_sessions_user        ON chat_sessions(user_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_messages_session      ON chat_messages(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_messages_user_created ON chat_messages(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_assistants_user       ON assistants(user_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_models_provider       ON models(provider_id);
CREATE INDEX IF NOT EXISTS idx_models_vendor         ON models(vendor, sort_order);
`

type Store struct {
	db     *sql.DB
	dbPath string
}

// DBPath 返回 SQLite 文件路径（供 admin 备份/导出等需要原始文件的场景使用）。
func (s *Store) DBPath() string { return s.dbPath }

type User struct {
	ID           string
	Username     string
	PasswordHash string
	IsAdmin      bool
	Role         string
	IsDisabled   bool
	QuotaUSD     float64
	Unlimited    bool
	IsPro        bool
	ProUntil     *time.Time
	BalanceUSD   float64
	CreatedAt    time.Time
}

// ProActive 判断 Pro 是否仍在有效期内（pro_until 为 NULL 表示永久）。
func (u *User) ProActive() bool {
	if !u.IsPro {
		return false
	}
	if u.ProUntil == nil {
		return true
	}
	return u.ProUntil.After(time.Now())
}

type ChatSession struct {
	ID           int64
	UserID       string
	Vendor       string
	ModelID      string
	Provider     string
	AssistantID  int64
	SystemPrompt string
	Title        string
	MsgCount     int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type ChatMessage struct {
	ID               int64
	SessionID        int64
	UserID           string
	Role             string
	Content          string
	ModelID          string
	PromptTokens     int
	CompletionTokens int
	CostUSD          float64
	IsEstimatedCost  bool
	CreatedAt        time.Time
}

type Provider struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"` // "openai_compatible", "gemini", "xai", "openrouter"
	BaseURL    string    `json:"base_url"`
	APIKey     string    `json:"api_key"`
	IsEnabled  bool      `json:"is_enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	ModelCount int       `json:"model_count"`
}

type ModelItem struct {
	ID           string    `json:"id"`
	ProviderID   int64     `json:"provider_id"`
	ProviderName string    `json:"provider_name,omitempty"`
	RawModelID   string    `json:"raw_model_id"`
	Name         string    `json:"name"`
	Vendor       string    `json:"vendor"`
	Tier         string    `json:"tier"`
	Description  string    `json:"description"`
	IsEnabled    bool      `json:"is_enabled"`
	SortOrder    int       `json:"sort_order"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func InitDB(dbPath string) (*Store, error) {
	// modernc.org/sqlite 使用 "sqlite" 驱动名。pragma 通过 DSN 传入。
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// SQLite 单写模式：1 个连接避免写冲突；busy_timeout 保证排队等待而非报错。
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err = db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	if err = migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db, dbPath: dbPath}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// migrate 幂等地补齐旧库缺失的列/表，可重复执行。
func migrate(db *sql.DB) error {
	// 旧库（V1）升级到 V2：补齐 users 配额列、chat_sessions 助手列。
	adds := []struct{ table, col, ddl string }{
		{"users", "quota_usd", "ALTER TABLE users ADD COLUMN quota_usd REAL NOT NULL DEFAULT 1.0"},
		{"users", "unlimited", "ALTER TABLE users ADD COLUMN unlimited INTEGER NOT NULL DEFAULT 0"},
		{"chat_sessions", "assistant_id", "ALTER TABLE chat_sessions ADD COLUMN assistant_id INTEGER NOT NULL DEFAULT 0"},
		{"chat_sessions", "system_prompt", "ALTER TABLE chat_sessions ADD COLUMN system_prompt TEXT NOT NULL DEFAULT ''"},
		// V3：会员与充值
		{"users", "is_pro", "ALTER TABLE users ADD COLUMN is_pro INTEGER NOT NULL DEFAULT 0"},
		{"users", "pro_until", "ALTER TABLE users ADD COLUMN pro_until DATETIME"},
		{"users", "balance_usd", "ALTER TABLE users ADD COLUMN balance_usd REAL NOT NULL DEFAULT 0"},
		{"users", "role", "ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user'"},
	}
	for _, a := range adds {
		ok, err := columnExists(db, a.table, a.col)
		if err != nil {
			return err
		}
		if !ok {
			if _, err := db.Exec(a.ddl); err != nil {
				return fmt.Errorf("migrate %s.%s: %w", a.table, a.col, err)
			}
		}
	}

	// 确保 providers 和 models 表存在
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS providers (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		name        TEXT NOT NULL,
		type        TEXT NOT NULL DEFAULT 'openai_compatible',
		base_url    TEXT NOT NULL,
		api_key     TEXT NOT NULL DEFAULT '',
		is_enabled  INTEGER NOT NULL DEFAULT 1,
		created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS models (
		id          TEXT PRIMARY KEY,
		provider_id INTEGER NOT NULL DEFAULT 0,
		raw_model_id TEXT NOT NULL DEFAULT '',
		name        TEXT NOT NULL,
		vendor      TEXT NOT NULL DEFAULT 'other',
		tier        TEXT NOT NULL DEFAULT 'standard',
		description TEXT NOT NULL DEFAULT '',
		is_enabled  INTEGER NOT NULL DEFAULT 1,
		sort_order  INTEGER NOT NULL DEFAULT 0,
		created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_models_provider ON models(provider_id);
	CREATE INDEX IF NOT EXISTS idx_models_vendor   ON models(vendor, sort_order);`); err != nil {
		return fmt.Errorf("create providers/models table: %w", err)
	}

	seedInitialProvidersAndModels(db)

	// 旧库 is_admin=1 且尚未写入角色 → 先标成 admin，启动时 EnsureAdmin 再把环境变量账号升为 owner。
	if _, err := db.Exec(`UPDATE users SET role = 'admin' WHERE is_admin = 1 AND (role IS NULL OR role = '' OR role = 'user')`); err != nil {
		return fmt.Errorf("migrate admin roles: %w", err)
	}
	return nil
}

func seedInitialProvidersAndModels(db *sql.DB) {
	var pCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM providers`).Scan(&pCount)
	if pCount == 0 {
		var orKey string
		_ = db.QueryRow(`SELECT value FROM app_config WHERE key='openrouter_api_key'`).Scan(&orKey)

		// 1. xAI Grok 官方直连
		resGrok, _ := db.Exec(`INSERT INTO providers (name, type, base_url, api_key, is_enabled) VALUES (?, ?, ?, ?, 1)`,
			"xAI Grok 官方", "openai_compatible", "https://api.x.ai/v1", "")
		grokID, _ := resGrok.LastInsertId()

		// 2. Google Gemini 官方直连 (OpenAI 兼容端点)
		resGemini, _ := db.Exec(`INSERT INTO providers (name, type, base_url, api_key, is_enabled) VALUES (?, ?, ?, ?, 1)`,
			"Google Gemini 官方", "openai_compatible", "https://generativelanguage.googleapis.com/v1beta/openai", "")
		geminiID, _ := resGemini.LastInsertId()

		// 3. OpenRouter 聚合
		resOR, _ := db.Exec(`INSERT INTO providers (name, type, base_url, api_key, is_enabled) VALUES (?, ?, ?, ?, 1)`,
			"OpenRouter", "openrouter", "https://openrouter.ai/api/v1", strings.TrimSpace(orKey))
		orID, _ := resOR.LastInsertId()

		// 初始化模型表，分流到对应服务商
		idx := 0
		for _, v := range models.Vendors {
			if v.ID == "other" {
				continue
			}
			targetPID := orID
			if v.ID == "grok" && grokID > 0 {
				targetPID = grokID
			} else if v.ID == "gemini" && geminiID > 0 {
				targetPID = geminiID
			}
			for _, m := range models.Registry[v.ID] {
				_, _ = db.Exec(`INSERT OR IGNORE INTO models (id, provider_id, raw_model_id, name, vendor, tier, description, is_enabled, sort_order)
					VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?)`,
					m.ID, targetPID, m.ID, m.DisplayName, m.Vendor, m.Tier, m.ShortDesc, idx)
				idx++
			}
		}
	}
}

func columnExists(db *sql.DB, table, col string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == col {
			return true, nil
		}
	}
	return false, rows.Err()
}

// ── users ────────────────────────────────────────────────────────────────

func (s *Store) CreateUserWithPassword(id, username, passwordHash, role string, quotaUSD float64, unlimited bool) error {
	role = NormalizeRole(role)
	admin, unl := 0, 0
	if RoleRank(role) >= 1 {
		admin = 1
	}
	if unlimited {
		unl = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO users (id, username, password_hash, is_admin, role, quota_usd, unlimited) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, username, passwordHash, admin, role, quotaUSD, unl,
	)
	return err
}

func scanUser(row interface {
	Scan(dest ...any) error
}) (*User, error) {
	var (
		u         User
		isAdmin   int
		disabled  int
		unlimited int
		isPro     int
		proUntil  sql.NullString
	)
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &isAdmin, &u.Role, &disabled, &u.QuotaUSD, &unlimited,
		&isPro, &proUntil, &u.BalanceUSD, &u.CreatedAt); err != nil {
		return nil, err
	}
	u.Role = NormalizeRole(u.Role)
	u.IsAdmin = isAdmin != 0 || u.IsStaffPlus()
	u.IsDisabled = disabled != 0
	u.Unlimited = unlimited != 0
	u.IsPro = isPro != 0
	u.ProUntil = parseDBTime(proUntil)
	return &u, nil
}

const userCols = `id, username, password_hash, is_admin, role, is_disabled, quota_usd, unlimited, is_pro, pro_until, balance_usd, created_at`

func (s *Store) GetUser(id string) (*User, error) {
	row := s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func (s *Store) GetUserByUsername(username string) (*User, error) {
	row := s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE username = ?`, username)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

// SetUserQuota 设置用户配额（USD）与是否无限。
func (s *Store) SetUserQuota(userID string, quotaUSD float64, unlimited bool) error {
	u := 0
	if unlimited {
		u = 1
	}
	_, err := s.db.Exec(`UPDATE users SET quota_usd = ?, unlimited = ? WHERE id = ?`, quotaUSD, u, userID)
	return err
}

// UpdatePassword 更新用户密码哈希。
func (s *Store) UpdatePassword(userID, passwordHash string) error {
	_, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, userID)
	return err
}

// SetPro 开关 Pro 会员。until 为 nil 表示永久有效。
//
// 注意：pro_until 一律以 RFC3339(UTC) 字符串写入。若直接把 time.Time 交给驱动，
// modernc.org/sqlite 会存成 Go 默认布局（"2006-01-02 15:04:05.999999 -0700 MST"），
// 读回时 sql.NullTime 无法解析 → GetUser 报错 → 用户被整个挡在门外。
func (s *Store) SetPro(userID string, isPro bool, until *time.Time) error {
	p := 0
	if isPro {
		p = 1
	}
	if until == nil {
		_, err := s.db.Exec(`UPDATE users SET is_pro = ?, pro_until = NULL WHERE id = ?`, p, userID)
		return err
	}
	_, err := s.db.Exec(`UPDATE users SET is_pro = ?, pro_until = ? WHERE id = ?`,
		p, until.UTC().Format(time.RFC3339), userID)
	return err
}

// parseDBTime 宽容解析库里的时间文本（RFC3339 为主，兼容历史/其它写法）。
func parseDBTime(ns sql.NullString) *time.Time {
	if !ns.Valid || strings.TrimSpace(ns.String) == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05.999999999 -0700 MST", // Go 默认布局（历史坏数据）
		"2006-01-02 15:04:05.999999-07:00",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, ns.String); err == nil {
			return &t
		}
	}
	return nil
}

// AddBalance 给用户充值（amount 可为负数用于扣减/纠错），返回新余额。
func (s *Store) AddBalance(userID string, amount float64) (float64, error) {
	if _, err := s.db.Exec(`UPDATE users SET balance_usd = balance_usd + ? WHERE id = ?`, amount, userID); err != nil {
		return 0, err
	}
	var bal float64
	err := s.db.QueryRow(`SELECT balance_usd FROM users WHERE id = ?`, userID).Scan(&bal)
	return bal, err
}

// CountMessagesSince 统计用户自 since 起发出的消息数（按 role='user' 计次）。
func (s *Store) CountMessagesSince(userID string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM chat_messages WHERE user_id = ? AND role = 'user' AND created_at >= ?`,
		userID, since.UTC()).Scan(&n)
	return n, err
}

// CountMessagesByModelSince 返回 model_id → 条数（自 since 起，role='user'）。
// 上层据此按档位汇总，避免把档位规则写进 SQL。
func (s *Store) CountMessagesByModelSince(userID string, since time.Time) (map[string]int, error) {
	rows, err := s.db.Query(
		`SELECT COALESCE(model_id,''), COUNT(*) FROM chat_messages
		 WHERE user_id = ? AND role = 'user' AND created_at >= ? GROUP BY model_id`,
		userID, since.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// CountModelMessagesSince 统计用户指定模型在 since 以后的消息条数（用于 Grok 4.6 等单模型专属限频）。
func (s *Store) CountModelMessagesSince(userID string, modelPattern string, since time.Time) (int, error) {
	return s.CountModelsMessagesSince(userID, []string{modelPattern}, since)
}

func (s *Store) CountModelsMessagesSince(userID string, patterns []string, since time.Time) (int, error) {
	if len(patterns) == 0 {
		return 0, nil
	}
	var b strings.Builder
	args := []any{userID}
	b.WriteString(`SELECT COUNT(*) FROM chat_messages WHERE user_id = ? AND role = 'user' AND created_at >= ? AND (`)
	args = append(args, since.UTC())
	for i, p := range patterns {
		if i > 0 {
			b.WriteString(" OR ")
		}
		b.WriteString(`model_id LIKE ?`)
		args = append(args, "%"+p+"%")
	}
	b.WriteString(`)`)
	var count int
	err := s.db.QueryRow(b.String(), args...).Scan(&count)
	return count, err
}

// GetUserSpent 返回用户累计花费（assistant 消息 cost_usd 之和）。
func (s *Store) GetUserSpent(userID string) (float64, error) {
	var spent float64
	err := s.db.QueryRow(
		`SELECT COALESCE(SUM(cost_usd),0) FROM chat_messages WHERE user_id = ? AND role='assistant'`, userID).Scan(&spent)
	return spent, err
}

func (s *Store) SetAdmin(username string, isAdmin bool) error {
	if isAdmin {
		_, err := s.db.Exec(`UPDATE users SET is_admin = 1, role = ? WHERE username = ?`, RoleOwner, username)
		return err
	}
	_, err := s.db.Exec(`UPDATE users SET is_admin = 0, role = ? WHERE username = ?`, RoleUser, username)
	return err
}

func (s *Store) SetUserRole(userID, role string) error {
	role = NormalizeRole(role)
	admin := 0
	if RoleRank(role) >= 1 {
		admin = 1
	}
	_, err := s.db.Exec(`UPDATE users SET role = ?, is_admin = ? WHERE id = ?`, role, admin, userID)
	return err
}

func (s *Store) CountByRole(role string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = ?`, NormalizeRole(role)).Scan(&n)
	return n, err
}

func (s *Store) SetDisabled(userID string, disabled bool) error {
	d := 0
	if disabled {
		d = 1
	}
	_, err := s.db.Exec(`UPDATE users SET is_disabled = ? WHERE id = ?`, d, userID)
	return err
}

// UserStat 是 admin 面板用户列表行（含聚合用量 + 会员 + 今日次数）。
type UserStat struct {
	ID            string  `json:"id"`
	Username      string  `json:"username"`
	IsAdmin       bool    `json:"is_admin"`
	Role          string  `json:"role"`
	IsDisabled    bool    `json:"is_disabled"`
	QuotaUSD      float64 `json:"quota_usd"`
	Unlimited     bool    `json:"unlimited"`
	IsPro         bool    `json:"is_pro"`
	ProUntil      string  `json:"pro_until"`
	BalanceUSD    float64 `json:"balance_usd"`
	CreatedAt     string  `json:"created_at"`
	TotalMessages int     `json:"total_messages"`
	TotalSpend    float64 `json:"total_spend"`
	TodayMessages int     `json:"today_messages"`
	Remaining     float64 `json:"remaining"`
}

// ListUsersWithStats 列出全部用户；dayStart 用于统计“今日消息数”（由上层按纽约 0 点算好传入）。
func (s *Store) ListUsersWithStats(dayStart time.Time) ([]UserStat, error) {
	rows, err := s.db.Query(`
		SELECT u.id, u.username, u.is_admin, u.role, u.is_disabled, u.quota_usd, u.unlimited,
		       u.is_pro, u.pro_until, u.balance_usd, u.created_at,
		       COALESCE(m.cnt, 0), COALESCE(m.spend, 0), COALESCE(t.cnt, 0)
		FROM users u
		LEFT JOIN (
			SELECT user_id, COUNT(*) AS cnt, SUM(cost_usd) AS spend
			FROM chat_messages WHERE role='assistant' GROUP BY user_id
		) m ON m.user_id = u.id
		LEFT JOIN (
			SELECT user_id, COUNT(*) AS cnt FROM chat_messages
			WHERE role='user' AND created_at >= ? GROUP BY user_id
		) t ON t.user_id = u.id
		ORDER BY u.created_at DESC`, dayStart.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserStat
	for rows.Next() {
		var (
			us        UserStat
			isAdmin   int
			disabled  int
			unlimited int
			isPro     int
			proUntil  sql.NullString
			created   time.Time
		)
		if err := rows.Scan(&us.ID, &us.Username, &isAdmin, &us.Role, &disabled, &us.QuotaUSD, &unlimited,
			&isPro, &proUntil, &us.BalanceUSD, &created,
			&us.TotalMessages, &us.TotalSpend, &us.TodayMessages); err != nil {
			return nil, err
		}
		us.Role = NormalizeRole(us.Role)
		if us.Role == RoleUser && isAdmin != 0 {
			us.Role = RoleAdmin
		}
		us.IsAdmin = isAdmin != 0 || RoleRank(us.Role) >= 1
		us.IsDisabled = disabled != 0
		us.Unlimited = unlimited != 0
		us.IsPro = isPro != 0
		if t := parseDBTime(proUntil); t != nil {
			us.ProUntil = t.Format("2006-01-02")
		}
		us.CreatedAt = created.Format("2006-01-02 15:04")
		us.Remaining = us.QuotaUSD - us.TotalSpend
		out = append(out, us)
	}
	return out, rows.Err()
}

// SiteStats 是 admin 面板的站点总览。
type SiteStats struct {
	TotalUsers    int     `json:"total_users"`
	TotalMessages int     `json:"total_messages"`
	TotalSpend    float64 `json:"total_spend"`
	Active7d      int     `json:"active_7d"`
}

func (s *Store) GetSiteStats() (SiteStats, error) {
	var st SiteStats
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&st.TotalUsers)
	_ = s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(cost_usd),0) FROM chat_messages WHERE role='assistant'`).
		Scan(&st.TotalMessages, &st.TotalSpend)
	_ = s.db.QueryRow(`SELECT COUNT(DISTINCT user_id) FROM chat_messages WHERE created_at >= datetime('now','-7 days')`).
		Scan(&st.Active7d)
	return st, nil
}

// ── chat sessions ────────────────────────────────────────────────────────

func (s *Store) CreateChatSession(userID, vendor, modelID, provider string, assistantID int64, systemPrompt, title string) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO chat_sessions (user_id, vendor, model_id, provider, assistant_id, system_prompt, title)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, vendor, modelID, provider, assistantID, systemPrompt, title)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const sessionCols = `id, user_id, vendor, model_id, provider, assistant_id, system_prompt, title, msg_count, created_at, updated_at`

func scanSession(sc interface{ Scan(...any) error }) (*ChatSession, error) {
	var c ChatSession
	err := sc.Scan(&c.ID, &c.UserID, &c.Vendor, &c.ModelID, &c.Provider, &c.AssistantID, &c.SystemPrompt, &c.Title, &c.MsgCount, &c.CreatedAt, &c.UpdatedAt)
	return &c, err
}

func (s *Store) GetChatSession(id int64, userID string) (*ChatSession, error) {
	row := s.db.QueryRow(`SELECT `+sessionCols+` FROM chat_sessions WHERE id = ? AND user_id = ?`, id, userID)
	c, err := scanSession(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) GetChatSessions(userID string) ([]ChatSession, error) {
	rows, err := s.db.Query(`SELECT `+sessionCols+` FROM chat_sessions WHERE user_id = ? AND msg_count > 0 ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatSession
	for rows.Next() {
		c, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Store) DeleteChatSession(id int64, userID string) error {
	// ON DELETE CASCADE 负责删除关联消息。
	_, err := s.db.Exec(`DELETE FROM chat_sessions WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

func (s *Store) UpdateSessionTitle(id int64, title string) error {
	_, err := s.db.Exec(`UPDATE chat_sessions SET title = ? WHERE id = ?`, title, id)
	return err
}

func (s *Store) UpdateSessionModel(id int64, userID string, vendor string, modelID string, provider string) error {
	_, err := s.db.Exec(
		`UPDATE chat_sessions SET vendor = ?, model_id = ?, provider = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ?`,
		vendor, modelID, provider, id, userID)
	return err
}

func (s *Store) TouchSession(id int64) error {
	_, err := s.db.Exec(`UPDATE chat_sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	return err
}

func (s *Store) IncrSessionMsgCount(id int64) error {
	_, err := s.db.Exec(`UPDATE chat_sessions SET msg_count = msg_count + 1 WHERE id = ?`, id)
	return err
}

// ── chat messages ────────────────────────────────────────────────────────

func (s *Store) SaveMessage(m *ChatMessage) error {
	est := 0
	if m.IsEstimatedCost {
		est = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO chat_messages
		 (session_id, user_id, role, content, model_id, prompt_tokens, completion_tokens, cost_usd, is_estimated_cost)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.SessionID, m.UserID, m.Role, m.Content, m.ModelID,
		m.PromptTokens, m.CompletionTokens, m.CostUSD, est)
	return err
}

func (s *Store) GetMessages(sessionID int64, userID string, limit int) ([]ChatMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	// 取最近 limit 条后按时间正序返回。
	rows, err := s.db.Query(
		`SELECT id, session_id, user_id, role, content, COALESCE(model_id,''),
		        prompt_tokens, completion_tokens, cost_usd, is_estimated_cost, created_at
		 FROM (
			SELECT * FROM chat_messages WHERE session_id = ? AND user_id = ?
			ORDER BY id DESC LIMIT ?
		 ) ORDER BY id ASC`, sessionID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatMessage
	for rows.Next() {
		var (
			m   ChatMessage
			est int
		)
		if err := rows.Scan(&m.ID, &m.SessionID, &m.UserID, &m.Role, &m.Content, &m.ModelID,
			&m.PromptTokens, &m.CompletionTokens, &m.CostUSD, &est, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.IsEstimatedCost = est != 0
		out = append(out, m)
	}
	return out, rows.Err()
}

// ── usage aggregation ────────────────────────────────────────────────────

type UsageSummary struct {
	TotalMessages int     `json:"total_messages"`
	TotalSpend    float64 `json:"total_spend"`
}

func (s *Store) GetUsageSummary(userID string) (UsageSummary, error) {
	var u UsageSummary
	err := s.db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(cost_usd),0) FROM chat_messages WHERE user_id = ? AND role='assistant'`, userID).
		Scan(&u.TotalMessages, &u.TotalSpend)
	return u, err
}

type ModelUsage struct {
	ModelID  string  `json:"model_id"`
	Messages int     `json:"messages"`
	Spend    float64 `json:"spend"`
}

func (s *Store) GetUsageByModel(userID string) ([]ModelUsage, error) {
	rows, err := s.db.Query(
		`SELECT COALESCE(model_id,''), COUNT(*), COALESCE(SUM(cost_usd),0)
		 FROM chat_messages WHERE user_id = ? AND role='assistant'
		 GROUP BY model_id ORDER BY SUM(cost_usd) DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelUsage
	for rows.Next() {
		var m ModelUsage
		if err := rows.Scan(&m.ModelID, &m.Messages, &m.Spend); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type DayUsage struct {
	Day   string  `json:"day"`
	Spend float64 `json:"spend"`
}

func (s *Store) GetUsageByDay(userID string) ([]DayUsage, error) {
	rows, err := s.db.Query(
		`SELECT date(created_at) AS d, COALESCE(SUM(cost_usd),0)
		 FROM chat_messages WHERE user_id = ? AND role='assistant'
		   AND created_at >= date('now','-30 days')
		 GROUP BY d ORDER BY d ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DayUsage
	for rows.Next() {
		var d DayUsage
		if err := rows.Scan(&d.Day, &d.Spend); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ── pricing cache ────────────────────────────────────────────────────────

func (s *Store) UpsertPricing(modelID string, prompt, completion float64) error {
	_, err := s.db.Exec(
		`INSERT INTO model_pricing (model_id, prompt_usd_per_token, completion_usd_per_token, updated_at)
		 VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(model_id) DO UPDATE SET
		   prompt_usd_per_token=excluded.prompt_usd_per_token,
		   completion_usd_per_token=excluded.completion_usd_per_token,
		   updated_at=CURRENT_TIMESTAMP`,
		modelID, prompt, completion)
	return err
}

// ── app_config（provider key 等运行时配置）────────────────────────────────

// SetProviderKeyByBaseURL 把 key 写进 base_url 含 needle 的服务商。没有则新建 xAI 官方行。
func (s *Store) SetProviderKeyByBaseURL(needle, apiKey string) error {
	needle = strings.ToLower(strings.TrimSpace(needle))
	apiKey = strings.TrimSpace(apiKey)
	if needle == "" || apiKey == "" {
		return nil
	}
	providers, err := s.ListProviders()
	if err != nil {
		return err
	}
	for _, p := range providers {
		if strings.Contains(strings.ToLower(p.BaseURL), needle) {
			p.APIKey = apiKey
			p.IsEnabled = true
			return s.UpdateProvider(&p)
		}
	}
	_, err = s.CreateProvider(&Provider{
		Name:      "xAI Grok 官方",
		Type:      "openai_compatible",
		BaseURL:   "https://api.x.ai/v1",
		APIKey:    apiKey,
		IsEnabled: true,
	})
	return err
}

func (s *Store) GetConfig(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM app_config WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (s *Store) SetConfig(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO app_config (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`,
		key, value)
	return err
}

func (s *Store) GetAllConfig() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM app_config`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// ── assistants（Cherry 风格助手：提示词 + 模型 + 参数）────────────────────────

type Assistant struct {
	ID           int64   `json:"id"`
	UserID       string  `json:"-"`
	Name         string  `json:"name"`
	Emoji        string  `json:"emoji"`
	SystemPrompt string  `json:"system_prompt"`
	ModelID      string  `json:"model_id"`
	Temperature  float64 `json:"temperature"`
	IsDefault    bool    `json:"is_default"`
}

func (s *Store) CreateAssistant(a *Assistant) (int64, error) {
	def := 0
	if a.IsDefault {
		def = 1
	}
	res, err := s.db.Exec(
		`INSERT INTO assistants (user_id, name, emoji, system_prompt, model_id, temperature, is_default)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.UserID, a.Name, a.Emoji, a.SystemPrompt, a.ModelID, a.Temperature, def)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func scanAssistant(sc interface{ Scan(...any) error }) (*Assistant, error) {
	var (
		a   Assistant
		def int
	)
	if err := sc.Scan(&a.ID, &a.UserID, &a.Name, &a.Emoji, &a.SystemPrompt, &a.ModelID, &a.Temperature, &def); err != nil {
		return nil, err
	}
	a.IsDefault = def != 0
	return &a, nil
}

const assistantCols = `id, user_id, name, emoji, system_prompt, model_id, temperature, is_default`

func (s *Store) GetAssistants(userID string) ([]Assistant, error) {
	rows, err := s.db.Query(`SELECT `+assistantCols+` FROM assistants WHERE user_id = ? ORDER BY is_default DESC, id ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Assistant
	for rows.Next() {
		a, err := scanAssistant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (s *Store) GetAssistant(id int64, userID string) (*Assistant, error) {
	row := s.db.QueryRow(`SELECT `+assistantCols+` FROM assistants WHERE id = ? AND user_id = ?`, id, userID)
	a, err := scanAssistant(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (s *Store) UpdateAssistant(a *Assistant) error {
	_, err := s.db.Exec(
		`UPDATE assistants SET name=?, emoji=?, system_prompt=?, model_id=?, temperature=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=? AND user_id=?`,
		a.Name, a.Emoji, a.SystemPrompt, a.ModelID, a.Temperature, a.ID, a.UserID)
	return err
}

func (s *Store) DeleteAssistant(id int64, userID string) error {
	_, err := s.db.Exec(`DELETE FROM assistants WHERE id = ? AND user_id = ? AND is_default = 0`, id, userID)
	return err
}

func (s *Store) CountAssistants(userID string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM assistants WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// ── pricing cache ────────────────────────────────────────────────────────

func (s *Store) GetAllPricing() (map[string][2]float64, error) {
	rows, err := s.db.Query(`SELECT model_id, prompt_usd_per_token, completion_usd_per_token FROM model_pricing`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][2]float64)
	for rows.Next() {
		var (
			id string
			p  float64
			c  float64
		)
		if err := rows.Scan(&id, &p, &c); err != nil {
			return nil, err
		}
		out[id] = [2]float64{p, c}
	}
	return out, rows.Err()
}

// ── Provider CRUD ────────────────────────────────────────────────────────────

const providerCols = `id, name, type, base_url, api_key, is_enabled, created_at, updated_at`

func parseTimeString(s string) time.Time {
	if strings.TrimSpace(s) == "" {
		return time.Now()
	}
	t := parseDBTime(sql.NullString{String: s, Valid: true})
	if t != nil {
		return *t
	}
	return time.Now()
}

func scanProvider(scanner interface{ Scan(dest ...any) error }) (*Provider, error) {
	var (
		p         Provider
		createdAt string
		updatedAt string
		enabled   int
	)
	if err := scanner.Scan(&p.ID, &p.Name, &p.Type, &p.BaseURL, &p.APIKey, &enabled, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	p.IsEnabled = enabled == 1
	p.CreatedAt = parseTimeString(createdAt)
	p.UpdatedAt = parseTimeString(updatedAt)
	return &p, nil
}

func (s *Store) ListProviders() ([]Provider, error) {
	// 必须一条 SQL 拿齐：MaxOpenConns(1) 下在 rows 未关时再 QueryRow 会永久死锁，
	// 表现为登录/注册/模型列表全部卡住。
	rows, err := s.db.Query(`
		SELECT p.id, p.name, p.type, p.base_url, p.api_key, p.is_enabled, p.created_at, p.updated_at,
		       COALESCE(c.cnt, 0)
		FROM providers p
		LEFT JOIN (SELECT provider_id, COUNT(*) AS cnt FROM models GROUP BY provider_id) c
		  ON c.provider_id = p.id
		ORDER BY p.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Provider
	for rows.Next() {
		var (
			p         Provider
			createdAt string
			updatedAt string
			enabled   int
		)
		if err := rows.Scan(&p.ID, &p.Name, &p.Type, &p.BaseURL, &p.APIKey, &enabled, &createdAt, &updatedAt, &p.ModelCount); err != nil {
			return nil, err
		}
		p.IsEnabled = enabled == 1
		p.CreatedAt = parseTimeString(createdAt)
		p.UpdatedAt = parseTimeString(updatedAt)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProvider(id int64) (*Provider, error) {
	row := s.db.QueryRow(`SELECT `+providerCols+` FROM providers WHERE id = ?`, id)
	p, err := scanProvider(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err == nil && p != nil {
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM models WHERE provider_id = ?`, p.ID).Scan(&p.ModelCount)
	}
	return p, err
}

func (s *Store) CreateProvider(p *Provider) (int64, error) {
	en := 0
	if p.IsEnabled {
		en = 1
	}
	if p.Type == "" {
		p.Type = "openai_compatible"
	}
	res, err := s.db.Exec(
		`INSERT INTO providers (name, type, base_url, api_key, is_enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		p.Name, p.Type, p.BaseURL, p.APIKey, en)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateProvider(p *Provider) error {
	en := 0
	if p.IsEnabled {
		en = 1
	}
	if p.Type == "" {
		p.Type = "openai_compatible"
	}
	if p.APIKey != "" {
		_, err := s.db.Exec(
			`UPDATE providers SET name=?, type=?, base_url=?, api_key=?, is_enabled=?, updated_at=CURRENT_TIMESTAMP
			 WHERE id=?`,
			p.Name, p.Type, p.BaseURL, p.APIKey, en, p.ID)
		return err
	}
	_, err := s.db.Exec(
		`UPDATE providers SET name=?, type=?, base_url=?, is_enabled=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=?`,
		p.Name, p.Type, p.BaseURL, en, p.ID)
	return err
}

func (s *Store) SetProviderModelsEnabled(providerID int64, enabled bool) error {
	en := 0
	if enabled {
		en = 1
	}
	_, err := s.db.Exec(`UPDATE models SET is_enabled = ?, updated_at = CURRENT_TIMESTAMP WHERE provider_id = ?`, en, providerID)
	return err
}

func (s *Store) ToggleProvider(id int64, enabled bool) error {
	en := 0
	if enabled {
		en = 1
	}
	_, err := s.db.Exec(`UPDATE providers SET is_enabled=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, en, id)
	return err
}

func (s *Store) DeleteProvider(id int64) error {
	_, _ = s.db.Exec(`DELETE FROM models WHERE provider_id = ?`, id)
	_, err := s.db.Exec(`DELETE FROM providers WHERE id = ?`, id)
	return err
}

// ── Models CRUD ──────────────────────────────────────────────────────────────

func (s *Store) ListAllModels() ([]ModelItem, error) {
	rows, err := s.db.Query(`
		SELECT m.id, m.provider_id, m.raw_model_id, m.name, m.vendor, m.tier, m.description, m.is_enabled, m.sort_order, m.created_at, m.updated_at,
		       COALESCE(p.name, '') as provider_name
		FROM models m
		LEFT JOIN providers p ON m.provider_id = p.id
		ORDER BY m.vendor ASC, m.sort_order ASC, m.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelItem
	for rows.Next() {
		var (
			m         ModelItem
			createdAt string
			updatedAt string
			enabled   int
		)
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.RawModelID, &m.Name, &m.Vendor, &m.Tier, &m.Description, &enabled, &m.SortOrder, &createdAt, &updatedAt, &m.ProviderName); err != nil {
			return nil, err
		}
		m.IsEnabled = enabled == 1
		m.CreatedAt = parseTimeString(createdAt)
		m.UpdatedAt = parseTimeString(updatedAt)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) ListUsableModels() ([]ModelItem, error) {
	rows, err := s.db.Query(`
		SELECT m.id, m.provider_id, m.raw_model_id, m.name, m.vendor, m.tier, m.description, m.is_enabled, m.sort_order, m.created_at, m.updated_at,
		       COALESCE(p.name, '') as provider_name
		FROM models m
		INNER JOIN providers p ON m.provider_id = p.id
		WHERE m.is_enabled = 1 AND p.is_enabled = 1 AND TRIM(COALESCE(p.api_key, '')) != ''
		ORDER BY m.sort_order ASC, m.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelItem
	for rows.Next() {
		var (
			m         ModelItem
			createdAt string
			updatedAt string
			enabled   int
		)
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.RawModelID, &m.Name, &m.Vendor, &m.Tier, &m.Description, &enabled, &m.SortOrder, &createdAt, &updatedAt, &m.ProviderName); err != nil {
			return nil, err
		}
		m.IsEnabled = enabled == 1
		m.CreatedAt = parseTimeString(createdAt)
		m.UpdatedAt = parseTimeString(updatedAt)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) ListEnabledModels() ([]ModelItem, error) {
	rows, err := s.db.Query(`
		SELECT m.id, m.provider_id, m.raw_model_id, m.name, m.vendor, m.tier, m.description, m.is_enabled, m.sort_order, m.created_at, m.updated_at,
		       COALESCE(p.name, '') as provider_name
		FROM models m
		LEFT JOIN providers p ON m.provider_id = p.id
		WHERE m.is_enabled = 1 AND (p.is_enabled IS NULL OR p.is_enabled = 1)
		ORDER BY m.sort_order ASC, m.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelItem
	for rows.Next() {
		var (
			m         ModelItem
			createdAt string
			updatedAt string
			enabled   int
		)
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.RawModelID, &m.Name, &m.Vendor, &m.Tier, &m.Description, &enabled, &m.SortOrder, &createdAt, &updatedAt, &m.ProviderName); err != nil {
			return nil, err
		}
		m.IsEnabled = enabled == 1
		m.CreatedAt = parseTimeString(createdAt)
		m.UpdatedAt = parseTimeString(updatedAt)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) GetModel(id string) (*ModelItem, error) {
	row := s.db.QueryRow(`
		SELECT m.id, m.provider_id, m.raw_model_id, m.name, m.vendor, m.tier, m.description, m.is_enabled, m.sort_order, m.created_at, m.updated_at,
		       COALESCE(p.name, '') as provider_name
		FROM models m
		LEFT JOIN providers p ON m.provider_id = p.id
		WHERE m.id = ?`, id)
	var (
		m         ModelItem
		createdAt string
		updatedAt string
		enabled   int
	)
	if err := row.Scan(&m.ID, &m.ProviderID, &m.RawModelID, &m.Name, &m.Vendor, &m.Tier, &m.Description, &enabled, &m.SortOrder, &createdAt, &updatedAt, &m.ProviderName); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	m.IsEnabled = enabled == 1
	m.CreatedAt = parseTimeString(createdAt)
	m.UpdatedAt = parseTimeString(updatedAt)
	return &m, nil
}

func (s *Store) SaveModel(m *ModelItem) error {
	en := 0
	if m.IsEnabled {
		en = 1
	}
	if m.RawModelID == "" {
		m.RawModelID = m.ID
	}
	_, err := s.db.Exec(`
		INSERT INTO models (id, provider_id, raw_model_id, name, vendor, tier, description, is_enabled, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			provider_id=excluded.provider_id,
			raw_model_id=excluded.raw_model_id,
			name=excluded.name,
			vendor=excluded.vendor,
			tier=excluded.tier,
			description=excluded.description,
			is_enabled=excluded.is_enabled,
			sort_order=excluded.sort_order,
			updated_at=CURRENT_TIMESTAMP`,
		m.ID, m.ProviderID, m.RawModelID, m.Name, m.Vendor, m.Tier, m.Description, en, m.SortOrder)
	return err
}

func (s *Store) BatchSaveModels(items []ModelItem) error {
	for _, m := range items {
		if err := s.SaveModel(&m); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ToggleModel(id string, enabled bool) error {
	en := 0
	if enabled {
		en = 1
	}
	_, err := s.db.Exec(`UPDATE models SET is_enabled=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, en, id)
	return err
}

func (s *Store) DeleteModel(id string) error {
	_, err := s.db.Exec(`DELETE FROM models WHERE id=?`, id)
	return err
}

// ReorderModels 按 ids 数组下标重写 sort_order（1-based）。
// 单事务保证要么全成功要么全失败，避免半路崩溃导致顺序错乱。
func (s *Store) ReorderModels(ids []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`UPDATE models SET sort_order=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, id := range ids {
		if _, err := stmt.Exec(i+1, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
