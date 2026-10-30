package chat

import (
	"testing"

	"deepweay-chat/internal/store"
)

func TestFallbackCandidates(t *testing.T) {
	usable := []store.ModelItem{
		{ID: "grok-4.3", Vendor: "grok", IsEnabled: true},
		{ID: "supergrok", Vendor: "grok", IsEnabled: true},
		{ID: "deepseek-v4-flash-0731", Vendor: "deepseek", IsEnabled: true},
		{ID: "qwen3.8-27b", Vendor: "qwen", IsEnabled: true},
	}

	t.Run("supergrok fallback", func(t *testing.T) {
		candidates := FallbackCandidates("supergrok", usable)
		if len(candidates) == 0 {
			t.Fatalf("expected candidates for supergrok, got 0")
		}
		// First candidate should be grok-4.6 or grok-4.3
		if candidates[0] != "grok-4.6" && candidates[0] != "grok-4.3" {
			t.Errorf("expected first candidate grok-4.6 or grok-4.3, got %s", candidates[0])
		}
		// supergrok itself should not be present
		for _, c := range candidates {
			if c == "supergrok" {
				t.Errorf("supergrok should not be in candidates list")
			}
		}
	})

	t.Run("kimi fallback", func(t *testing.T) {
		candidates := FallbackCandidates("moonshotai/Kimi-K2.6", usable)
		if len(candidates) == 0 {
			t.Fatalf("expected candidates for kimi, got 0")
		}
		if candidates[0] != "qwen3.8-27b" {
			t.Errorf("expected first candidate qwen3.8-27b, got %s", candidates[0])
		}
	})

	t.Run("gemini unknown fallback", func(t *testing.T) {
		candidates := FallbackCandidates("gemini-3.7-flash", usable)
		if len(candidates) == 0 {
			t.Fatalf("expected candidates for gemini-3.7-flash, got 0")
		}
	})
}
