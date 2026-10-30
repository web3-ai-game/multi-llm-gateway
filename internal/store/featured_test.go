package store

import (
	"path/filepath"
	"strings"
	"testing"
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
	if n != 8 {
		t.Fatalf("enabled %d, want 8", n)
	}
	on, err := st.ListEnabledModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(on) != 8 {
		t.Fatalf("list enabled %d, want 8", len(on))
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
	if n2 != 2 {
		t.Fatalf("activate grok featured %d, want 2 (grok-4.3 + supergrok)", n2)
	}
	usable, err = st.ListUsableModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(usable) != 2 {
		t.Fatalf("usable after grok key %d, want 2", len(usable))
	}
	for _, m := range usable {
		if m.Vendor != "grok" {
			t.Fatalf("unexpected vendor %s, only grok should be usable", m.Vendor)
		}
	}
}
