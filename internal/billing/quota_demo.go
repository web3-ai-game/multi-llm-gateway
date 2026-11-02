package billing

import "time"

// TierOrder is the public quota ladder.
var TierOrder = []string{"cheap", "standard", "premium", "pro"}

// DemoLimits is the small ladder used by the offline quota demo.
func DemoLimits() Limits {
	return Limits{
		Free: TierLimits{Cheap: 4, Standard: 3, Premium: 2, Pro: 1},
		Pro:  TierLimits{Cheap: 8, Standard: 6, Premium: 4, Pro: 2},
	}
}

// QuotaStep is one request against the New York daily bucket.
type QuotaStep struct {
	At        time.Time
	Tier      string
	Allowed   bool
	Used      int
	Limit     int
	Remaining int
	ResetAt   time.Time
}

// WalkQuota applies one request per step and resets at America/New_York midnight.
func WalkQuota(limits Limits, isPro bool, steps []time.Time, tier string) []QuotaStep {
	used := 0
	var day time.Time
	out := make([]QuotaStep, 0, len(steps))
	limit := limits.LimitFor(tier, isPro)
	for _, at := range steps {
		start := DayStart(at)
		if day.IsZero() || !start.Equal(day) {
			used = 0
			day = start
		}
		allowed := used < limit
		if allowed {
			used++
		}
		remaining := limit - used
		if remaining < 0 {
			remaining = 0
		}
		out = append(out, QuotaStep{
			At:        at,
			Tier:      tier,
			Allowed:   allowed,
			Used:      used,
			Limit:     limit,
			Remaining: remaining,
			ResetAt:   NextReset(at),
		})
	}
	return out
}
