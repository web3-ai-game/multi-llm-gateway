// Offline demo. No API key, no database, no network.
package main

import (
	"context"
	"fmt"
	"time"

	"deepweay-chat/internal/billing"
	"deepweay-chat/internal/provider"
	"deepweay-chat/internal/provider/mock"
)

func main() {
	fmt.Println("multi-llm-gateway demo")
	fmt.Println("mode: mock")
	fmt.Println("keys: none")
	fmt.Println("vendors: xAI Anthropic OpenAI Google DeepSeek MiniMax Moonshot")
	fmt.Println()
	ctx := context.Background()
	for _, vendor := range mock.Vendors {
		var chunks int
		res, err := mock.New(vendor).StreamChatCompletion(ctx, provider.ChatRequest{
			Model:    "demo-" + vendor,
			Messages: []provider.Message{{Role: "user", Content: "Show the tier walk."}},
		}, func(raw string) {
			chunks++
			fmt.Printf("  sse %s %s\n", vendor, raw)
		})
		if err != nil {
			fmt.Printf("%s error: %v\n", vendor, err)
			continue
		}
		fmt.Printf("%s done chunks=%d completion_tokens=%d\n\n", vendor, chunks, res.Usage.CompletionTokens)
	}

	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		loc = time.FixedZone("EST", -5*3600)
	}
	evening := time.Date(2026, 11, 1, 23, 50, 0, 0, loc)
	fmt.Println("quota: free user, New York day, cheap/standard/premium/pro")
	limits := billing.DemoLimits()
	for _, tier := range billing.TierOrder {
		steps := []time.Time{evening, evening.Add(time.Minute), evening.Add(20 * time.Minute)}
		fmt.Printf("\n[%s] limit=%d\n", tier, limits.LimitFor(tier, false))
		for _, step := range billing.WalkQuota(limits, false, steps, tier) {
			state := "allow"
			if !step.Allowed {
				state = "deny"
			}
			fmt.Printf("  %s %s used=%d/%d remaining=%d reset=%s\n",
				step.At.Format("2006-01-02 15:04 MST"), state, step.Used, step.Limit, step.Remaining, step.ResetAt.Format(time.RFC3339))
		}
	}
}
