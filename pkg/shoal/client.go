// Package shoal provides a Go client for the Shoal controller API.
//
// Usage:
//
//	client := shoal.New("http://localhost:8180")
//
//	// Acquire a lease
//	lease, err := client.Lease(ctx, "my-scraper", "example.com", "")
//
//	// Navigate
//	resp, err := client.Navigate(ctx, lease.LeaseID, "https://example.com", nil)
//	fmt.Println(resp.HTML)
//
//	// Release
//	client.Release(ctx, lease.LeaseID)
//
//	// Or use the high-level helper:
//	resp, err := client.Fetch(ctx, "https://example.com", "my-scraper")
package shoal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/we-be/shoal/internal/api"
)

// Client talks to a Shoal controller.
type Client struct {
	baseURL string
	http    *http.Client
}

// New creates a client pointing at the given controller URL.
func New(controllerURL string) *Client {
	return &Client{
		baseURL: controllerURL,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

// LeaseResponse is returned by Lease.
type LeaseResponse = api.LeaseResponse

// NavigateResponse is returned by Navigate.
type NavigateResponse = api.NavigateResponse

// Action is a browser action (fill, click, submit, wait, eval).
type Action = api.Action

// Cookie is a browser cookie.
type Cookie = api.Cookie

// XHRResponse is a captured XHR/fetch response.
type XHRResponse = api.XHRResponse

// PoolStatus is the pool's current state.
type PoolStatus = api.PoolStatus

// BrowserIdentity is an agent's identity.
type BrowserIdentity = api.BrowserIdentity

// HealthStatus is an agent's health.
type HealthStatus = api.HealthStatus

// --- Lease API ---

// Lease acquires an agent for the given domain. Class can be "heavy", "light", or "" (auto).
func (c *Client) Lease(ctx context.Context, consumer, domain, class string) (*LeaseResponse, error) {
	req := api.LeaseRequest{
		Consumer: consumer,
		Domain:   domain,
		Class:    class,
	}
	var resp LeaseResponse
	if err := c.post(ctx, "/lease", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Navigate sends a request through a leased agent. URL and actions are optional.
// Pass url="" to execute actions on the current page without navigating (stateful flows).
func (c *Client) Navigate(ctx context.Context, leaseID, url string, actions []Action) (*NavigateResponse, error) {
	req := api.RequestPayload{
		LeaseID: leaseID,
		URL:     url,
		Actions: actions,
	}
	var resp NavigateResponse
	if err := c.post(ctx, "/request", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// NavigateWithTimeout is like Navigate but with a custom timeout in milliseconds.
func (c *Client) NavigateWithTimeout(ctx context.Context, leaseID, url string, actions []Action, timeoutMS int) (*NavigateResponse, error) {
	req := api.RequestPayload{
		LeaseID:    leaseID,
		URL:        url,
		Actions:    actions,
		MaxTimeout: timeoutMS,
	}
	var resp NavigateResponse
	if err := c.post(ctx, "/request", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Release returns an agent to the pool.
func (c *Client) Release(ctx context.Context, leaseID string) error {
	req := api.ReleaseRequest{LeaseID: leaseID}
	var resp api.ReleaseResponse
	return c.post(ctx, "/release", req, &resp)
}

// Renew forces a CF clearance renewal for a domain.
func (c *Client) Renew(ctx context.Context, domain string) error {
	req := struct {
		Domain string `json:"domain"`
	}{Domain: domain}
	var resp map[string]string
	return c.post(ctx, "/renew", req, &resp)
}

// --- Status ---

// Status returns pool counts.
func (c *Client) Status(ctx context.Context) (*PoolStatus, error) {
	var resp PoolStatus
	if err := c.get(ctx, "/pool/status", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Agents returns all agent identities.
func (c *Client) Agents(ctx context.Context) ([]BrowserIdentity, error) {
	var resp []BrowserIdentity
	if err := c.get(ctx, "/pool/agents", &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Health returns controller health.
func (c *Client) Health(ctx context.Context) (*HealthStatus, error) {
	var resp HealthStatus
	if err := c.get(ctx, "/health", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// --- High-level helpers ---

// Fetch is a one-shot helper: lease → navigate → release.
// Returns the navigation response. Uses auto class selection.
func (c *Client) Fetch(ctx context.Context, url, consumer string) (*NavigateResponse, error) {
	return c.FetchWithClass(ctx, url, consumer, "")
}

// FetchWithClass is like Fetch but lets you specify agent class.
func (c *Client) FetchWithClass(ctx context.Context, url, consumer, class string) (*NavigateResponse, error) {
	domain := extractDomain(url)

	lease, err := c.Lease(ctx, consumer, domain, class)
	if err != nil {
		return nil, fmt.Errorf("lease: %w", err)
	}
	defer c.Release(ctx, lease.LeaseID)

	resp, err := c.Navigate(ctx, lease.LeaseID, url, nil)
	if err != nil {
		return nil, fmt.Errorf("navigate: %w", err)
	}

	return resp, nil
}

// --- Internal ---

func (c *Client) post(ctx context.Context, path string, body, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp api.ErrorResponse
		json.Unmarshal(respBody, &errResp)
		return fmt.Errorf("%s: %s", errResp.Error, errResp.Detail)
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("decode: %w", err)
		}
	}
	return nil
}

func (c *Client) get(ctx context.Context, path string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return json.NewDecoder(resp.Body).Decode(result)
}

func extractDomain(rawURL string) string {
	// Simple extraction — strip scheme and path
	u := rawURL
	for _, prefix := range []string{"https://", "http://"} {
		if len(u) > len(prefix) && u[:len(prefix)] == prefix {
			u = u[len(prefix):]
			break
		}
	}
	for i, c := range u {
		if c == '/' || c == '?' || c == ':' {
			u = u[:i]
			break
		}
	}
	// Strip www.
	if len(u) > 4 && u[:4] == "www." {
		u = u[4:]
	}
	return u
}
