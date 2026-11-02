package store

import (
	"path/filepath"
	"strings"
	"testing"

	"deepweay-chat/internal/models"
)

func TestApplyFeaturedModels(t *testing.T) {
	st, err := InitDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	n, err := st.ApplyFeaturedModels()
	if err != nil {
		t.Fatal(err)
	}
	want := len(models.Featured)
	if n != want {
		t.Fatalf("enabled %d, want %d", n, want)
	}
	on, err := st.ListEnabledModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(on) != want {
		t.Fatalf("list enabled %d, want %d", len(on), want)
	}

	usable, err := st.ListUsableModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(usable) != 0 {
		t.Fatalf("usable %d, want 0 when providers have empty keys", len(usable))
	}

	providers, err := st.ListProviders()
	if err != nil || len(providers) == 0 {
		t.Fatal(err)
	}
	var grok *Provider
	for i := range providers {
		if strings.Contains(strings.ToLower(providers[i].Name+providers[i].BaseURL), "x.ai") ||
			strings.Contains(strings.ToLower(providers[i].Name), "grok") {
			grok = &providers[i]
			break
		}
	}
	if grok == nil {
		t.Fatal("missing grok provider")
	}
	grok.APIKey = "xai-test-key"
	if err := st.UpdateProvider(grok); err != nil {
		t.Fatal(err)
	}
	n2, err := st.EnableFeaturedForProvider(grok)
	if err != nil {
		t.Fatal(err)
	}
	wantGrok := 0
	for _, spec := range models.Featured {
		if spec.Vendor == "grok" {
			wantGrok++
		}
	}
	if n2 != wantGrok {
		t.Fatalf("activate grok featured %d, want %d", n2, wantGrok)
	}
	usable, err = st.ListUsableModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(usable) != wantGrok {
		t.Fatalf("usable after grok key %d, want %d", len(usable), wantGrok)
	}
	for _, m := range usable {
		if m.Vendor != "grok" {
			t.Fatalf("unexpected vendor %s, only grok should be usable", m.Vendor)
		}
	}
}
