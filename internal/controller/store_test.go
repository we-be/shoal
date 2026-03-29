package controller

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/we-be/shoal/internal/api"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-pool.json")

	// Create pool with state
	pool1 := NewPool()
	pool1.Register(api.RegisterRequest{Address: ":8181", Backend: api.BackendChrome, IP: "1.2.3.4"})
	pool1.Register(api.RegisterRequest{Address: ":8182", Backend: api.BackendTLSClient})

	// Build some domain state
	lease, _ := pool1.Acquire(api.LeaseRequest{Consumer: "test", Domain: "example.com"})
	pool1.RecordNavigation(lease.ID, "https://example.com/", []api.Cookie{
		{Name: "session", Value: "abc", Domain: "example.com"},
		{Name: "cf_clearance", Value: "xyz", Domain: "example.com", Expires: float64(time.Now().Add(time.Hour).Unix())},
	})
	pool1.Release(lease.ID)

	// Save
	store1 := NewStore(pool1, path, time.Minute)
	store1.save()

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("snapshot file not created: %v", err)
	}

	// Load into fresh pool
	pool2 := NewPool()
	store2 := NewStore(pool2, path, time.Minute)
	if err := store2.Load(); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	// Verify state restored
	agents := pool2.Agents()
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}

	// Find the chrome agent
	var chrome *api.BrowserIdentity
	for _, a := range agents {
		if a.Backend == api.BackendChrome {
			chrome = &a
			break
		}
	}
	if chrome == nil {
		t.Fatal("chrome agent not found after restore")
	}

	if chrome.IP != "1.2.3.4" {
		t.Fatalf("expected IP 1.2.3.4, got %s", chrome.IP)
	}

	state := chrome.Domains["example.com"]
	if state == nil {
		t.Fatal("expected domain state for example.com")
	}
	if !state.HasCFClearance {
		t.Fatal("expected CF clearance to be preserved")
	}
	if len(state.Cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(state.Cookies))
	}
}

func TestStoreLoadMissing(t *testing.T) {
	pool := NewPool()
	store := NewStore(pool, "/nonexistent/path/pool.json", time.Minute)

	// Should not error — just starts fresh
	if err := store.Load(); err != nil {
		t.Fatalf("load of missing file should not error: %v", err)
	}
}
