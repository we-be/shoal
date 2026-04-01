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
		{Name: api.CookieCFClearance, Value: "xyz", Domain: "example.com", Expires: float64(time.Now().Add(time.Hour).Unix())},
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

	// Find the agent that has the example.com domain state
	var withDomain *api.BrowserIdentity
	for _, a := range agents {
		if _, ok := a.Domains["example.com"]; ok {
			withDomain = &a
			break
		}
	}
	if withDomain == nil {
		t.Fatal("no agent has example.com domain state after restore")
	}

	state := withDomain.Domains["example.com"]
	if !state.HasCFClearance {
		t.Fatal("expected CF clearance to be preserved")
	}
	if len(state.Cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(state.Cookies))
	}

	// Verify chrome agent has its IP
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
}

func TestStoreLoadMissing(t *testing.T) {
	pool := NewPool()
	store := NewStore(pool, "/nonexistent/path/pool.json", time.Minute)

	// Should not error — just starts fresh
	if err := store.Load(); err != nil {
		t.Fatalf("load of missing file should not error: %v", err)
	}
}
