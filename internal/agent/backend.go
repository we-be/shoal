package agent

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/we-be/shoal/internal/api"
)

// BrowserBackend is the interface any browser implementation must satisfy.
// Swap this out to change what's under the hood — Lightpanda, Chrome, Camoufox, etc.
type BrowserBackend interface {
	Navigate(ctx context.Context, req api.NavigateRequest) (*api.NavigateResponse, error)
	Health() api.HealthStatus
	Close() error
}

// StubBackend is a simple HTTP-fetch backend for testing the orchestration layer.
// No real browser — just proves the plumbing works.
type StubBackend struct {
	client  *http.Client
	started time.Time
}

func NewStubBackend() *StubBackend {
	return &StubBackend{
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
		started: time.Now(),
	}
}

func (s *StubBackend) Navigate(ctx context.Context, req api.NavigateRequest) (*api.NavigateResponse, error) {
	timeout := 30 * time.Second
	if req.MaxTimeout > 0 {
		timeout = time.Duration(req.MaxTimeout) * time.Millisecond
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	s.client.Timeout = timeout
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", req.URL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	return &api.NavigateResponse{
		URL:    resp.Request.URL.String(),
		Status: resp.StatusCode,
		HTML:   string(body),
	}, nil
}

func (s *StubBackend) Health() api.HealthStatus {
	return api.HealthStatus{
		Status:  "ok",
		Backend: "stub",
		Uptime:  int64(time.Since(s.started).Seconds()),
	}
}

func (s *StubBackend) Close() error {
	return nil
}
