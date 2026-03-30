package controller

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/we-be/shoal/internal/api"
)

// ProxyProvider supplies proxies and tracks their health.
type ProxyProvider interface {
	GetProxy() (*api.ProxyConfig, error)
	RecordSuccess(proxyURL string)
	RecordFailure(proxyURL string)
	Stats() ProxyStats
}

// ProxyStats summarizes proxy pool state.
type ProxyStats struct {
	Total   int `json:"total"`
	Healthy int `json:"healthy"`
}

// --- Static Provider (single proxy, current behavior) ---

type staticProvider struct {
	proxy *api.ProxyConfig
}

func NewStaticProvider(proxy *api.ProxyConfig) ProxyProvider {
	if proxy == nil || proxy.URL == "" {
		return nil
	}
	return &staticProvider{proxy: proxy}
}

func (s *staticProvider) GetProxy() (*api.ProxyConfig, error) {
	return s.proxy, nil
}

func (s *staticProvider) RecordSuccess(string) {}
func (s *staticProvider) RecordFailure(string) {}

func (s *staticProvider) Stats() ProxyStats {
	return ProxyStats{Total: 1, Healthy: 1}
}

// --- Pool Provider (multiple proxies with health tracking) ---

type proxyHealth struct {
	successes int
	failures  int
	lastFail  time.Time
}

// PoolProvider manages a list of proxies with round-robin selection
// and health-based filtering.
type PoolProvider struct {
	mu       sync.Mutex
	proxies  []api.ProxyConfig
	health   map[string]*proxyHealth
	index    int
	source   string        // file path or HTTP URL for refresh
	sourceKind string      // "file" or "http"
	stopCh   chan struct{}
}

func NewPoolProvider(proxies []api.ProxyConfig) *PoolProvider {
	health := make(map[string]*proxyHealth, len(proxies))
	for _, p := range proxies {
		health[p.URL] = &proxyHealth{}
	}
	return &PoolProvider{
		proxies: proxies,
		health:  health,
		stopCh:  make(chan struct{}),
	}
}

// StartRefresh periodically reloads the proxy list from the original source.
func (p *PoolProvider) StartRefresh(source, kind string, interval time.Duration) {
	p.source = source
	p.sourceKind = kind

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-p.stopCh:
				return
			case <-ticker.C:
				p.refresh()
			}
		}
	}()

	log.Printf("proxy pool refresh enabled: %s every %s", source, interval)
}

func (p *PoolProvider) Stop() {
	close(p.stopCh)
}

func (p *PoolProvider) refresh() {
	var proxies []api.ProxyConfig
	var err error

	switch p.sourceKind {
	case "file":
		proxies, err = LoadProxiesFromFile(p.source)
	case "http":
		proxies, err = LoadProxiesFromHTTP(p.source)
	default:
		return
	}

	if err != nil {
		log.Printf("proxy refresh failed: %v", err)
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Merge health data — keep stats for existing proxies, add new ones
	newHealth := make(map[string]*proxyHealth, len(proxies))
	for _, proxy := range proxies {
		if existing, ok := p.health[proxy.URL]; ok {
			newHealth[proxy.URL] = existing
		} else {
			newHealth[proxy.URL] = &proxyHealth{}
		}
	}

	added := len(proxies) - len(p.proxies)
	p.proxies = proxies
	p.health = newHealth

	if added != 0 {
		log.Printf("proxy pool refreshed: %d proxies (%+d)", len(proxies), added)
	}
}

func (p *PoolProvider) GetProxy() (*api.ProxyConfig, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.proxies) == 0 {
		return nil, fmt.Errorf("no proxies available")
	}

	// Try to find a healthy proxy (round-robin)
	for range len(p.proxies) {
		proxy := p.proxies[p.index%len(p.proxies)]
		p.index++

		h := p.health[proxy.URL]
		if h != nil && h.failureRate() > 0.5 && h.successes+h.failures >= 5 {
			continue // skip unhealthy proxies
		}

		return &proxy, nil
	}

	// All proxies unhealthy — return the least-bad one
	p.index++
	proxy := p.proxies[p.index%len(p.proxies)]
	return &proxy, nil
}

func (p *PoolProvider) RecordSuccess(proxyURL string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if h, ok := p.health[proxyURL]; ok {
		h.successes++
	}
}

func (p *PoolProvider) RecordFailure(proxyURL string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if h, ok := p.health[proxyURL]; ok {
		h.failures++
		h.lastFail = time.Now()
	}
}

func (p *PoolProvider) Stats() ProxyStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	healthy := 0
	for _, h := range p.health {
		if h.failureRate() <= 0.5 || h.successes+h.failures < 5 {
			healthy++
		}
	}

	return ProxyStats{
		Total:   len(p.proxies),
		Healthy: healthy,
	}
}

func (h *proxyHealth) failureRate() float64 {
	total := h.successes + h.failures
	if total == 0 {
		return 0
	}
	return float64(h.failures) / float64(total)
}

// --- File Provider (loads from JSON, supports refresh) ---

// LoadProxiesFromFile reads a JSON array of proxy configs.
// Format: [{"url": "http://host:port", "username": "user", "password": "pass"}, ...]
func LoadProxiesFromFile(path string) ([]api.ProxyConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading proxy file: %w", err)
	}

	var proxies []api.ProxyConfig
	if err := json.Unmarshal(data, &proxies); err != nil {
		return nil, fmt.Errorf("parsing proxy file: %w", err)
	}

	log.Printf("loaded %d proxies from %s", len(proxies), path)
	return proxies, nil
}

// --- HTTP Provider (fetches from API endpoint) ---

// LoadProxiesFromHTTP fetches a JSON array of proxy configs from a URL.
func LoadProxiesFromHTTP(url string) ([]api.ProxyConfig, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching proxies: %w", err)
	}
	defer resp.Body.Close()

	var proxies []api.ProxyConfig
	if err := json.NewDecoder(resp.Body).Decode(&proxies); err != nil {
		return nil, fmt.Errorf("parsing proxy response: %w", err)
	}

	log.Printf("loaded %d proxies from %s", len(proxies), url)
	return proxies, nil
}
