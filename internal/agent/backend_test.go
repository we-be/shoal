package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/we-be/shoal/internal/api"
)

func TestStubBackendNavigate(t *testing.T) {
	// Start a test HTTP server
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

func TestAgentServer(t *testing.T) {
	backend := NewStubBackend()
	srv := NewServer(backend)

	// Test health endpoint
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("health returned %d", w.Code)
	}
}
