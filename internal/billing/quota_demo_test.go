package billing

import (
	"testing"
	"time"
)

func TestWalkQuotaResetsAtNewYorkMidnight(t *testing.T) {
	loc := nyLoc
	before := time.Date(2026, 11, 1, 23, 50, 0, 0, loc)
	after := time.Date(2026, 11, 2, 0, 10, 0, 0, loc)
	limits := DemoLimits()
	steps := WalkQuota(limits, false, []time.Time{before, before.Add(time.Minute), after}, "pro")
	if !steps[0].Allowed || steps[0].Remaining != 0 {
		t.Fatalf("first pro request should consume the only free slot: %+v", steps[0])
	}
	if steps[1].Allowed {
		t.Fatalf("second request before midnight should be denied: %+v", steps[1])
	}
	if !steps[2].Allowed || steps[2].Used != 1 {
		t.Fatalf("request after New York midnight should reset: %+v", steps[2])
	}
	if !steps[2].ResetAt.After(steps[2].At) {
		t.Fatalf("reset should be the next New York midnight: %+v", steps[2])
	}
	want := time.Date(2026, 11, 2, 0, 0, 0, 0, loc)
	if !steps[0].ResetAt.Equal(want) {
		t.Fatalf("reset before midnight = %s, want %s", steps[0].ResetAt, want)
	}
	wantAfter := time.Date(2026, 11, 3, 0, 0, 0, 0, loc)
	if !steps[2].ResetAt.Equal(wantAfter) {
		t.Fatalf("reset after midnight = %s, want %s", steps[2].ResetAt, wantAfter)
	}
}

func TestDemoLimitsCoverAllTiers(t *testing.T) {
	limits := DemoLimits()
	for _, tier := range TierOrder {
		if limits.LimitFor(tier, false) <= 0 {
			t.Fatalf("free tier %s has no demo limit", tier)
		}
		if limits.LimitFor(tier, true) <= limits.LimitFor(tier, false) {
			t.Fatalf("pro tier %s should be higher than free", tier)
		}
	}
}
