package api

// Set via ldflags at build time:
//
//	go build -ldflags "-X github.com/we-be/shoal/internal/api.Version=0.3.0"
var Version = "dev"

// Component codenames — agents are fish, controllers are sealife.
const (
	AgentCodename      = "mullet"     // fast, everywhere, travels in schools
	ControllerCodename = "xiphosura"  // horseshoe crab — ancient, armored, orchestrates
)
