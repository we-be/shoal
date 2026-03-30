// Package remora detects blocks, challenges, and degraded responses.
// Named after the remora fish — rides alongside the shoal, cleans up
// the mess, identifies what went wrong so the school can adapt.
//
// Usage:
//
//	detection := remora.Scan(resp)
//	if detection.Blocked {
//	    log.Printf("blocked by %s: %s", detection.System, detection.Suggest)
//	}
package remora

import (
	"strings"

	"github.com/we-be/shoal/internal/api"
)

// Detection is the result of scanning a response for blocks.
type Detection struct {
	Blocked    bool     `json:"blocked"`
	Quality    string   `json:"quality"`              // "good", "partial", "blocked", "empty"
	System     string   `json:"system,omitempty"`      // "cloudflare", "akamai", "datadome", etc.
	Type       string   `json:"type,omitempty"`        // "challenge", "bot_detect", "paywall", "rate_limit", "geo_block"
	Confidence float64  `json:"confidence"`            // 0.0 - 1.0
	Hints      []string `json:"hints,omitempty"`
	Suggest    string   `json:"suggest,omitempty"`     // "retry_heavy", "retry_proxy", "wait", "skip"
}

// Scan analyzes a NavigateResponse for signs of blocking, challenges,
// paywalls, and degraded content. Returns a Detection with actionable
// recommendations.
func Scan(resp *api.NavigateResponse) Detection {
	if resp.ContentSize < 100 {
		return Detection{
			Quality:    "empty",
			Confidence: 1.0,
			Hints:      []string{"response under 100 bytes"},
			Suggest:    "retry_heavy",
		}
	}

	lower := strings.ToLower(resp.HTML)
	title := strings.ToLower(resp.Title)

	// --- Bot Management Systems ---

	// Cloudflare
	if d := detectCloudflare(title, lower); d != nil {
		return *d
	}

	// Akamai Bot Manager
	if d := detectAkamai(title, lower); d != nil {
		return *d
	}

	// DataDome
	if d := detectDataDome(title, lower); d != nil {
		return *d
	}

	// PerimeterX / HUMAN
	if d := detectPerimeterX(title, lower); d != nil {
		return *d
	}

	// Kasada
	if d := detectKasada(lower); d != nil {
		return *d
	}

	// Generic bot detection
	if d := detectGenericBot(title, lower); d != nil {
		return *d
	}

	// --- Access Restrictions ---

	// Paywall
	if d := detectPaywall(lower); d != nil {
		return *d
	}

	// Rate limiting
	if d := detectRateLimit(lower, resp.Status); d != nil {
		return *d
	}

	// Geo-blocking
	if d := detectGeoBlock(lower); d != nil {
		return *d
	}

	// --- Content Quality ---

	// JS shell (SPA that didn't render)
	if d := detectJSShell(lower, resp.ContentSize); d != nil {
		return *d
	}

	// Client-side app error
	if strings.Contains(title, "application error") || strings.Contains(title, "client-side exception") {
		return Detection{
			Quality:    "partial",
			Type:       "app_error",
			Confidence: 0.9,
			Hints:      []string{"client-side application error"},
			Suggest:    "retry_heavy",
		}
	}

	// Small response
	if resp.ContentSize < 500 {
		return Detection{
			Quality:    "partial",
			Confidence: 0.5,
			Hints:      []string{"very small response"},
			Suggest:    "retry_heavy",
		}
	}

	return Detection{
		Quality:    "good",
		Confidence: 1.0,
	}
}

// --- Cloudflare ---

func detectCloudflare(title, body string) *Detection {
	// Challenge page
	if strings.Contains(title, "just a moment") ||
		strings.Contains(title, "attention required") {
		return &Detection{
			Blocked:    true,
			Quality:    "blocked",
			System:     "cloudflare",
			Type:       "challenge",
			Confidence: 1.0,
			Hints:      []string{"cloudflare challenge page"},
			Suggest:    "retry_heavy",
		}
	}

	// Turnstile widget
	if strings.Contains(body, "cf-turnstile") || strings.Contains(body, "challenges.cloudflare.com") {
		return &Detection{
			Blocked:    true,
			Quality:    "blocked",
			System:     "cloudflare",
			Type:       "challenge",
			Confidence: 0.95,
			Hints:      []string{"cloudflare turnstile detected"},
			Suggest:    "retry_heavy",
		}
	}

	// CF error pages
	if strings.Contains(body, "cf-error-details") || strings.Contains(body, "cloudflare ray id") {
		return &Detection{
			Blocked:    true,
			Quality:    "blocked",
			System:     "cloudflare",
			Type:       "error",
			Confidence: 0.9,
			Hints:      []string{"cloudflare error page"},
			Suggest:    "retry_proxy",
		}
	}

	return nil
}

// --- Akamai Bot Manager ---

func detectAkamai(title, body string) *Detection {
	signals := 0
	var hints []string

	if strings.Contains(body, "akamai") && strings.Contains(body, "bot manager") {
		signals += 2
		hints = append(hints, "akamai bot manager detected")
	}
	if strings.Contains(body, "_abck") { // Akamai sensor cookie
		signals++
		hints = append(hints, "akamai sensor cookie")
	}
	if strings.Contains(body, "ak_bmsc") {
		signals++
		hints = append(hints, "akamai bot management cookie")
	}

	if signals >= 2 {
		return &Detection{
			Blocked:    true,
			Quality:    "blocked",
			System:     "akamai",
			Type:       "bot_detect",
			Confidence: float64(signals) * 0.3,
			Hints:      hints,
			Suggest:    "retry_heavy",
		}
	}
	return nil
}

