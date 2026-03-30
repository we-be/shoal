package controller

import (
	"strings"

	"github.com/we-be/shoal/internal/api"
)

// scoreResponseQuality analyzes a NavigateResponse and sets the Quality
// and QualityHints fields. Callers can use these to decide whether to
// retry with a heavier agent class.
//
// Quality levels:
//   - "good"    — real content, usable as-is
//   - "partial" — some content but degraded (paywall, truncated, JS-only)
//   - "blocked" — CF challenge, bot detection, or access denied
//   - "empty"   — no meaningful content returned
func scoreResponseQuality(resp *api.NavigateResponse) {
	var hints []string

	// Empty
	if resp.ContentSize < 100 {
		resp.Quality = "empty"
		resp.QualityHints = []string{"response under 100 bytes"}
		return
	}

	html := resp.HTML
	lower := strings.ToLower(html)
	title := strings.ToLower(resp.Title)

	// CF challenge
	if strings.Contains(title, "just a moment") ||
		strings.Contains(title, "attention required") ||
		strings.Contains(title, "ddos-guard") {
		resp.Quality = "blocked"
		resp.QualityHints = []string{"cloudflare challenge page"}
		return
	}

	// Bot detection / access denied
	botSignals := []struct {
		pattern string
		hint    string
	}{
		{"access denied", "access denied"},
		{"403 forbidden", "403 forbidden"},
		{"robot", "bot detection"},
		{"captcha", "captcha required"},
		{"please verify you are a human", "human verification"},
		{"enable javascript", "javascript required"},
		{"browser is not supported", "browser not supported"},
	}
	for _, sig := range botSignals {
		if strings.Contains(lower, sig.pattern) {
			hints = append(hints, sig.hint)
		}
	}
	if len(hints) > 0 {
		resp.Quality = "blocked"
		resp.QualityHints = hints
		return
	}

	// Paywall / login wall
	paywallSignals := []struct {
		pattern string
		hint    string
	}{
		{"subscribe to continue", "paywall"},
		{"subscription required", "paywall"},
		{"sign in to read", "login wall"},
		{"create a free account", "login wall"},
		{"this content is for members", "members only"},
		{"premium content", "premium content"},
	}
	for _, sig := range paywallSignals {
		if strings.Contains(lower, sig.pattern) {
			hints = append(hints, sig.hint)
		}
	}

	// JS-rendered shell (lots of script tags, very little text)
	if strings.Count(lower, "<script") > 10 && resp.ContentSize > 50000 {
		// Check text-to-markup ratio
		textLen := len(strings.TrimSpace(stripTags(html)))
		if textLen < resp.ContentSize/20 {
			hints = append(hints, "js-heavy shell (low text ratio)")
		}
	}

	// Client-side error
	if strings.Contains(title, "application error") ||
		strings.Contains(title, "client-side exception") {
		hints = append(hints, "client-side app error")
	}

	// Small content might be partial (but APIs can be legitimately small)
	if resp.ContentSize < 500 && resp.Status == 200 {
		hints = append(hints, "very small response")
	}

	// No title is suspicious for large pages (APIs and small pages don't need titles)
	if resp.Title == "" && resp.ContentSize > 10000 {
		hints = append(hints, "no page title")
	}

	// Classify
	if len(hints) > 0 {
		// Check if any are hard blocks
		for _, h := range hints {
			if h == "paywall" || h == "login wall" || h == "members only" || h == "client-side app error" {
				resp.Quality = "partial"
				resp.QualityHints = hints
				return
			}
		}
		resp.Quality = "partial"
		resp.QualityHints = hints
		return
	}

	resp.Quality = "good"
}

// stripTags removes HTML tags for text ratio calculation.
func stripTags(html string) string {
	var b strings.Builder
	inTag := false
	for _, r := range html {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
		} else if !inTag {
			b.WriteRune(r)
		}
	}
	return b.String()
}
