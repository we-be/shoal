package remora

import (
	"testing"

	"github.com/we-be/shoal/internal/api"
)

func TestScanGoodResponse(t *testing.T) {
	d := Scan(&api.NavigateResponse{
		HTML:        "<html><head><title>Hello</title></head><body><p>Real content here with enough text to be meaningful</p></body></html>",
		ContentSize: 5000,
		Title:       "Hello",
	})
	if d.Quality != "good" {
		t.Fatalf("expected good, got %s (%v)", d.Quality, d.Hints)
	}
}

func TestScanEmpty(t *testing.T) {
	d := Scan(&api.NavigateResponse{ContentSize: 50, HTML: "<html></html>"})
	if d.Quality != "empty" {
		t.Fatalf("expected empty, got %s", d.Quality)
	}
}

func TestScanCloudflareChallenge(t *testing.T) {
	d := Scan(&api.NavigateResponse{
		HTML:        "<html><head><title>Just a moment...</title></head><body>CF challenge</body></html>",
		ContentSize: 5000,
		Title:       "Just a moment...",
	})
	if !d.Blocked || d.System != "cloudflare" || d.Type != "challenge" {
		t.Fatalf("expected cloudflare challenge, got blocked=%v system=%s type=%s", d.Blocked, d.System, d.Type)
	}
	if d.Suggest != "retry_heavy" {
		t.Fatalf("expected retry_heavy, got %s", d.Suggest)
	}
}

func TestScanCloudflareTurnstile(t *testing.T) {
	d := Scan(&api.NavigateResponse{
		HTML:        "<html><body><div id='cf-turnstile'></div><script src='challenges.cloudflare.com/cdn'></script></body></html>",
		ContentSize: 5000,
		Title:       "Site",
	})
	if !d.Blocked || d.System != "cloudflare" {
		t.Fatalf("expected cloudflare block, got blocked=%v system=%s", d.Blocked, d.System)
	}
}

func TestScanDataDome(t *testing.T) {
	d := Scan(&api.NavigateResponse{
		HTML:        "<html><body><script src='dd.datadome.co/tags.js'></script></body></html>",
		ContentSize: 5000,
		Title:       "Site",
	})
	if !d.Blocked || d.System != "datadome" {
		t.Fatalf("expected datadome block, got blocked=%v system=%s", d.Blocked, d.System)
	}
}

func TestScanPaywall(t *testing.T) {
	d := Scan(&api.NavigateResponse{
		HTML:        "<html><body><p>Subscribe to continue reading this article</p></body></html>",
		ContentSize: 5000,
		Title:       "Article",
	})
	if d.Quality != "partial" || d.Type != "paywall" {
		t.Fatalf("expected partial/paywall, got %s/%s", d.Quality, d.Type)
	}
	if d.Suggest != "skip" {
		t.Fatalf("expected skip, got %s", d.Suggest)
	}
}

func TestScanRateLimit(t *testing.T) {
	d := Scan(&api.NavigateResponse{
		HTML:        "<html><body>Rate limit exceeded</body></html>",
		ContentSize: 500,
		Status:      429,
		Title:       "Error",
	})
	if !d.Blocked || d.Type != "rate_limit" {
		t.Fatalf("expected rate_limit, got blocked=%v type=%s", d.Blocked, d.Type)
	}
	if d.Suggest != "wait" {
		t.Fatalf("expected wait, got %s", d.Suggest)
	}
}

func TestScanGeoBlock(t *testing.T) {
	d := Scan(&api.NavigateResponse{
		HTML:        "<html><body>This content is not available in your region</body></html>",
		ContentSize: 500,
		Title:       "Unavailable",
	})
	if !d.Blocked || d.Type != "geo_block" {
		t.Fatalf("expected geo_block, got blocked=%v type=%s", d.Blocked, d.Type)
	}
	if d.Suggest != "retry_proxy" {
		t.Fatalf("expected retry_proxy, got %s", d.Suggest)
	}
}
