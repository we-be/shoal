package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/we-be/shoal/internal/api"
)

// Chrome launch flags for anti-detection headless mode.
var chromeFlags = []string{
	"--headless=new",
	"--no-sandbox",
	"--disable-blink-features=AutomationControlled",
	"--disable-dev-shm-usage",
	"--disable-gpu",
	"--no-first-run",
	"--no-default-browser-check",
	"--disable-extensions",
	"--disable-popup-blocking",
	"--disable-background-networking",
	"--disable-sync",
	"--disable-translate",
	"--metrics-recording-only",
	"--no-zygote",
	"--password-store=basic",
	"--use-mock-keychain",
	"--window-size=1920,1080",
}

// NewChromeBackend launches Chrome with remote debugging and connects via CDP.
// This is the grouper — heavy, full browser, capable of solving CF Turnstile.
func NewChromeBackend(chromeBin string, cdpPort int) (*CDPBackend, error) {
	if chromeBin == "" {
		chromeBin = findChrome()
	}
	if chromeBin == "" {
		return nil, fmt.Errorf("chrome not found — install Chrome or pass -chrome-bin")
	}

	portStr := strconv.Itoa(cdpPort)
	args := append(chromeFlags,
		"--remote-debugging-port="+portStr,
		"--remote-debugging-address=127.0.0.1",
		"about:blank",
	)

	var cmd *exec.Cmd
	if strings.HasPrefix(chromeBin, "flatpak::") {
		appID := strings.TrimPrefix(chromeBin, "flatpak::")
		flatpakArgs := append([]string{"run", appID}, args...)
		cmd = exec.Command("flatpak", flatpakArgs...)
		log.Printf("launching flatpak chrome: %s", appID)
	} else {
		cmd = exec.Command(chromeBin, args...)
	}

	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting chrome: %w", err)
	}

	log.Printf("chrome started (pid=%d, port=%d)", cmd.Process.Pid, cdpPort)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", cdpPort)

	// Wait for Chrome to be fully ready with a page target
	wsURL, pageTargetID, err := waitForChromeReady(baseURL, 15*time.Second)
	if err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("chrome didn't come up: %w", err)
	}

	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), wsURL)

	// Attach to Chrome's existing page target instead of creating a new one.
	// Flatpak Chrome (and some headless configs) can't create new targets.
	tabCtx, tabCancel := chromedp.NewContext(allocCtx,
		chromedp.WithTargetID(target.ID(pageTargetID)))

	if err := chromedp.Run(tabCtx, chromedp.Navigate("about:blank")); err != nil {
		tabCancel()
		allocCancel()
		cmd.Process.Kill()
		return nil, fmt.Errorf("attaching to chrome tab: %w", err)
	}

	log.Printf("chrome backend ready (attached to target %s)", pageTargetID)

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

// waitForChromeReady polls until Chrome has a page target available.
// Returns the browser WebSocket URL and the page target ID.
func waitForChromeReady(baseURL string, timeout time.Duration) (string, string, error) {
	deadline := time.Now().Add(timeout)

	var wsURL string
	for time.Now().Before(deadline) {
		// Get browser WebSocket URL
		discovered, err := discoverCDPURL(baseURL)
		if err == nil && wsURL == "" {
			wsURL = discovered
			log.Printf("chrome CDP ready: %s", wsURL)
		}

		// Look for a page target
		resp, err := http.Get(baseURL + "/json/list")
		if err == nil {
			var targets []struct {
				Type     string `json:"type"`
				ID       string `json:"id"`
				URL      string `json:"url"`
			}
			json.NewDecoder(resp.Body).Decode(&targets)
			resp.Body.Close()

			for _, t := range targets {
				if t.Type == "page" {
					if wsURL == "" {
						return "", "", fmt.Errorf("page target found but no browser WS URL")
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
