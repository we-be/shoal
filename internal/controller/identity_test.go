package controller

import (
	"testing"
	"time"

	"github.com/we-be/shoal/internal/api"
)

func TestNewFishID(t *testing.T) {
	ids := make(map[string]bool)
	for range 100 {
		id := newFishID()
		if id == "" {
			t.Fatal("empty fish ID")
		}
		if ids[id] {
			t.Fatalf("duplicate fish ID: %s", id)
		}
		ids[id] = true
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://www.example.com/path", "example.com"},
		{"https://example.com", "example.com"},
		{"https://sub.example.com/foo?bar=1", "sub.example.com"},
		{"https://www.hapag-lloyd.com/en/tracking", "hapag-lloyd.com"},
		{"http://localhost:9090/login", "localhost"},
		{"garbage", "garbage"},
	}

	for _, tt := range tests {
		got := extractDomain(tt.url)
		if got != tt.expected {
			t.Errorf("extractDomain(%q) = %q, want %q", tt.url, got, tt.expected)
		}
	}
}

func TestUpdateIdentity(t *testing.T) {
	identity := newIdentity("stub", "heavy", "1.2.3.4")

	// First visit
	updateIdentity(identity, "https://example.com/page1", []api.Cookie{
		{Name: "session", Value: "abc", Domain: "example.com"},
	})

	if identity.UseCount != 1 {
		t.Fatalf("expected 1 use, got %d", identity.UseCount)
	}

	state := identity.Domains["example.com"]
	if state == nil {
		t.Fatal("expected domain state for example.com")
	}
	if state.VisitCount != 1 {
		t.Fatalf("expected 1 visit, got %d", state.VisitCount)
	}
	if len(state.Cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(state.Cookies))
	}

	// Second visit with CF clearance
	updateIdentity(identity, "https://example.com/page2", []api.Cookie{
		{Name: "cf_clearance", Value: "xyz", Domain: "example.com", Expires: float64(time.Now().Add(time.Hour).Unix())},
	})

	if state.VisitCount != 2 {
		t.Fatalf("expected 2 visits, got %d", state.VisitCount)
	}
	if !state.HasCFClearance {
		t.Fatal("expected CF clearance to be flagged")
	}
	if state.CFExpiry == nil {
		t.Fatal("expected CF expiry to be set")
	}
	if state.CFURL != "https://example.com/page2" {
		t.Fatalf("expected CFURL to be set, got %q", state.CFURL)
	}
}

func TestMergeCookies(t *testing.T) {
	existing := []api.Cookie{
		{Name: "a", Value: "1", Domain: "example.com"},
		{Name: "b", Value: "2", Domain: "example.com"},
	}

	incoming := []api.Cookie{
		{Name: "a", Value: "updated", Domain: "example.com"},
		{Name: "c", Value: "3", Domain: "example.com"},
	}

	merged := mergeCookies(existing, incoming, "example.com")

	if len(merged) != 3 {
		t.Fatalf("expected 3 cookies, got %d", len(merged))
	}

	byName := make(map[string]api.Cookie)
	for _, c := range merged {
		byName[c.Name] = c
	}

	if byName["a"].Value != "updated" {
		t.Errorf("cookie 'a' should be updated, got %q", byName["a"].Value)
	}
	if byName["b"].Value != "2" {
		t.Errorf("cookie 'b' should be preserved, got %q", byName["b"].Value)
	}
	if byName["c"].Value != "3" {
		t.Errorf("cookie 'c' should be added, got %q", byName["c"].Value)
	}
}

func TestMergeCookiesPrunesExpired(t *testing.T) {
	existing := []api.Cookie{
		{Name: "expired", Value: "old", Domain: "example.com", Expires: 1000}, // long past
		{Name: "valid", Value: "ok", Domain: "example.com", Expires: float64(time.Now().Add(time.Hour).Unix())},
	}

	merged := mergeCookies(existing, nil, "example.com")

	if len(merged) != 1 {
		t.Fatalf("expected 1 cookie (expired pruned), got %d", len(merged))
	}
	if merged[0].Name != "valid" {
		t.Fatalf("expected 'valid' cookie to survive, got %q", merged[0].Name)
	}
}

func TestDomainWarmth(t *testing.T) {
	identity := newIdentity("stub", "heavy", "")

	// Cold
	if w := domainWarmth(identity, "unknown.com"); w != 0 {
		t.Fatalf("expected 0 warmth for unknown domain, got %d", w)
	}

	// Visited (warmth 1 — after visit with no cookies... actually we always get cookies from navigate)
	// With cookies (warmth 2)
	updateIdentity(identity, "https://warm.com/", []api.Cookie{
		{Name: "session", Value: "x", Domain: "warm.com"},
	})
	if w := domainWarmth(identity, "warm.com"); w != 2 {
		t.Fatalf("expected warmth 2 (has cookies), got %d", w)
	}

	// CF clearance (warmth 3)
	updateIdentity(identity, "https://cf.com/", []api.Cookie{
		{Name: "cf_clearance", Value: "x", Domain: "cf.com", Expires: float64(time.Now().Add(time.Hour).Unix())},
	})
	if w := domainWarmth(identity, "cf.com"); w != 3 {
		t.Fatalf("expected warmth 3 (cf clearance), got %d", w)
	}
}
