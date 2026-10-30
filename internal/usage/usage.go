package usage

import (
	"encoding/json"
	"net/http"

	"deepweay-chat/internal/models"
	"deepweay-chat/internal/store"
)

type Usage struct {
	Store *store.Store
}

func New(st *store.Store) *Usage { return &Usage{Store: st} }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// SummaryHandler GET /api/usage — 用户用量聚合（总花费 + 按模型 + 按天）。
func (u *Usage) SummaryHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		usr := store.UserFromCtx(r.Context())

		summary, _ := u.Store.GetUsageSummary(usr.ID)
		byModel, _ := u.Store.GetUsageByModel(usr.ID)
		byDay, _ := u.Store.GetUsageByDay(usr.ID)

		// 给按模型加上展示名 + 厂商。
		type modelRow struct {
			ModelID   string  `json:"model_id"`
			ModelName string  `json:"model_name"`
			Vendor    string  `json:"vendor"`
			Messages  int     `json:"messages"`
			Spend     float64 `json:"spend"`
		}
		rows := make([]modelRow, 0, len(byModel))
		for _, m := range byModel {
			name := m.ModelID
			vendor := ""
			if mm, ok := models.Get(m.ModelID); ok {
				name = mm.DisplayName
				vendor = mm.Vendor
			}
			rows = append(rows, modelRow{
				ModelID: m.ModelID, ModelName: name, Vendor: vendor,
				Messages: m.Messages, Spend: m.Spend,
			})
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"total_messages": summary.TotalMessages,
			"total_spend":    summary.TotalSpend,
			"by_model":       rows,
			"by_day":         byDay,
		})
	}
}
