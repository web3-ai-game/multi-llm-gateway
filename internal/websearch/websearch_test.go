package websearch

import (
	"testing"
)

func TestCleanText(t *testing.T) {
	raw := "<b>Hello</b> &amp; &quot;World&quot;! &#39;test&#39;&nbsp;&nbsp;"
	clean := cleanText(raw)
	expected := "Hello & \"World\"! 'test'"
	if clean != expected {
		t.Errorf("cleanText(%q) = %q, expected %q", raw, clean, expected)
	}
}

func TestFormatPrompt(t *testing.T) {
	results := []SearchResult{
		{Title: "Golang Official", URL: "https://go.dev", Snippet: "Build simple, secure, scalable systems with Go."},
	}
	prompt := FormatPrompt(results)
	if prompt == "" {
		t.Errorf("expected non-empty prompt")
	}
}
