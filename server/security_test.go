package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stefanriegel/Universal-Token-Assessment/server"
)

// TestResolveListenAddr verifies the default bind address is loopback-only
// (security: desktop default must not listen on all interfaces) and that an
// explicit LISTEN_ADDR value is passed through unchanged (container mode).
func TestResolveListenAddr(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want string
	}{
		{"empty defaults to loopback", "", "127.0.0.1:8080"},
		{"explicit all-interfaces passes through", "0.0.0.0:8080", "0.0.0.0:8080"},
		{"explicit loopback ephemeral passes through", "127.0.0.1:0", "127.0.0.1:0"},
		{"whitespace trimmed then defaults", "  ", "127.0.0.1:8080"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := server.ResolveListenAddr(tc.env)
			if got != tc.want {
				t.Errorf("ResolveListenAddr(%q) = %q, want %q", tc.env, got, tc.want)
			}
		})
	}
}

// TestOriginGuardRejectsCrossOriginMutation verifies a browser request carrying
// a foreign Origin header is blocked on state-changing methods (CSRF defense),
// while same-origin, loopback, and non-browser (no-Origin) requests are allowed
// through to their handlers.
func TestOriginGuardRejectsCrossOriginMutation(t *testing.T) {
	router := testRouterDefaults(fakeFS())
	ts := httptest.NewServer(router)
	defer ts.Close()

	tests := []struct {
		name         string
		origin       string
		wantRejected bool
	}{
		{"foreign origin rejected", "https://evil.example.com", true},
		{"loopback localhost allowed", "http://localhost:8080", false},
		{"loopback 127.0.0.1 allowed", "http://127.0.0.1:8080", false},
		{"no origin (curl/connector) allowed", "", false},
		{"foreign origin on different port rejected", "http://attacker.local:9999", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/scan", nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			defer resp.Body.Close()

			rejected := resp.StatusCode == http.StatusForbidden
			if rejected != tc.wantRejected {
				t.Errorf("origin %q: got status %d (rejected=%v), wantRejected=%v",
					tc.origin, resp.StatusCode, rejected, tc.wantRejected)
			}
		})
	}
}

// TestOriginGuardAllowsCrossOriginReads verifies GET requests are never blocked
// by the origin guard even with a foreign Origin — only state-changing methods
// are guarded.
func TestOriginGuardAllowsCrossOriginReads(t *testing.T) {
	router := testRouterDefaults(fakeFS())
	ts := httptest.NewServer(router)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/health", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		t.Errorf("GET with foreign origin should not be blocked, got 403")
	}
}
