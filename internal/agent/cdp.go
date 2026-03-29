package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/we-be/shoal/internal/api"
)

// CDPBackend connects to any CDP-speaking browser over WebSocket.
// Browser-agnostic — works with Lightpanda, Chrome, Chromium, or anything
// that speaks the Chrome DevTools Protocol. Like the tide, it doesn't care
// what kind of fish is swimming.
type CDPBackend struct {
	allocCtx    context.Context
	allocCancel context.CancelFunc
	cmd         *exec.Cmd // non-nil if we launched the browser subprocess
	started     time.Time
	name        string
}

// NewCDPBackend connects to an existing CDP endpoint at the given WebSocket URL.
func NewCDPBackend(wsURL string) (*CDPBackend, error) {
	// Try to discover the real WebSocket URL from /json/version
	resolved, err := discoverCDPURL(wsURL)
	if err != nil {
		log.Printf("cdp discovery failed, using direct URL: %v", err)
		resolved = wsURL
	}

	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), resolved)

	return &CDPBackend{
		allocCtx:    allocCtx,
		allocCancel: allocCancel,
		started:     time.Now(),
		name:        "cdp",
	}, nil
}

// NewLightpandaBackend launches a Lightpanda process and connects via CDP.
// Each agent gets its own browser — one fish in the shoal.
func NewLightpandaBackend(binPath string, cdpPort int) (*CDPBackend, error) {
	portStr := strconv.Itoa(cdpPort)

	cmd := exec.Command(binPath, "serve", "--host", "127.0.0.1", "--port", portStr,
		"--insecure-disable-tls-host-verification")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting lightpanda: %w", err)
	}

	log.Printf("lightpanda started (pid=%d, port=%d)", cmd.Process.Pid, cdpPort)

	// Wait for CDP to be ready
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", cdpPort)
	wsURL, err := waitForCDP(baseURL, 10*time.Second)
	if err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("lightpanda didn't come up: %w", err)
	}

	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), wsURL)

	return &CDPBackend{
		allocCtx:    allocCtx,
		allocCancel: allocCancel,
		cmd:         cmd,
		started:     time.Now(),
		name:        "lightpanda",
	}, nil
}

func (b *CDPBackend) Navigate(ctx context.Context, req api.NavigateRequest) (*api.NavigateResponse, error) {
	// Each request gets a fresh tab — clean slate
	tabCtx, tabCancel := chromedp.NewContext(b.allocCtx)
	defer tabCancel()

	timeout := 30 * time.Second
	if req.MaxTimeout > 0 {
		timeout = time.Duration(req.MaxTimeout) * time.Millisecond
	}
	tabCtx, timeoutCancel := context.WithTimeout(tabCtx, timeout)
	defer timeoutCancel()

	var html string
	var currentURL string
	var cookies []*network.Cookie

	err := chromedp.Run(tabCtx,
		chromedp.Navigate(req.URL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
		chromedp.Location(&currentURL),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			cookies, err = network.GetCookies().Do(ctx)
			return err
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("navigating to %s: %w", req.URL, err)
	}

	// Convert CDP cookies to our format
	apiCookies := make([]api.Cookie, len(cookies))
	for i, c := range cookies {
		apiCookies[i] = api.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
		}
	}

	return &api.NavigateResponse{
		URL:     currentURL,
		Status:  200,
		HTML:    html,
		Cookies: apiCookies,
	}, nil
}

func (b *CDPBackend) Health() api.HealthStatus {
	status := "ok"

	// If we own the process, check it's still swimming
	if b.cmd != nil && b.cmd.Process != nil {
		if err := b.cmd.Process.Signal(syscall.Signal(0)); err != nil {
			status = "unhealthy"
		}
	}

	return api.HealthStatus{
		Status:  status,
		Backend: b.name,
		Uptime:  int64(time.Since(b.started).Seconds()),
	}
}

func (b *CDPBackend) Close() error {
	b.allocCancel()

	if b.cmd != nil && b.cmd.Process != nil {
		log.Printf("stopping %s (pid=%d)", b.name, b.cmd.Process.Pid)
		// Ask nicely first
		b.cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- b.cmd.Wait() }()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			b.cmd.Process.Kill()
		}
	}
	return nil
}

// discoverCDPURL queries /json/version to find the browser's WebSocket URL.
// Falls back to constructing a URL from the base if discovery fails.
func discoverCDPURL(rawURL string) (string, error) {
	// Normalize to HTTP for the discovery request
	httpURL := rawURL
	httpURL = strings.Replace(httpURL, "ws://", "http://", 1)
	httpURL = strings.Replace(httpURL, "wss://", "https://", 1)
	httpURL = strings.TrimSuffix(httpURL, "/")

	resp, err := http.Get(httpURL + "/json/version")
	if err != nil {
		return "", fmt.Errorf("discovery request failed: %w", err)
	}
	defer resp.Body.Close()

	var version struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&version); err != nil {
		return "", fmt.Errorf("decoding /json/version: %w", err)
	}

	if version.WebSocketDebuggerURL != "" {
		return version.WebSocketDebuggerURL, nil
	}

	return "", fmt.Errorf("no webSocketDebuggerUrl in /json/version")
}

// waitForCDP polls the CDP endpoint until it responds or times out.
// Returns the discovered WebSocket URL.
func waitForCDP(baseURL string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		wsURL, err := discoverCDPURL(baseURL)
		if err == nil {
			return wsURL, nil
		}

		// Also try direct WebSocket URL as fallback
		wsBase := strings.Replace(baseURL, "http://", "ws://", 1)
		resp, err := http.Get(baseURL + "/json/version")
		if err == nil {
			resp.Body.Close()
			return wsBase, nil
		}

		time.Sleep(200 * time.Millisecond)
	}

	return "", fmt.Errorf("timeout waiting for CDP at %s", baseURL)
}
