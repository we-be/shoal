package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/we-be/shoal/internal/api"
)

// Agent manages its own lifecycle: starts the server, registers with the
// controller, and runs until told to stop.
type Agent struct {
	backend       BrowserBackend
	server        *Server
	listenAddr    string
	controllerURL string
	agentID       string
}

func New(listenAddr, controllerURL string, backend BrowserBackend) *Agent {
	return &Agent{
		backend:       backend,
		server:        NewServer(backend),
		listenAddr:    listenAddr,
		controllerURL: controllerURL,
	}
}

// Run starts the HTTP server and registers with the controller.
func (a *Agent) Run() error {
	// Register in background once server is ready
	go a.registerLoop()

	log.Printf("agent listening on %s", a.listenAddr)
	return http.ListenAndServe(a.listenAddr, a.server)
}

// registerLoop attempts to register with the controller, retrying on failure.
func (a *Agent) registerLoop() {
	// Give the server a moment to start
	time.Sleep(100 * time.Millisecond)

	req := api.RegisterRequest{
		Address: a.listenAddr,
		Backend: a.backend.Health().Backend,
	}

	body, _ := json.Marshal(req)

	for {
		resp, err := http.Post(
			a.controllerURL+"/register",
			"application/json",
			bytes.NewReader(body),
		)
		if err != nil {
			log.Printf("registration failed, retrying in 2s: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		var regResp api.RegisterResponse
		if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
			resp.Body.Close()
			log.Printf("bad registration response, retrying in 2s: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		resp.Body.Close()

		a.agentID = regResp.AgentID
		log.Printf("registered with controller as %s", a.agentID)
		return
	}
}

func (a *Agent) Close() error {
	if err := a.backend.Close(); err != nil {
		return fmt.Errorf("closing backend: %w", err)
	}
	return nil
}