// --- DataDome ---

func detectDataDome(title, body string) *Detection {
	if strings.Contains(body, "datadome") ||
		strings.Contains(body, "dd.datadome") ||
		strings.Contains(body, "geo.captcha-delivery.com") {
		return &Detection{
			Blocked:    true,
			Quality:    "blocked",
			System:     "datadome",
			Type:       "bot_detect",
			Confidence: 0.95,
			Hints:      []string{"datadome bot detection"},
			Suggest:    "retry_heavy",
		}
	}
	return nil
}

// --- PerimeterX / HUMAN ---

func detectPerimeterX(title, body string) *Detection {
	if strings.Contains(body, "perimeterx") ||
		strings.Contains(body, "_pxhd") ||
		strings.Contains(body, "px-captcha") ||
		strings.Contains(body, "human security") {
		return &Detection{
			Blocked:    true,
			Quality:    "blocked",
			System:     "perimeterx",
			Type:       "bot_detect",
			Confidence: 0.9,
			Hints:      []string{"perimeterx/human bot detection"},
			Suggest:    "retry_heavy",
		}
	}
	return nil
}

// --- Kasada ---

func detectKasada(body string) *Detection {
	if strings.Contains(body, "kasada") || strings.Contains(body, "/_sec/cp_challenge") {
		return &Detection{
			Blocked:    true,
			Quality:    "blocked",
			System:     "kasada",
			Type:       "bot_detect",
			Confidence: 0.9,
			Hints:      []string{"kasada bot detection"},
			Suggest:    "retry_heavy",
		}
	}
	return nil
}

// --- Generic Bot Detection ---

func detectGenericBot(title, body string) *Detection {
	var hints []string
	signals := 0

	patterns := []struct {
		pattern string
		hint    string
	}{
		{"access denied", "access denied"},
		{"403 forbidden", "403 forbidden"},
		{"please verify you are a human", "human verification"},
		{"unusual traffic", "unusual traffic detected"},
		{"automated access", "automated access detected"},
		{"browser is not supported", "browser not supported"},
		{"enable javascript to continue", "javascript required"},
		{"please enable cookies", "cookies required"},
	}

	for _, p := range patterns {
		if strings.Contains(body, p.pattern) {
			signals++
			hints = append(hints, p.hint)
		}
	}

	if signals > 0 {
		return &Detection{
			Blocked:    signals >= 2,
			Quality:    "blocked",
			System:     "generic",
			Type:       "bot_detect",
			Confidence: float64(signals) * 0.4,
			Hints:      hints,
			Suggest:    "retry_heavy",
		}
	}
	return nil
}

// --- Paywall ---

func detectPaywall(body string) *Detection {
	var hints []string

	patterns := []struct {
		pattern string
		hint    string
	}{
		{"subscribe to continue", "paywall"},
		{"subscription required", "paywall"},
		{"sign in to read", "login wall"},
		{"create a free account", "login wall"},
		{"this content is for members", "members only"},
		{"premium content", "premium content"},
		{"start your free trial", "trial wall"},
	}

	for _, p := range patterns {
		if strings.Contains(body, p.pattern) {
			hints = append(hints, p.hint)
		}
	}

	if len(hints) > 0 {
		return &Detection{
			Quality:    "partial",
			Type:       "paywall",
			Confidence: 0.8,
			Hints:      hints,
			Suggest:    "skip",
		}
	}
	return nil
}

// --- Rate Limiting ---

func detectRateLimit(body string, status int) *Detection {
	if status == 429 || strings.Contains(body, "rate limit") || strings.Contains(body, "too many requests") {
		return &Detection{
			Blocked:    true,
			Quality:    "blocked",
			Type:       "rate_limit",
			Confidence: 0.95,
			Hints:      []string{"rate limited"},
			Suggest:    "wait",
		}
	}
	return nil
}

// --- Geo-blocking ---

func detectGeoBlock(body string) *Detection {
	if strings.Contains(body, "not available in your region") ||
		strings.Contains(body, "not available in your country") ||
		strings.Contains(body, "geo-restricted") {
		return &Detection{
			Blocked:    true,
			Quality:    "blocked",
			Type:       "geo_block",
			Confidence: 0.9,
			Hints:      []string{"geo-restricted content"},
			Suggest:    "retry_proxy",
		}
	}
	return nil
}

// --- JS Shell Detection ---

func detectJSShell(body string, size int) *Detection {
	if size < 50000 {
		return nil
	}

	scriptCount := strings.Count(body, "<script")
	if scriptCount <= 10 {
		return nil
	}

	// Rough text-to-markup ratio
	textLen := 0
	inTag := false
	for _, r := range body {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
		} else if !inTag && r > ' ' {
			textLen++
		}
	}

	if textLen < size/20 {
		return &Detection{
			Quality:    "partial",
			Type:       "js_shell",
			Confidence: 0.7,
			Hints:      []string{"js-heavy shell, low text ratio", "content may need browser rendering"},
			Suggest:    "retry_heavy",
		}
	}

	return nil
}
