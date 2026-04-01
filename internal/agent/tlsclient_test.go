package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/we-be/shoal/internal/api"
)

func TestTLSClientNavigate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><head><title>Test</title></head><body>hello</body></html>"))
	}))
	defer ts.Close()

	backend, err := NewTLSClientBackend("", nil)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	defer backend.Close()

	resp, err := backend.Navigate(context.Background(), api.NavigateRequest{URL: ts.URL})
	if err != nil {
		t.Fatalf("navigate failed: %v", err)
	}

	if resp.Status != 200 {
		t.Fatalf("expected 200, got %d", resp.Status)
	}
	if resp.ContentSize == 0 {
		t.Fatal("expected non-zero content size")
	}
	if resp.Title != "Test" {
		t.Fatalf("expected title 'Test', got '%s'", resp.Title)
	}
}

func TestTLSClientNavigateRedirect(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html><head><title>Final</title></head><body>landed</body></html>"))
	}))
	defer final.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	defer redirect.Close()

	backend, _ := NewTLSClientBackend("", nil)
	defer backend.Close()

	resp, err := backend.Navigate(context.Background(), api.NavigateRequest{URL: redirect.URL})
	if err != nil {
		t.Fatalf("navigate failed: %v", err)
	}

	if !resp.Redirected {
		t.Fatal("expected redirected=true")
	}
	if resp.URL != final.URL+"/" && resp.URL != final.URL {
		t.Fatalf("expected final URL %s, got %s", final.URL, resp.URL)
	}
}

func TestTLSClientNavigate404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte("not found"))
	}))
	defer ts.Close()

	backend, _ := NewTLSClientBackend("", nil)
	defer backend.Close()

	resp, err := backend.Navigate(context.Background(), api.NavigateRequest{URL: ts.URL})
	if err != nil {
		t.Fatalf("navigate failed: %v", err)
	}
	if resp.Status != 404 {
		t.Fatalf("expected 404, got %d", resp.Status)
	}
}

func TestTLSClientSetProxy(t *testing.T) {
	backend, _ := NewTLSClientBackend("", nil)
	defer backend.Close()

	err := backend.SetProxy(&api.ProxyConfig{
		URL:      "http://127.0.0.1:9999",
		Username: "user",
		Password: "pass",
	})
	if err != nil {
		t.Fatalf("set proxy failed: %v", err)
	}
}

func TestInsertProxyAuth(t *testing.T) {
	tests := []struct {
		url, user, pass, expected string
	}{
		{"http://host:8080", "user", "pass", "http://user:pass@host:8080"},
		{"https://host:443", "u", "p", "https://u:p@host:443"},
		{"socks5://host:1080", "a", "b", "socks5://a:b@host:1080"},
	}

	for _, tt := range tests {
		got := insertProxyAuth(tt.url, tt.user, tt.pass)
		if got != tt.expected {
			t.Errorf("insertProxyAuth(%q, %q, %q) = %q, want %q", tt.url, tt.user, tt.pass, got, tt.expected)
		}
	}
}
