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
// that speaks the Chrome DevTools Protocol.
//
// Maintains a persistent browser tab so cookies, sessions, and login state
// survive across navigations. This is the fish's memory — the accumulated
// dirt and oil on its hands.
type CDPBackend struct {
	allocCtx    context.Context
	allocCancel context.CancelFunc
	tabCtx      context.Context    // persistent tab — cookies live here
	tabCancel   context.CancelFunc
	cmd         *exec.Cmd // non-nil if we launched the browser subprocess
	started     time.Time
	name        string
}

// NewCDPBackend connects to an existing CDP endpoint at the given WebSocket URL.
func NewCDPBackend(wsURL string) (*CDPBackend, error) {
	resolved, err := discoverCDPURL(wsURL)
	if err != nil {
		log.Printf("cdp discovery failed, using direct URL: %v", err)
		resolved = wsURL
	}

	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), resolved)
	return initCDPBackend(allocCtx, allocCancel, nil, api.BackendCDP)
}

// NewLightpandaBackend launches a Lightpanda process and connects via CDP.
func NewLightpandaBackend(binPath string, cdpPort int) (*CDPBackend, error) {
	portStr := strconv.Itoa(cdpPort)

	cmd := exec.Command(binPath, "serve", "--host", "127.0.0.1", "--port", portStr,
		"--insecure-disable-tls-host-verification",
		"--timeout", "86400")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting lightpanda: %w", err)
	}

	log.Printf("lightpanda started (pid=%d, port=%d)", cmd.Process.Pid, cdpPort)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", cdpPort)
	wsURL, err := waitForCDP(baseURL, 10*time.Second)
	if err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("lightpanda didn't come up: %w", err)
	}

	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), wsURL)
	return initCDPBackend(allocCtx, allocCancel, cmd, api.BackendLightpanda)
}

// initCDPBackend creates a persistent browser tab that lives for the agent's lifetime.
func initCDPBackend(allocCtx context.Context, allocCancel context.CancelFunc, cmd *exec.Cmd, name string) (*CDPBackend, error) {
	// Create ONE persistent tab — this is the fish's body.
	// All navigations reuse this tab so cookies persist.
	tabCtx, tabCancel := chromedp.NewContext(allocCtx)

	// Warm up the tab
	if err := chromedp.Run(tabCtx, chromedp.Navigate("about:blank")); err != nil {
		tabCancel()
		allocCancel()
		if cmd != nil {
			cmd.Process.Kill()
		}
		return nil, fmt.Errorf("initializing browser tab: %w", err)
	}

	log.Printf("%s backend ready (persistent tab initialized)", name)

	return &CDPBackend{
		allocCtx:    allocCtx,
		allocCancel: allocCancel,
		tabCtx:      tabCtx,
		tabCancel:   tabCancel,
		cmd:         cmd,
		started:     time.Now(),
		name:        name,
	}, nil
}

func (b *CDPBackend) Navigate(ctx context.Context, req api.NavigateRequest) (*api.NavigateResponse, error) {
	timeout := 30 * time.Second
	if req.MaxTimeout > 0 {
		timeout = time.Duration(req.MaxTimeout) * time.Millisecond
	}

	// Child context with timeout — cancelling this does NOT kill the
	// persistent tab. The fish keeps swimming even if one request times out.
	navCtx, navCancel := context.WithTimeout(b.tabCtx, timeout)
	defer navCancel()

	// Navigate to the URL
	if err := chromedp.Run(navCtx,
		chromedp.Navigate(req.URL),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		return nil, fmt.Errorf("navigating to %s: %w", req.URL, err)
	}

	// Execute post-navigation actions (fill forms, click buttons, etc.)
	for _, action := range req.Actions {
		if err := executeAction(navCtx, action); err != nil {
			return nil, fmt.Errorf("action %s on %s: %w", action.Type, action.Selector, err)
		}
	}

	// Collect results after navigation + actions
	var html string
	var currentURL string
	var cookies []*network.Cookie

	if err := chromedp.Run(navCtx,
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
		chromedp.Location(&currentURL),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			cookies, err = network.GetCookies().Do(ctx)
			return err
		}),
	); err != nil {
		return nil, fmt.Errorf("collecting results: %w", err)
	}

	apiCookies := make([]api.Cookie, len(cookies))
	for i, c := range cookies {
		apiCookies[i] = api.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
			Expires:  float64(c.Expires),
		}
	}

	return &api.NavigateResponse{
		URL:     currentURL,
		Status:  200,
		HTML:    html,
		Cookies: apiCookies,
	}, nil
}

// executeAction runs a single browser automation step via JS evaluation.
// Uses Runtime.evaluate for broad CDP compatibility (works with Lightpanda,
// Chrome, and anything else that speaks the protocol).
func executeAction(ctx context.Context, action api.Action) error {
	switch action.Type {
	case "fill":
		js := fmt.Sprintf(
			`document.querySelector(%q).value = %q`,
			action.Selector, action.Value,
		)
		return chromedp.Run(ctx, chromedp.Evaluate(js, nil))

	case "click":
		js := fmt.Sprintf(`document.querySelector(%q).click()`, action.Selector)
		err := chromedp.Run(ctx, chromedp.Evaluate(js, nil))
		if err != nil {
			return err
		}
		// Wait for any navigation triggered by the click
		return chromedp.Run(ctx, chromedp.WaitReady("body", chromedp.ByQuery))

	case "submit":
		js := fmt.Sprintf(`document.querySelector(%q).submit()`, action.Selector)
		err := chromedp.Run(ctx, chromedp.Evaluate(js, nil))
		if err != nil {
			return err
		}
		return chromedp.Run(ctx, chromedp.WaitReady("body", chromedp.ByQuery))

	case "wait":
		if action.WaitMS > 0 {
			return chromedp.Run(ctx, chromedp.Sleep(time.Duration(action.WaitMS)*time.Millisecond))
		}
		js := fmt.Sprintf(
			`new Promise(r => { const check = () => document.querySelector(%q) ? r() : setTimeout(check, 100); check(); })`,
			action.Selector,
		)
		return chromedp.Run(ctx, chromedp.Evaluate(js, nil))

	case "eval":
		return chromedp.Run(ctx, chromedp.Evaluate(action.Value, nil))

	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
}

func (b *CDPBackend) Health() api.HealthStatus {
	status := api.HealthOK

	if b.cmd != nil && b.cmd.Process != nil {
		if err := b.cmd.Process.Signal(syscall.Signal(0)); err != nil {
			status = api.HealthUnhealthy
		}
	}

	return api.HealthStatus{
		Status:  status,
		Backend: b.name,
		Uptime:  int64(time.Since(b.started).Seconds()),
	}
}

func (b *CDPBackend) Close() error {
	b.tabCancel()
	b.allocCancel()

	if b.cmd != nil && b.cmd.Process != nil {
		log.Printf("stopping %s (pid=%d)", b.name, b.cmd.Process.Pid)
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
func discoverCDPURL(rawURL string) (string, error) {
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
func waitForCDP(baseURL string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		wsURL, err := discoverCDPURL(baseURL)
		if err == nil {
			return wsURL, nil
		}

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
