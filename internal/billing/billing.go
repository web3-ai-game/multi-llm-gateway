// Package billing 负责免费额度、Pro 会员门禁与充值相关的配置。
//
// 计费模型（面向初学者尽量简单）：
//   - 免费用户：按模型档位分配「每天多少条」，纽约时间 0 点重置。
//   - Pro 会员：额度大幅放宽，并解锁 pro 档模型（Opus / GPT-5.6 等）。
//   - 充值：人工核验——用户付款后邮件联系，管理员在后台开通/加余额。
package billing

import (
	"encoding/json"
	"time"

	_ "time/tzdata" // 内嵌时区库：精简 Linux 镜像常缺 /usr/share/zoneinfo

	"deepweay-chat/internal/models"
)

// 配置键（存 app_config，管理员可在后台改，无需重新部署）
const (
	KeyLimits       = "billing_limits"       // JSON: Limits
	KeyAlipayQR     = "pay_alipay_qr"        // data URI 图片
	KeyWechatQR     = "pay_wechat_qr"        // data URI 图片
	KeyGooglePayOn  = "pay_googlepay_on"     // "1"/"0"，占位通道
	KeyContactEmail = "pay_contact_email"    // 付款后联系邮箱
	KeyProPrice     = "price_pro_usd"        // Pro 会员价
	KeyMinTopup     = "price_min_topup_usd"  // 最低充值额
	KeySoftNudgeAt  = "billing_soft_nudge_at" // 第几条消息弹一次升级提示
)

// 默认值
const (
	DefaultContactEmail = "ops@example.com"
	DefaultProPrice     = 10.0
	DefaultMinTopup     = 4.0
	DefaultSoftNudgeAt  = 15
)

// TierLimits 是各档位每日可发消息条数；0 表示不可用（需升级 Pro）。
type TierLimits struct {
	Cheap    int `json:"cheap"`
	Standard int `json:"standard"`
	Premium  int `json:"premium"`
	Pro      int `json:"pro"`
}

// Limits 同时描述免费用户与 Pro 会员的每日额度。
type Limits struct {
	Free TierLimits `json:"free"`
	Pro  TierLimits `json:"pro"`
}

// DefaultLimits 提供大幅放宽的每日额度配置：
func DefaultLimits() Limits {
	return Limits{
		Free: TierLimits{Cheap: 500, Standard: 300, Premium: 100, Pro: 30},
		Pro:  TierLimits{Cheap: 5000, Standard: 3000, Premium: 1500, Pro: 500},
	}
}

// ParseLimits 解析配置；为空或损坏时回退默认值。
func ParseLimits(raw string) Limits {
	if raw == "" {
		return DefaultLimits()
	}
	var l Limits
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		return DefaultLimits()
	}
	// 任一档位为负视为未配置，用默认补齐
	d := DefaultLimits()
	if l.Free.Cheap <= 0 && l.Free.Standard <= 0 && l.Free.Premium <= 0 {
		l.Free = d.Free
	}
	if l.Pro.Cheap <= 0 && l.Pro.Standard <= 0 && l.Pro.Premium <= 0 {
		l.Pro = d.Pro
	}
	return l
}

// LimitFor 返回某档位对该用户的每日上限。
func (l Limits) LimitFor(tier string, isPro bool) int {
	t := l.Free
	if isPro {
		t = l.Pro
	}
	switch tier {
	case models.TierCheap:
		return t.Cheap
	case models.TierStandard:
		return t.Standard
	case models.TierPremium:
		return t.Premium
	case models.TierPro:
		return t.Pro
	}
	return t.Standard
}

// nyLoc 是计费日所用时区（纽约）。加载失败时退回 UTC-5 固定偏移。
var nyLoc = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.FixedZone("EST", -5*3600)
	}
	return loc
}()

// DayStart 返回「当前计费日」的起点（纽约 0 点）对应的时刻。
func DayStart(now time.Time) time.Time {
	n := now.In(nyLoc)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, nyLoc)
}

// NextReset 返回下一次额度重置时刻（纽约次日 0 点）。
// 用日曆加一天，不用 Add(24h)。夏令時那天 24 小時會落在當天 23:00。
func NextReset(now time.Time) time.Time {
	n := now.In(nyLoc)
	return time.Date(n.Year(), n.Month(), n.Day()+1, 0, 0, 0, 0, nyLoc)
}

// TierUsage 汇总某档位的用量与上限。
type TierUsage struct {
	Tier      string `json:"tier"`
	Used      int    `json:"used"`
	Limit     int    `json:"limit"`
	Remaining int    `json:"remaining"`
	Locked    bool   `json:"locked"` // true = 该档需要 Pro 会员
}

// Summarize 把 model_id→条数 汇总成各档位用量。
func Summarize(byModel map[string]int, lim Limits, isPro bool) map[string]TierUsage {
	tiers := []string{models.TierCheap, models.TierStandard, models.TierPremium, models.TierPro}
	used := map[string]int{}
	for id, n := range byModel {
		used[models.TierOf(id)] += n
	}
	out := make(map[string]TierUsage, len(tiers))
	for _, t := range tiers {
		limit := lim.LimitFor(t, isPro)
		u := used[t]
		rem := limit - u
		if rem < 0 {
			rem = 0
		}
		out[t] = TierUsage{Tier: t, Used: u, Limit: limit, Remaining: rem, Locked: limit == 0}
	}
	return out
}

// Decision 是一次发送前的额度判定结果。
type Decision struct {
	Allowed   bool
	NeedsPro  bool   // 该模型属于 Pro 专享档
	Tier      string
	Used      int
	Limit     int
	Reason    string // 面向用户的中文提示
	NextReset time.Time
}

// Check 判断用户此刻能否用该模型发消息。
func Check(modelID string, isPro bool, byModel map[string]int, lim Limits, now time.Time) Decision {
	tier := models.TierOf(modelID)
	limit := lim.LimitFor(tier, isPro)
	used := 0
	for id, n := range byModel {
		if models.TierOf(id) == tier {
			used += n
		}
	}
	d := Decision{Tier: tier, Used: used, Limit: limit, NextReset: NextReset(now)}
	if limit <= 0 {
		d.NeedsPro = true
		d.Reason = "该模型为 Pro 会员专享，升级后即可使用"
		return d
	}
	if used >= limit {
		if !isPro {
			d.NeedsPro = true
		}
		d.Reason = "今日该档模型次数已用完，明天纽约 0 点重置；升级 Pro 可大幅提升额度"
		return d
	}
	d.Allowed = true
	return d
}
