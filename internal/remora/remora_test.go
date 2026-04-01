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

func TestScanImperva(t *testing.T) {
	d := Scan(&api.NavigateResponse{
		HTML:        "<html><body>Request unsuccessful. Incapsula incident ID: 123</body></html>",
		ContentSize: 5000,
		Title:       "Request Unsuccessful",
	})
	if !d.Blocked || d.System != "imperva" {
		t.Fatalf("expected imperva block, got blocked=%v system=%s", d.Blocked, d.System)
	}
}

func TestScanAWSWAF(t *testing.T) {
	d := Scan(&api.NavigateResponse{
		HTML:        "<html><body>This request was blocked by AWS WAF</body></html>",
		ContentSize: 500,
		Title:       "Request Blocked",
	})
	if !d.Blocked || d.System != "aws_waf" {
		t.Fatalf("expected aws_waf block, got blocked=%v system=%s", d.Blocked, d.System)
	}
	if d.Suggest != "retry_proxy" {
		t.Fatalf("expected retry_proxy for AWS WAF, got %s", d.Suggest)
	}
}

func TestScanShape(t *testing.T) {
	d := Scan(&api.NavigateResponse{
		HTML:        "<html><body><script>var _imp_apg_r_ = '123';</script></body></html>",
		ContentSize: 5000,
		Title:       "Site",
	})
	if !d.Blocked || d.System != "shape" {
		t.Fatalf("expected shape block, got blocked=%v system=%s", d.Blocked, d.System)
	}
}

func TestScanAkamai(t *testing.T) {
	d := Scan(&api.NavigateResponse{
		HTML:        "<html><body><script>var _abck='abc'; var ak_bmsc='xyz';</script>Akamai Bot Manager</body></html>",
		ContentSize: 5000,
		Title:       "Site",
	})
	if !d.Blocked || d.System != "akamai" {
		t.Fatalf("expected akamai block, got blocked=%v system=%s", d.Blocked, d.System)
	}
}

func TestScanPerimeterX(t *testing.T) {
	d := Scan(&api.NavigateResponse{
		HTML:        "<html><body><div id='px-captcha'>PerimeterX challenge</div></body></html>",
		ContentSize: 5000,
		Title:       "Site",
	})
	if !d.Blocked || d.System != "perimeterx" {
		t.Fatalf("expected perimeterx block, got blocked=%v system=%s", d.Blocked, d.System)
	}
}

func TestScanKasada(t *testing.T) {
	d := Scan(&api.NavigateResponse{
		HTML:        "<html><body><script src='/_sec/cp_challenge/ak-challenge'></script></body></html>",
		ContentSize: 5000,
		Title:       "Site",
	})
	if !d.Blocked || d.System != "kasada" {
		t.Fatalf("expected kasada block, got blocked=%v system=%s", d.Blocked, d.System)
	}
}

func TestScanGenericBot(t *testing.T) {
	d := Scan(&api.NavigateResponse{
		HTML:        "<html><body><h1>Access Denied</h1><p>Please verify you are a human</p></body></html>",
		ContentSize: 5000,
		Title:       "Blocked",
	})
	if d.Quality != "blocked" {
		t.Fatalf("expected blocked, got %s", d.Quality)
	}
}

func TestScanJSShell(t *testing.T) {
	// Build a large page with many scripts but little text
	scripts := ""
	for i := 0; i < 15; i++ {
		scripts += "<script src='bundle" + string(rune('a'+i)) + ".js'></script>"
	}
	html := "<html><head>" + scripts + "</head><body><div id='root'></div></body></html>"
	// Pad to 60KB
	for len(html) < 60000 {
		html += "<!-- padding -->"
	}

	d := Scan(&api.NavigateResponse{
		HTML:        html,
		ContentSize: len(html),
		Title:       "App",
	})
	if d.Quality != "partial" || d.Type != "js_shell" {
		t.Fatalf("expected partial/js_shell, got %s/%s", d.Quality, d.Type)
	}
}

func TestScanAppError(t *testing.T) {
	d := Scan(&api.NavigateResponse{
		HTML:        "<html><head><title>Application error: a client-side exception has occurred</title></head><body></body></html>",
		ContentSize: 5000,
		Title:       "Application error: a client-side exception has occurred",
	})
	if d.Quality != "partial" || d.Type != "app_error" {
		t.Fatalf("expected partial/app_error, got %s/%s", d.Quality, d.Type)
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
