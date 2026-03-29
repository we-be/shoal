package main

import (
	"flag"
	"log"

	"github.com/we-be/shoal/internal/agent"
)

func main() {
	addr := flag.String("addr", ":8181", "address to listen on")
	controller := flag.String("controller", "http://localhost:8180", "controller URL")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("shoal agent starting (controller=%s)", *controller)

	backend := agent.NewStubBackend()
	a := agent.New(*addr, *controller, backend)

	if err := a.Run(); err != nil {
		log.Fatalf("agent died: %v", err)
	}
}
