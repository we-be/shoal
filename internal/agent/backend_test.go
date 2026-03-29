package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/we-be/shoal/internal/api"
)

func TestStubBackendNavigate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body>hello</body></html>"))
	}))
	defer ts.Close()

	backend := NewStubBackend()
	defer backend.Close()

	resp, err := backend.Navigate(context.Background(), api.NavigateRequest{URL: ts.URL})
	if err != nil {
		t.Fatalf("navigate failed: %v", err)
	}
	if resp.Status != 200 {
		t.Fatalf("expected 200, got %d", resp.Status)
	}
	if resp.HTML != "<html><body>hello</body></html>" {
		t.Fatalf("unexpected HTML: %s", resp.HTML)
	}
}

func TestStubBackendTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Context should cancel before we respond
		<-r.Context().Done()
	}))
	defer ts.Close()

	backend := NewStubBackend()
	defer backend.Close()

	_, err := backend.Navigate(context.Background(), api.NavigateRequest{
		URL:        ts.URL,
		MaxTimeout: 100, // 100ms
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestStubBackendHealth(t *testing.T) {
	backend := NewStubBackend()
	health := backend.Health()

	if health.Status != api.HealthOK {
		t.Fatalf("expected ok, got %s", health.Status)
	}
	if health.Backend != api.BackendStub {
		t.Fatalf("expected stub, got %s", health.Backend)
	}
}

func TestTLSClientBackendHealth(t *testing.T) {
	backend, err := NewTLSClientBackend("", nil)
	if err != nil {
		t.Fatalf("failed to create tls client: %v", err)
	}
	defer backend.Close()

	health := backend.Health()
	if health.Status != api.HealthOK {
		t.Fatalf("expected ok, got %s", health.Status)
	}
	if health.Backend != api.BackendTLSClient {
		t.Fatalf("expected tls-client, got %s", health.Backend)
	}
}

func TestTLSClientSetCookies(t *testing.T) {
	backend, err := NewTLSClientBackend("", nil)
	if err != nil {
		t.Fatalf("failed to create tls client: %v", err)
	}
	defer backend.Close()

	err = backend.SetCookies("https://example.com", []api.Cookie{
		{Name: "session", Value: "abc123", Domain: "example.com", Path: "/"},
	})
	if err != nil {
		t.Fatalf("set cookies failed: %v", err)
	}
}

// --- Agent Server HTTP Tests ---

func TestAgentServerHealth(t *testing.T) {
	srv := NewServer(NewStubBackend())

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("health returned %d", w.Code)
	}

	var health api.HealthStatus
	json.NewDecoder(w.Body).Decode(&health)
	if health.Status != api.HealthOK {
		t.Fatalf("expected ok, got %s", health.Status)
	}
}

func TestAgentServerNavigate(t *testing.T) {
	// Target site
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>test page</html>"))
	}))
	defer ts.Close()

	srv := NewServer(NewStubBackend())

	body, _ := json.Marshal(api.NavigateRequest{URL: ts.URL})
	req := httptest.NewRequest("POST", "/navigate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("navigate returned %d: %s", w.Code, w.Body.String())
	}

	var resp api.NavigateResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.HTML != "<html>test page</html>" {
		t.Fatalf("unexpected HTML: %s", resp.HTML)
	}
}

func TestAgentServerNavigateBadRequest(t *testing.T) {
	srv := NewServer(NewStubBackend())

	// Missing URL
	body, _ := json.Marshal(api.NavigateRequest{URL: ""})
	req := httptest.NewRequest("POST", "/navigate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400 for missing URL, got %d", w.Code)
	}
}

func TestAgentServerSetCookies(t *testing.T) {
	backend, _ := NewTLSClientBackend("", nil)
	srv := NewServer(backend)

	body, _ := json.Marshal(api.SetCookiesRequest{
		URL: "https://example.com",
		Cookies: []api.Cookie{
			{Name: "token", Value: "xyz", Domain: "example.com"},
		},
	})
	req := httptest.NewRequest("POST", "/cookies/set", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("set cookies returned %d: %s", w.Code, w.Body.String())
	}
}

func TestAgentServerSetCookiesUnsupported(t *testing.T) {
	// Stub backend doesn't implement CookieSetter
	srv := NewServer(NewStubBackend())

	body, _ := json.Marshal(api.SetCookiesRequest{
		URL:     "https://example.com",
		Cookies: []api.Cookie{{Name: "a", Value: "b"}},
	})
	req := httptest.NewRequest("POST", "/cookies/set", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 501 {
		t.Fatalf("expected 501 for unsupported backend, got %d", w.Code)
	}
}
