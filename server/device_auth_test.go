package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/session"
)

// TestDeviceAuthTwoPhase drives start → pending → complete with a stubbed runner,
// verifying the code is surfaced before auth completes and subscriptions after.
func TestDeviceAuthTwoPhase(t *testing.T) {
	store := session.NewStore()
	h := NewValidateHandler(store)
	router := newDeviceRouter(h)

	// Gate lets the test hold the runner in the "pending" state until released.
	release := make(chan struct{})
	orig := deviceAuthRunnerFor
	deviceAuthRunnerFor = func(provider, authMethod string) func(context.Context, map[string]string, func(string)) ([]SubscriptionItem, error) {
		return func(ctx context.Context, creds map[string]string, onMessage func(string)) ([]SubscriptionItem, error) {
			onMessage("open https://example.test and enter CODE-1234")
			<-release
			return []SubscriptionItem{{ID: "sub-1", Name: "Sub One"}}, nil
		}
	}
	t.Cleanup(func() { deviceAuthRunnerFor = orig })

	// Phase 1: start returns the code immediately.
	startResp := doJSON(t, router, http.MethodPost, "/api/v1/providers/azure/device/start",
		`{"authMethod":"device-code","credentials":{"tenantId":"t"}}`)
	authID, _ := startResp["authId"].(string)
	if authID == "" {
		t.Fatalf("expected authId, got %v", startResp)
	}
	if msg, _ := startResp["message"].(string); !strings.Contains(msg, "CODE-1234") {
		t.Fatalf("expected code in message, got %q", msg)
	}

	// Poll before release → pending.
	pollResp := doJSON(t, router, http.MethodGet, "/api/v1/providers/azure/device/poll?authId="+authID, "")
	if pollResp["status"] != "pending" {
		t.Fatalf("expected pending, got %v", pollResp)
	}

	// Release the runner and poll until complete.
	close(release)
	var final map[string]any
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		final = doJSON(t, router, http.MethodGet, "/api/v1/providers/azure/device/poll?authId="+authID, "")
		if final["status"] != "pending" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if final["valid"] != true {
		t.Fatalf("expected valid=true on completion, got %v", final)
	}
	subs, _ := final["subscriptions"].([]any)
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %v", final["subscriptions"])
	}
}

// TestDeviceAuthRunnerMapping locks which provider/authMethod pairs use the
// two-phase device flow.
func TestDeviceAuthRunnerMapping(t *testing.T) {
	cases := []struct {
		provider, method string
		want             bool
	}{
		{"aws", "sso", true},
		{"azure", "device-code", true},
		{"azure", "device_code", true},
		{"gcp", "browser-oauth", true},
		{"gcp", "device-code", true},
		{"azure", "browser-sso", false}, // guarded, not device flow
		{"aws", "access_key", false},
		{"gcp", "service-account", false},
	}
	for _, c := range cases {
		got := deviceAuthRunner(c.provider, c.method) != nil
		if got != c.want {
			t.Errorf("deviceAuthRunner(%q,%q)=%v, want %v", c.provider, c.method, got, c.want)
		}
	}
}

func newDeviceRouter(h *ValidateHandler) http.Handler {
	r := chi.NewRouter()
	RegisterValidateHandler(r, h)
	return r
}

func doJSON(t *testing.T, router http.Handler, method, url, body string) map[string]any {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, url, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, url, nil)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s %s: %v (body=%s)", method, url, err, rr.Body.String())
	}
	return out
}
