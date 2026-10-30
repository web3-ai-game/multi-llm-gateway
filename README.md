[PUBLIC-READY]

# Deepweay Chat / Deepweay Chat

面向 AI 初学者的多模型聚合聊天平台——一个账号统一调用 7 大厂商 32 个主流模型，开放注册，按模型档位给每日免费额度，Pro 会员可解锁更高额度。

A multi-model aggregator chat platform for AI beginners — one account, unified access to 32 mainstream models across 7 vendors. Open registration, daily free quota by model tier, Pro membership unlocks higher limits.

Public export of the gateway. Production host, deploy scripts, and secrets stay in the private repo.

## 技术栈 / Stack

- **后端 / Backend**：Go 1.23，`net/http` + `go-chi/chi/v5`，编译为单一二进制 / compiles to a single binary
- **数据库 / Database**：SQLite（`modernc.org/sqlite` 纯 Go 驱动，WAL 模式 / pure-Go driver, WAL mode）
- **前端 / Frontend**：原生 HTML/CSS/JS，`go:embed` 直接打进二进制，零构建步骤 / vanilla HTML/CSS/JS embedded via `go:embed`, zero build step
- **认证 / Auth**：bcrypt + httpOnly Cookie，开放注册（无需邮箱）/ open registration (no email required)
- **AI 接入 / AI backend**：OpenRouter（默认）+ sub2api（可选，OpenAI 兼容协议）/ OpenRouter (default) + sub2api (optional, OpenAI-compatible)
- **部署 / Deploy**：systemd + Caddy 反向代理 / reverse proxy

## 功能 / Features

- **7 厂商 32 个模型统一接入 / 7 vendors, 32 models, unified access**：Grok(xAI)、Claude(Anthropic)、GPT(OpenAI)、Gemini(Google)、DeepSeek、MiniMax、Kimi(Moonshot)。可插拔 Provider 架构，一套 SSE 流式代码复用两条通道，新增供应商只需在注册表加一行 / pluggable provider architecture, one SSE streaming implementation shared across both channels, adding a vendor is a one-line registry entry (`internal/models/models.go`)
- **免费 + Pro 会员配额 / Free + Pro membership quota**（`internal/chat`, `internal/billing`）：按模型档位（cheap/standard/premium/pro）给每日免费次数，纽约时区每日 0 点重置；Pro 会员额度更高，admin 也可对单用户设 USD 总额度覆盖 / daily free message counts by model tier, resets at midnight NY time; Pro members get higher limits, admin can also override per-user with a USD quota
- **助手系统 / Assistants**（Cherry 风格 / Cherry-style）：每用户可建多个助手（名称/emoji/系统提示词/模型/温度），会话创建时快照系统提示词 / each user can create multiple assistants (name/emoji/system prompt/model/temperature), sessions snapshot the prompt at creation
- **Admin 可视化后台 / Admin panel**（`internal/admin`, `static/admin.html`）：用户列表与配额、Provider key 配置、Pro 开通、站点用量统计，全程无需碰服务器命令行 / user/quota management, provider key config, Pro activation, usage stats — no server shell needed
- **go:embed 零构建部署 / Zero-build deploy**：前端资源与后端一起编译进单个二进制，按职责拆分 9 个 internal 模块（store / provider / oai / models / pricing / chat / auth / admin / usage）/ frontend assets compiled into the binary alongside the backend; 9 internal modules split by responsibility

## 本地运行 / Getting started

```bash
go run .
```

默认监听 `:8080`（可通过 `PORT` 覆盖）。`.env` 中 `OPENROUTER_API_KEY` 为可选项，仅用于首次启动播种到数据库，后续可在 admin 后台可视化配置。

Listens on `:8080` by default (override with `PORT`). `OPENROUTER_API_KEY` in `.env` is optional — used only to seed the database on first run; afterwards it's configurable from the admin panel.

## 环境变量 / Env

| 键 / Key | 说明 / Description |
|---|---|
| `OPENROUTER_API_KEY` | OpenRouter API key（可选，首次播种用）/ optional, seeds first run — injected at runtime, never committed |
| `PORT` | 监听端口，默认 8080 / listen port, default 8080 |
| `DB_PATH` | SQLite 路径，默认 `./data/deepweay.db` / SQLite path |
| `TITLE_GEN_MODEL` | 生成会话标题用的便宜模型 / cheap model used to generate chat titles |
| `ADMIN_USERNAME` | 首次启动创建的管理员账号 / admin account created on first run |
| `ADMIN_PASSWORD` | 管理员密码，首次部署后需修改 / admin password, change after first deploy — injected at runtime, never committed |

真实值均不进仓库，No real values are committed. Inject them at runtime.

## 完成度 / Status

能跑，核心功能（多模型对话、免费/Pro 配额、助手系统、admin 后台）已完整实现并在生产运行。/ Runnable — core features (multi-model chat, free/Pro quota, assistants, admin panel) are fully implemented and running in production.
