package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/we-be/shoal/internal/controller"
)

func main() {
	addr := flag.String("addr", ":8180", "address to listen on")
	storePath := flag.String("store", "shoal-pool.json", "path to pool state snapshot file")
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

	srv := controller.NewServerWithConfig(config, *storePath, *addr)

	httpServer := &http.Server{Addr: *addr, Handler: srv}

	// Graceful shutdown — save pool state on SIGINT/SIGTERM
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("received %s, shutting down...", sig)

		srv.Shutdown()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpServer.Shutdown(ctx)
	}()

	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("controller died: %v", err)
	}
	log.Printf("controller stopped")
}
