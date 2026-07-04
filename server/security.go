package server

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// DefaultListenAddr is the loopback-only bind address used when LISTEN_ADDR is
// unset. Desktop mode must not listen on all interfaces: that would let any
// host on the LAN drive the local-control API. Container/all-interface mode is
// opt-in by setting LISTEN_ADDR explicitly (see Dockerfile).
const DefaultListenAddr = "127.0.0.1:8080"

// ResolveListenAddr returns the address the HTTP server should bind to, given
// the raw LISTEN_ADDR environment value. An empty/whitespace value resolves to
// the loopback-only default; any explicit value is passed through unchanged.
func ResolveListenAddr(env string) string {
	if strings.TrimSpace(env) == "" {
		return DefaultListenAddr
	}
	return env
}

// loopbackHosts are origin hosts always treated as same-machine and therefore
// permitted to drive state-changing local-control endpoints.
var loopbackHosts = map[string]bool{
	"localhost": true,
	"127.0.0.1": true,
	"::1":       true,
}

// isAllowedOriginHost reports whether the host component of a browser Origin
// header is permitted. Loopback names are always allowed; additionally any
// host in extra (e.g. a configured non-wildcard LISTEN_ADDR host) is allowed so
// that explicit container/all-interface deployments still work.
func isAllowedOriginHost(host string, extra map[string]bool) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if loopbackHosts[host] || extra[host] {
		return true
	}
	// Treat any loopback IP (e.g. 127.0.0.2) as same-machine.
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	return false
}

// originHost extracts the lowercased host (without port) from an Origin header
// value. Returns "" when the value is empty or unparseable.
func originHost(origin string) string {
	if origin == "" {
		return ""
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// allowedOriginHosts builds the set of extra (non-loopback) origin hosts that
// should be accepted, derived from the configured LISTEN_ADDR. A wildcard bind
// (":8080" / "0.0.0.0" / "::") contributes no extra host — loopback origins
// remain the only browser-accepted source even in container mode.
func allowedOriginHosts() map[string]bool {
	extra := map[string]bool{}
	addr := strings.TrimSpace(os.Getenv("LISTEN_ADDR"))
	if addr == "" {
		return extra
	}
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSpace(host))
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		// wildcard — no specific host to trust
	default:
		extra[host] = true
	}
	return extra
}

// isMutatingMethod reports whether an HTTP method can change server state and
// therefore needs cross-origin protection.
func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// originGuard is middleware that blocks cross-origin browser requests to
// state-changing endpoints (CSRF / local-control hardening, issue #56).
//
// Rules:
//   - Non-mutating methods (GET/HEAD/OPTIONS) pass through untouched.
//   - Requests with no Origin header pass through: these are non-browser
//     clients (curl, PowerShell connectors, the AD discovery agent) that
//     browsers cannot forge a CSRF request through.
//   - Requests with an Origin header must have a loopback (or configured)
//     host; otherwise they are rejected with 403.
func originGuard(extraHosts map[string]bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isMutatingMethod(r.Method) {
				if origin := r.Header.Get("Origin"); origin != "" {
					if !isAllowedOriginHost(originHost(origin), extraHosts) {
						http.Error(w, "cross-origin request forbidden", http.StatusForbidden)
						return
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
