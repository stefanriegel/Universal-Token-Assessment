package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// Two-phase device-authorization flow for interactive auth methods (AWS IAM
// Identity Center SSO, Azure Device Code).
//
// The single-request validate flow only works when a browser can be launched on
// the machine running the binary — true for the desktop single-binary, false in
// Docker/headless. These flows produce a verification URL + code mid-way that the
// user must open in their OWN browser, but a synchronous request can't deliver it
// until after auth already completed.
//
// The two-phase flow splits it:
//  1. POST .../device/start — kicks off auth in a background goroutine and returns
//     the code + URL as soon as the SDK produces it. The browser displays it.
//  2. GET  .../device/poll  — the browser polls until the background auth completes,
//     at which point the session is established and subscriptions returned.

// deviceAuthState holds one in-flight device authorization.
type deviceAuthState struct {
	mu          sync.Mutex
	message     string        // verification URL + code for the user
	ready       chan struct{} // closed once message is set OR auth failed early
	readyOnce   sync.Once
	done        bool
	subs        []SubscriptionItem
	creds       map[string]string
	provider    string
	authMethod  string
	forestIndex int
	err         error
	cancel      context.CancelFunc
}

func (s *deviceAuthState) signalReady() {
	s.readyOnce.Do(func() { close(s.ready) })
}

// deviceAuths maps an opaque authId to its in-flight state.
var deviceAuths sync.Map

// deviceAuthRunnerFor resolves the auth function for a provider/authMethod pair.
// It is a var so tests can inject a stub runner without hitting real cloud APIs.
var deviceAuthRunnerFor = deviceAuthRunner

// deviceAuthRunner returns the auth function for a provider/authMethod pair, or
// nil if the pair does not use the two-phase device flow.
func deviceAuthRunner(provider, authMethod string) func(context.Context, map[string]string, func(string)) ([]SubscriptionItem, error) {
	switch {
	case provider == "aws" && authMethod == "sso":
		return realAWSSSO
	case provider == "azure" && (authMethod == "device-code" || authMethod == "device_code"):
		return realAzureDeviceCode
	case provider == "gcp" && (authMethod == "browser-oauth" || authMethod == "device-code"):
		return realGCPDeviceAuth
	}
	return nil
}

func newAuthID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// HandleDeviceStart begins a two-phase device authorization and returns the
// verification message as soon as it is available.
func (h *ValidateHandler) HandleDeviceStart(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")

	var req ValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	runner := deviceAuthRunnerFor(provider, req.AuthMethod)
	if runner == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "provider/auth method does not support device authorization",
		})
		return
	}

	merged := make(map[string]string, len(req.Credentials)+1)
	for k, v := range req.Credentials {
		merged[k] = v
	}
	merged["authMethod"] = req.AuthMethod

	// The auth outlives this request, so it must not use the request context
	// (cancelled when this handler returns). Give it its own timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)

	st := &deviceAuthState{
		ready:       make(chan struct{}),
		creds:       merged,
		provider:    provider,
		authMethod:  req.AuthMethod,
		forestIndex: req.ForestIndex,
		cancel:      cancel,
	}
	authID := newAuthID()
	deviceAuths.Store(authID, st)

	go func() {
		defer cancel()
		subs, err := runner(ctx, merged, func(msg string) {
			st.mu.Lock()
			st.message = msg
			st.mu.Unlock()
			st.signalReady()
		})
		st.mu.Lock()
		st.done = true
		st.subs = subs
		st.err = err
		st.mu.Unlock()
		st.signalReady() // unblock start even if auth failed before emitting a message
	}()

	// Wait briefly for the verification message (or an early failure).
	select {
	case <-st.ready:
	case <-time.After(30 * time.Second):
	}

	st.mu.Lock()
	msg, done, authErr := st.message, st.done, st.err
	st.mu.Unlock()

	if msg == "" && done && authErr != nil {
		deviceAuths.Delete(authID)
		cancel()
		writeJSON(w, http.StatusOK, map[string]any{"error": authErr.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"authId": authID, "message": msg})
}

// HandleDevicePoll reports the status of an in-flight device authorization and,
// on success, establishes the session and returns the discovered subscriptions.
func (h *ValidateHandler) HandleDevicePoll(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	authID := r.URL.Query().Get("authId")

	v, ok := deviceAuths.Load(authID)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"status": "error", "error": "unknown or expired auth session"})
		return
	}
	st := v.(*deviceAuthState)

	st.mu.Lock()
	done, authErr, subs, creds, msg := st.done, st.err, st.subs, st.creds, st.message
	st.mu.Unlock()

	if !done {
		writeJSON(w, http.StatusOK, map[string]any{"status": "pending", "message": msg})
		return
	}

	// Terminal — clean up regardless of outcome.
	deviceAuths.Delete(authID)

	if authErr != nil {
		writeJSON(w, http.StatusOK, ValidateResponse{Valid: false, Error: authErr.Error(), Subscriptions: []SubscriptionItem{}})
		return
	}

	h.establishSession(w, r, provider, st.authMethod, creds, st.forestIndex)
	if subs == nil {
		subs = []SubscriptionItem{}
	}
	writeJSON(w, http.StatusOK, ValidateResponse{Valid: true, Subscriptions: subs})
}
