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

	"github.com/we-be/shoal/internal/api"
	"github.com/we-be/shoal/internal/controller"
)

func main() {
	// Flags with env var fallbacks for container deployments
	addr := flag.String("addr", api.EnvOr("SHOAL_ADDR", ":8180"), "address to listen on (SHOAL_ADDR)")
	storePath := flag.String("store", api.EnvOr("SHOAL_STORE", "shoal-pool.json"), "pool state snapshot path (SHOAL_STORE)")
	healthInterval := flag.Duration("health-interval", api.EnvDuration("SHOAL_HEALTH_INTERVAL", 15*time.Second), "health check interval (SHOAL_HEALTH_INTERVAL)")
	leaseTTL := flag.Duration("lease-ttl", api.EnvDuration("SHOAL_LEASE_TTL", 5*time.Minute), "max lease idle duration (SHOAL_LEASE_TTL)")
	maxMissed := flag.Int("max-missed-checks", api.EnvInt("SHOAL_MAX_MISSED_CHECKS", 3), "failed checks before removal (SHOAL_MAX_MISSED_CHECKS)")
	proxyFile := flag.String("proxy-file", api.EnvOr("SHOAL_PROXY_FILE", ""), "proxy list JSON file (SHOAL_PROXY_FILE)")
	proxyAPI := flag.String("proxy-api", api.EnvOr("SHOAL_PROXY_API", ""), "proxy list HTTP endpoint (SHOAL_PROXY_API)")
	proxyRefresh := flag.Duration("proxy-refresh", api.EnvDuration("SHOAL_PROXY_REFRESH", 5*time.Minute), "proxy list refresh interval (SHOAL_PROXY_REFRESH)")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("shoal controller %s (%s) listening on %s", api.Version, api.ControllerCodename, *addr)

	config := controller.HealthConfig{
		CheckInterval:   *healthInterval,
		LeaseTTL:        *leaseTTL,
		AgentTimeout:    5 * time.Second,
		MaxMissedChecks: *maxMissed,
	}

	srv := controller.NewServerWithConfig(config, *storePath, *addr)

	if *proxyFile != "" {
		proxies, err := controller.LoadProxiesFromFile(*proxyFile)
		if err != nil {
			log.Fatalf("loading proxy file: %v", err)
		}
		pool := controller.NewPoolProvider(proxies)
		pool.StartRefresh(*proxyFile, "file", *proxyRefresh)
		srv.SetProxyProvider(pool)
	} else if *proxyAPI != "" {
		proxies, err := controller.LoadProxiesFromHTTP(*proxyAPI)
		if err != nil {
			log.Fatalf("loading proxies from API: %v", err)
		}
		pool := controller.NewPoolProvider(proxies)
		pool.StartRefresh(*proxyAPI, "http", *proxyRefresh)
		srv.SetProxyProvider(pool)
	}

	httpServer := &http.Server{Addr: *addr, Handler: srv}

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
