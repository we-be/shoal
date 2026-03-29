package shoal

import "testing"

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://www.example.com/path", "example.com"},
		{"https://example.com", "example.com"},
		{"https://example.com:8080/foo", "example.com"},
		{"https://sub.example.com/foo?bar=1", "sub.example.com"},
		{"http://localhost:9090/login", "localhost"},
	}

	for _, tt := range tests {
		got := extractDomain(tt.url)
		if got != tt.expected {
			t.Errorf("extractDomain(%q) = %q, want %q", tt.url, got, tt.expected)
		}
	}
}
