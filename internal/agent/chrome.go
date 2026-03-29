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
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/we-be/shoal/internal/api"
)

// Chrome launch flags for anti-detection headless mode.
// NO --headless — we use xvfb or a real display.
var chromeFlags = []string{
	"--no-sandbox",
	"--disable-blink-features=AutomationControlled",
	"--disable-dev-shm-usage",
	"--disable-gpu-sandbox",
	"--no-first-run",
	"--no-default-browser-check",
	"--disable-extensions",
	"--disable-popup-blocking",
	"--disable-background-networking",
	"--disable-sync",
	"--disable-translate",
	"--disable-search-engine-choice-screen",
	"--disable-setuid-sandbox",
	"--metrics-recording-only",
	"--no-zygote",
	"--password-store=basic",
	"--use-mock-keychain",
	"--ignore-certificate-errors",
	"--ignore-ssl-errors",
	"--remote-allow-origins=*",
	"--enable-webgl",
	"--window-size=1920,1080",
	"--start-maximized",
}

// Stealth JS injected via CDP before any page loads.
const stealthJS = `
Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
window.chrome = window.chrome || {};
window.chrome.runtime = window.chrome.runtime || {};
`

// NewChromeBackend launches Chrome and connects via CDP.
// This is the grouper — full browser with real rendering, xvfb-compatible,
// capable of passing Cloudflare Turnstile's fingerprint gauntlet.
func NewChromeBackend(chromeBin string, cdpPort int, proxy *api.ProxyConfig) (*CDPBackend, error) {
	if chromeBin == "" {
		chromeBin = findChrome()
	}
	if chromeBin == "" {
		return nil, fmt.Errorf("chrome not found — install Chrome or pass -chrome-bin")
	}

	// Ensure we have a display (real or xvfb)
	display := os.Getenv("DISPLAY")
	if display == "" {
		xvfb, err := startXvfb()
		if err != nil {
			return nil, fmt.Errorf("no DISPLAY and couldn't start xvfb: %w", err)
		}
		display = xvfb
		log.Printf("started xvfb on %s", display)
	} else {
		log.Printf("using existing display %s", display)
	}

	portStr := strconv.Itoa(cdpPort)
	userDataDir := fmt.Sprintf("/tmp/shoal-chrome-%d", cdpPort)
	os.MkdirAll(userDataDir, 0o755)

	args := append([]string{}, chromeFlags...)
	args = append(args,
		"--remote-debugging-port="+portStr,
		"--remote-debugging-address=127.0.0.1",
		"--user-data-dir="+userDataDir,
	)

	// Add proxy server flag if configured
	if proxy != nil && proxy.URL != "" {
		args = append(args, "--proxy-server="+proxy.URL)
		log.Printf("chrome proxy: %s", proxy.URL)
	}

	args = append(args, "about:blank")

	var cmd *exec.Cmd
	if strings.HasPrefix(chromeBin, "flatpak::") {
		appID := strings.TrimPrefix(chromeBin, "flatpak::")
		flatpakArgs := append([]string{"run", appID}, args...)
		cmd = exec.Command("flatpak", flatpakArgs...)
		log.Printf("launching flatpak chrome: %s", appID)
	} else {
		cmd = exec.Command(chromeBin, args...)
	}

	cmd.Env = append(os.Environ(), "DISPLAY="+display)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting chrome: %w", err)
	}

	log.Printf("chrome started (pid=%d, port=%d, display=%s)", cmd.Process.Pid, cdpPort, display)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", cdpPort)

	wsURL, pageTargetID, err := waitForChromeReady(baseURL, 15*time.Second)
	if err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("chrome didn't come up: %w", err)
	}

	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), wsURL)

	tabCtx, tabCancel := chromedp.NewContext(allocCtx,
		chromedp.WithTargetID(target.ID(pageTargetID)))

	// Inject stealth script + set up proxy auth if needed
	initActions := chromedp.Tasks{
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(stealthJS).Do(ctx)
			return err
		}),
	}

	// Set up CDP Fetch-based proxy auth (Chrome 137+ broke extension-based auth)
	if proxy != nil && proxy.Username != "" {
		initActions = append(initActions, chromedp.ActionFunc(func(ctx context.Context) error {
			return setupCDPProxyAuth(ctx, proxy.Username, proxy.Password)
		}))
		log.Printf("chrome CDP proxy auth enabled for %s", proxy.URL)
	}

	initActions = append(initActions, chromedp.Navigate("about:blank"))

	if err := chromedp.Run(tabCtx, initActions); err != nil {
		tabCancel()
		allocCancel()
		cmd.Process.Kill()
		return nil, fmt.Errorf("chrome stealth init: %w", err)
	}

	log.Printf("chrome backend ready (stealth injected, target %s)", pageTargetID)

	return &CDPBackend{
		allocCtx:    allocCtx,
		allocCancel: allocCancel,
		tabCtx:      tabCtx,
		tabCancel:   tabCancel,
		cmd:         cmd,
		started:     time.Now(),
		name:        api.BackendChrome,
	}, nil
}

