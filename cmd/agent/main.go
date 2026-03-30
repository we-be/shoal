package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/we-be/shoal/internal/agent"
	"github.com/we-be/shoal/internal/api"
)

func main() {
	// Flags with env var fallbacks for container deployments
	addr := flag.String("addr", api.EnvOr("SHOAL_ADDR", ":8181"), "address to listen on (SHOAL_ADDR)")
	controller := flag.String("controller", api.EnvOr("SHOAL_CONTROLLER", "http://localhost:8180"), "controller URL (SHOAL_CONTROLLER)")
	backendType := flag.String("backend", api.EnvOr("SHOAL_BACKEND", "stub"), "backend: stub, cdp, lightpanda, chrome, tls-client (SHOAL_BACKEND)")
	cdpURL := flag.String("cdp-url", api.EnvOr("SHOAL_CDP_URL", ""), "CDP WebSocket URL (SHOAL_CDP_URL)")
	lpBin := flag.String("lightpanda-bin", api.EnvOr("SHOAL_LIGHTPANDA_BIN", "lightpanda"), "lightpanda binary path (SHOAL_LIGHTPANDA_BIN)")
	lpPort := flag.Int("lightpanda-port", api.EnvInt("SHOAL_CDP_PORT", 9222), "CDP port (SHOAL_CDP_PORT)")
	chromeBin := flag.String("chrome-bin", api.EnvOr("SHOAL_CHROME_BIN", ""), "chrome binary path (SHOAL_CHROME_BIN)")
	userAgent := flag.String("user-agent", api.EnvOr("SHOAL_USER_AGENT", ""), "User-Agent string (SHOAL_USER_AGENT)")
	proxyURL := flag.String("proxy-url", api.EnvOr("SHOAL_PROXY_URL", ""), "proxy URL (SHOAL_PROXY_URL)")
	proxyUser := flag.String("proxy-user", api.EnvOr("SHOAL_PROXY_USER", ""), "proxy username (SHOAL_PROXY_USER)")
	proxyPass := flag.String("proxy-pass", api.EnvOr("SHOAL_PROXY_PASS", ""), "proxy password (SHOAL_PROXY_PASS)")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	var proxy *api.ProxyConfig
	if *proxyURL != "" {
		proxy = &api.ProxyConfig{
			URL:      *proxyURL,
			Username: *proxyUser,
			Password: *proxyPass,
		}
	}

	var backend agent.BrowserBackend
	var err error

	switch *backendType {
	case "stub":
		log.Printf("using stub backend (plain HTTP fetch)")
		backend = agent.NewStubBackend()
	case "cdp":
		if *cdpURL == "" {
			log.Fatal("cdp backend requires -cdp-url or SHOAL_CDP_URL")
		}
		log.Printf("using CDP backend (url=%s)", *cdpURL)
		backend, err = agent.NewCDPBackend(*cdpURL)
	case "lightpanda":
		log.Printf("using lightpanda backend (bin=%s, port=%d)", *lpBin, *lpPort)
		backend, err = agent.NewLightpandaBackend(*lpBin, *lpPort)
	case "chrome":
		log.Printf("using chrome backend (grouper, port=%d)", *lpPort)
		backend, err = agent.NewChromeBackend(*chromeBin, *lpPort, proxy)
	case "tls-client":
		log.Printf("using tls-client backend (minnow)")
		backend, err = agent.NewTLSClientBackend(*userAgent, proxy)
	default:
		log.Fatalf("unknown backend: %s", *backendType)
	}
	if err != nil {
		log.Fatalf("failed to create %s backend: %v", *backendType, err)
	}

	log.Printf("shoal agent %s (%s) starting (controller=%s)", api.Version, api.AgentCodename, *controller)
	a := agent.New(*addr, *controller, backend)

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("received %s, shutting down agent...", sig)
		a.Close()
		os.Exit(0)
	}()

	if err := a.Run(); err != nil {
		backend.Close()
		log.Fatalf("agent died: %v", err)
	}
}
