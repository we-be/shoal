package main

import (
	"flag"
	"log"

	"github.com/we-be/shoal/internal/agent"
)

func main() {
	addr := flag.String("addr", ":8181", "address to listen on")
	controller := flag.String("controller", "http://localhost:8180", "controller URL")
	backendType := flag.String("backend", "stub", "browser backend: stub, cdp, lightpanda, chrome, tls-client")
	cdpURL := flag.String("cdp-url", "", "CDP WebSocket URL (for cdp backend)")
	lpBin := flag.String("lightpanda-bin", "lightpanda", "path to lightpanda binary")
	lpPort := flag.Int("lightpanda-port", 9222, "CDP port for lightpanda/chrome subprocess")
	chromeBin := flag.String("chrome-bin", "", "path to chrome binary (auto-detected if empty)")
	userAgent := flag.String("user-agent", "", "User-Agent string (for tls-client backend)")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	var backend agent.BrowserBackend
	var err error

	switch *backendType {
	case "stub":
		log.Printf("using stub backend (plain HTTP fetch)")
		backend = agent.NewStubBackend()
	case "cdp":
		if *cdpURL == "" {
			log.Fatal("cdp backend requires -cdp-url flag")
		}
		log.Printf("using CDP backend (url=%s)", *cdpURL)
		backend, err = agent.NewCDPBackend(*cdpURL)
	case "lightpanda":
		log.Printf("using lightpanda backend (bin=%s, port=%d)", *lpBin, *lpPort)
		backend, err = agent.NewLightpandaBackend(*lpBin, *lpPort)
	case "chrome":
		log.Printf("using chrome backend (grouper, port=%d)", *lpPort)
		backend, err = agent.NewChromeBackend(*chromeBin, *lpPort)
	case "tls-client":
		log.Printf("using tls-client backend (minnow)")
		backend, err = agent.NewTLSClientBackend(*userAgent)
	default:
		log.Fatalf("unknown backend: %s", *backendType)
	}
	if err != nil {
		log.Fatalf("failed to create %s backend: %v", *backendType, err)
	}

	log.Printf("shoal agent starting (controller=%s)", *controller)
	a := agent.New(*addr, *controller, backend)

	if err := a.Run(); err != nil {
		backend.Close()
		log.Fatalf("agent died: %v", err)
	}
}
