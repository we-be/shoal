package agent

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"strings"
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

func NewTLSClientBackend(userAgent string, proxy *api.ProxyConfig) (*TLSClientBackend, error) {
	if userAgent == "" {
		userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"
	}

	jar := tls_client.NewCookieJar()
	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithClientProfile(profiles.Chrome_146),
		tls_client.WithCookieJar(jar),
		tls_client.WithInsecureSkipVerify(),
	}

	client, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
	if err != nil {
		return nil, fmt.Errorf("creating tls client: %w", err)
	}

	// Set proxy if configured
	if proxy != nil && proxy.URL != "" {
		proxyURL := proxy.URL
		// tls-client expects http://user:pass@host:port format
		if proxy.Username != "" {
			proxyURL = insertProxyAuth(proxy.URL, proxy.Username, proxy.Password)
		}
		if err := client.SetProxy(proxyURL); err != nil {
			return nil, fmt.Errorf("setting proxy: %w", err)
		}
		log.Printf("tls-client proxy: %s", proxy.URL)
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
		"sec-ch-ua":       {`"Google Chrome";v="146", "Chromium";v="146", "Not_A Brand";v="24"`},
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

// SetProxy applies a proxy at runtime (called when controller assigns from pool).
func (t *TLSClientBackend) SetProxy(proxy *api.ProxyConfig) error {
	proxyURL := proxy.URL
	if proxy.Username != "" {
		proxyURL = insertProxyAuth(proxy.URL, proxy.Username, proxy.Password)
	}
	if err := t.client.SetProxy(proxyURL); err != nil {
		return fmt.Errorf("setting proxy: %w", err)
	}
	log.Printf("tls-client proxy set: %s", proxy.URL)
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

// insertProxyAuth inserts user:pass into a proxy URL.
// http://host:port -> http://user:pass@host:port
func insertProxyAuth(proxyURL, username, password string) string {
	auth := url.UserPassword(username, password).String()
	if strings.HasPrefix(proxyURL, "http://") {
		return "http://" + auth + "@" + strings.TrimPrefix(proxyURL, "http://")
	}
	if strings.HasPrefix(proxyURL, "https://") {
		return "https://" + auth + "@" + strings.TrimPrefix(proxyURL, "https://")
	}
	if strings.HasPrefix(proxyURL, "socks5://") {
		return "socks5://" + auth + "@" + strings.TrimPrefix(proxyURL, "socks5://")
	}
	return proxyURL
}
