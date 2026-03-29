package agent

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"sync"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/we-be/shoal/internal/api"
)

// TLSClientBackend makes HTTP requests with Chrome's exact TLS fingerprint.
// No browser, no DOM, no JS — just raw HTTP that looks like Chrome to
// Cloudflare's JA3/JA4 fingerprinting. This is the minnow: small, fast,
// and travels in schools.
//
// Use it to make bulk requests with cookies earned by a heavy browser
// (the grouper). The cf_clearance cookie is bound to TLS fingerprint +
// IP + User-Agent, so we match all three.
type TLSClientBackend struct {
	client    tls_client.HttpClient
	userAgent string
	mu        sync.Mutex // protects cookie operations
	started   time.Time
}

func NewTLSClientBackend(userAgent string) (*TLSClientBackend, error) {
	if userAgent == "" {
		userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	}

	jar := tls_client.NewCookieJar()
	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithClientProfile(profiles.Chrome_131),
		tls_client.WithCookieJar(jar),
		tls_client.WithInsecureSkipVerify(),
	}

	client, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
	if err != nil {
		return nil, fmt.Errorf("creating tls client: %w", err)
	}

	return &TLSClientBackend{
		client:    client,
		userAgent: userAgent,
		started:   time.Now(),
	}, nil
}

func (t *TLSClientBackend) Navigate(ctx context.Context, req api.NavigateRequest) (*api.NavigateResponse, error) {
	httpReq, err := http.NewRequest(http.MethodGet, req.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	httpReq.Header = http.Header{
		"accept":          {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8"},
		"accept-language": {"en-US,en;q=0.9"},
		"user-agent":      {t.userAgent},
		"sec-ch-ua":       {`"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`},
		"sec-ch-ua-mobile":   {"?0"},
		"sec-ch-ua-platform": {`"Linux"`},
		"sec-fetch-dest":     {"document"},
		"sec-fetch-mode":     {"navigate"},
		"sec-fetch-site":     {"none"},
		"sec-fetch-user":     {"?1"},
		"upgrade-insecure-requests": {"1"},
		http.HeaderOrderKey: {
			"accept",
			"accept-language",
			"sec-ch-ua",
			"sec-ch-ua-mobile",
			"sec-ch-ua-platform",
			"sec-fetch-dest",
			"sec-fetch-mode",
			"sec-fetch-site",
			"sec-fetch-user",
			"upgrade-insecure-requests",
			"user-agent",
		},
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", req.URL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	// Collect cookies from the jar for this URL
	u, _ := url.Parse(req.URL)
	jarCookies := t.client.GetCookies(u)

	apiCookies := make([]api.Cookie, len(jarCookies))
	for i, c := range jarCookies {
		var expires float64
		if !c.Expires.IsZero() {
			expires = float64(c.Expires.Unix())
		}
		apiCookies[i] = api.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HttpOnly,
			Expires:  expires,
		}
	}

	return &api.NavigateResponse{
		URL:       resp.Request.URL.String(),
		Status:    resp.StatusCode,
		HTML:      string(body),
		Cookies:   apiCookies,
		UserAgent: t.userAgent,
	}, nil
}

// SetCookies injects cookies into the TLS client's jar. This is how the
// controller hands off cf_clearance from a grouper to minnows.
func (t *TLSClientBackend) SetCookies(targetURL string, cookies []api.Cookie) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	u, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("parsing url: %w", err)
	}

	httpCookies := make([]*http.Cookie, len(cookies))
	for i, c := range cookies {
		httpCookies[i] = &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HttpOnly: c.HTTPOnly,
		}
		if c.Expires > 0 {
			httpCookies[i].Expires = time.Unix(int64(c.Expires), 0)
		}
	}

	t.client.SetCookies(u, httpCookies)
	return nil
}

func (t *TLSClientBackend) Health() api.HealthStatus {
	return api.HealthStatus{
		Status:  api.HealthOK,
		Backend: api.BackendTLSClient,
		Uptime:  int64(time.Since(t.started).Seconds()),
	}
}

func (t *TLSClientBackend) Close() error {
	t.client.CloseIdleConnections()
	return nil
}