// setupCDPProxyAuth enables CDP Fetch to handle proxy authentication.
// This replaces Chrome's broken MV3 proxy extension auth (Chrome 137+).
func setupCDPProxyAuth(ctx context.Context, username, password string) error {
	// Enable Fetch with auth interception
	if err := fetch.Enable().WithHandleAuthRequests(true).Do(ctx); err != nil {
		return fmt.Errorf("fetch.enable: %w", err)
	}

	// Listen for auth challenges and paused requests in background
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *fetch.EventAuthRequired:
			go func() {
				err := fetch.ContinueWithAuth(e.RequestID, &fetch.AuthChallengeResponse{
					Response: fetch.AuthChallengeResponseResponseProvideCredentials,
					Username: username,
					Password: password,
				}).Do(ctx)
				if err != nil {
					log.Printf("proxy auth failed: %v", err)
				}
			}()
		case *fetch.EventRequestPaused:
			go func() {
				err := fetch.ContinueRequest(e.RequestID).Do(ctx)
				if err != nil {
					log.Printf("continue request failed: %v", err)
				}
			}()
		}
	})

	return nil
}

// startXvfb tries to start a virtual framebuffer.
func startXvfb() (string, error) {
	xvfbPath, err := exec.LookPath("Xvfb")
	if err != nil {
		return "", fmt.Errorf("xvfb not found in PATH")
	}

	display := ":99"
	cmd := exec.Command(xvfbPath, display, "-screen", "0", "1920x1080x24", "-ac")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("starting Xvfb: %w", err)
	}

	os.Setenv("DISPLAY", display)
	time.Sleep(500 * time.Millisecond)
	return display, nil
}

// waitForChromeReady polls until Chrome has a page target available.
func waitForChromeReady(baseURL string, timeout time.Duration) (string, string, error) {
	deadline := time.Now().Add(timeout)

	var wsURL string
	for time.Now().Before(deadline) {
		discovered, err := discoverCDPURL(baseURL)
		if err == nil && wsURL == "" {
			wsURL = discovered
			log.Printf("chrome CDP ready: %s", wsURL)
		}

		resp, err := http.Get(baseURL + "/json/list")
		if err == nil {
			var targets []struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				URL  string `json:"url"`
			}
			json.NewDecoder(resp.Body).Decode(&targets)
			resp.Body.Close()

			for _, t := range targets {
				if t.Type == "page" {
					if wsURL == "" {
						continue
					}
					return wsURL, t.ID, nil
				}
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return "", "", fmt.Errorf("timeout waiting for chrome page target")
}

// findChrome searches for Chrome/Chromium.
func findChrome() string {
	for _, c := range []string{
		"google-chrome-stable",
		"google-chrome",
		"chromium-browser",
		"chromium",
	} {
		if path, err := exec.LookPath(c); err == nil {
			return path
		}
	}

	if _, err := exec.LookPath("flatpak"); err == nil {
		out, _ := exec.Command("flatpak", "list", "--app", "--columns=application").Output()
		apps := string(out)
		for _, appID := range []string{"com.google.Chrome", "org.chromium.Chromium"} {
			if strings.Contains(apps, appID) {
				return "flatpak::" + appID
			}
		}
	}

	return ""
}

// Suppress unused import for network package (used by CDPBackend in cdp.go)
var _ = network.GetCookies
