package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestListProvidersNoDeadlock(t *testing.T) {
	st, err := InitDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if _, err := st.CreateProvider(&Provider{Name: "a", BaseURL: "http://a.example", IsEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateProvider(&Provider{Name: "b", BaseURL: "http://b.example", IsEnabled: true}); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := st.ListProviders()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ListProviders deadlocked under MaxOpenConns(1)")
	}
}
