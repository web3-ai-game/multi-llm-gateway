package mock

import (
	"context"
	"strings"
	"testing"

	"deepweay-chat/internal/provider"
)

func TestStreamSevenVendors(t *testing.T) {
	if len(Vendors) != 7 {
		t.Fatalf("want 7 vendors, got %d", len(Vendors))
	}
	for _, vendor := range Vendors {
		var lines []string
		res, err := New(vendor).StreamChatCompletion(context.Background(), provider.ChatRequest{
			Model:    "demo",
			Messages: []provider.Message{{Role: "user", Content: "quota walk"}},
		}, func(raw string) {
			lines = append(lines, raw)
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Usage == nil || res.Usage.CompletionTokens == 0 {
			t.Fatalf("%s missing usage", vendor)
		}
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, vendor) || !strings.Contains(joined, "usage") {
			t.Fatalf("%s stream missing vendor or usage: %s", vendor, joined)
		}
	}
}
