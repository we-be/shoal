package main

import (
	"flag"
	"log"

	"github.com/we-be/shoal/internal/agent"
)

func main() {
	addr := flag.String("addr", ":8181", "address to listen on")
	controller := flag.String("controller", "http://localhost:8180", "controller URL")
	backendType := flag.String("backend", "stub", "browser backend: stub, cdp, lightpanda")
	cdpURL := flag.String("cdp-url", "", "CDP WebSocket URL (for cdp backend)")
	lpBin := flag.String("lightpanda-bin", "lightpanda", "path to lightpanda binary")
	lpPort := flag.Int("lightpanda-port", 9222, "CDP port for lightpanda subprocess")
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
