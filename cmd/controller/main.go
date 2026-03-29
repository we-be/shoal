package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/we-be/shoal/internal/controller"
)

func main() {
	addr := flag.String("addr", ":8180", "address to listen on")
	healthInterval := flag.Duration("health-interval", 15*time.Second, "health check interval")
	leaseTTL := flag.Duration("lease-ttl", 5*time.Minute, "max lease duration before auto-expire")
	maxMissed := flag.Int("max-missed-checks", 3, "remove agent after N consecutive failed health checks")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("shoal controller listening on %s", *addr)

	config := controller.HealthConfig{
		CheckInterval:   *healthInterval,
		LeaseTTL:        *leaseTTL,
		AgentTimeout:    5 * time.Second,
		MaxMissedChecks: *maxMissed,
	}

	srv := controller.NewServerWithConfig(config)
	if err := http.ListenAndServe(*addr, srv); err != nil {
		log.Fatalf("controller died: %v", err)
	}
}
