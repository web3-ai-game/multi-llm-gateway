# multi-llm-gateway

一個帳號、七家廠商、三十二個模型。單一 Go 二進位，配額在串流寫出之前檢查。

One account, seven vendors, thirty-two models. One Go binary. Quota is checked before the stream is written.

這是脫敏後的公開匯出。正式環境的主機、部署腳本和金鑰留在私有庫。本樹沒有原點 IP。

This is a sanitized public export. The production host, deploy scripts, and secrets stay in the private repo. This tree has no origin address.

## 這份程式在做什麼

訪客註冊後就能聊天，不需要信箱。模型來自 xAI、Anthropic、OpenAI、Google、DeepSeek、MiniMax、Moonshot。新增一家廠商是註冊表的一列，兩條上游共用同一套 SSE。

每日免費額度按檔位計算：`cheap`、`standard`、`premium`、`pro`。紐約時間午夜重置。Pro 檔較高。管理員也可以對單一使用者設定美元上限。

助手在建立當下把系統提示詞快照進會話。之後改助手，不會改寫舊紀錄。

管理後台涵蓋使用者、配額、供應商金鑰和用量。金鑰在執行時注入，不進 git。

## What it does

A visitor registers and chats. No email. Models come from xAI, Anthropic, OpenAI, Google, DeepSeek, MiniMax, and Moonshot. A new vendor is one registry row. Both upstreams share one SSE writer.

Daily free quota is bucketed: `cheap`, `standard`, `premium`, `pro`. It resets at midnight America/New_York. Pro is the higher tier. An admin can also set a USD cap on one user.

An assistant snapshots its system prompt onto the session at creation. Later edits do not rewrite history.

The admin panel covers users, quotas, provider keys, and usage. Keys are injected at runtime and are not in git.

## 技術棧

Go 1.23、`chi`、`modernc.org/sqlite`（純 Go，WAL）、bcrypt、httpOnly cookie。前端是原生 HTML、CSS、JS，用 `go:embed` 打進同一個二進位。沒有前端建置步驟。

預設上游是 OpenRouter。另一條是 OpenAI 相容協定，給自建閘道用。

Go 1.23, chi, `modernc.org/sqlite` (pure Go, WAL), bcrypt, httpOnly cookie. The UI is vanilla HTML, CSS, and JS, embedded with `go:embed`. There is no frontend build.

The default upstream is OpenRouter. The second channel speaks the OpenAI-compatible protocol, for a gateway you run yourself.

## 本地執行

需要 Go 1.23 以上。

Needs Go 1.23 or newer.

```bash
go run .
```

預設聽 `:8080`。用 `PORT` 改。`OPENROUTER_API_KEY` 可省略，只在第一次啟動時把供應商種進資料庫。之後在管理後台改。

Listens on `:8080`. Override with `PORT`. `OPENROUTER_API_KEY` is optional. It only seeds the database on first boot. After that, change providers in the admin panel.

| 變數 | 用途 |
|---|---|
| `OPENROUTER_API_KEY` | 可省略。第一次啟動時種入資料庫。Optional. Seeds the database on first boot. |
| `PORT` | 預設 `8080`。Default `8080`. |
| `DB_PATH` | SQLite 路徑。預設 `./data/deepweay.db`。SQLite path. |
| `TITLE_GEN_MODEL` | 用來產生會話標題的便宜模型。Cheap model used to title a session. |
| `ADMIN_USERNAME` | 第一次啟動建立的管理員帳號。Admin account created on first boot. |
| `ADMIN_PASSWORD` | 管理員密碼。第一次部署後就改。Admin password. Change it after the first deploy. |

真實值不要提交。Inject real values at runtime. Do not commit them.

## 目錄

| 路徑 | 職責 |
|---|---|
| `internal/models` | 廠商與模型註冊表。Vendor and model registry. |
| `internal/provider` | 上游介面。Upstream interface. |
| `internal/chat` | 配額檢查與 SSE。Quota check and SSE. |
| `internal/billing` | 檔位與 Pro。Tiers and Pro. |
| `internal/store` | SQLite。 |
| `internal/admin` | 管理後台。Admin panel. |
| `static/` | 嵌入的前端。Embedded UI. |

## 狀態

核心對話、配額、助手和管理後台已在正式環境跑過。這個公開樹不含部署腳本。

Core chat, quotas, assistants, and the admin panel have run in production. This public tree does not include deploy scripts.

## 授權

未另附授權檔。看原始碼可以，拿去商用前先來信。

No license file is attached. Reading the source is fine. Ask before commercial use.

聯絡：one@oece-x.com
