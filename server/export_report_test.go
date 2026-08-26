package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/calculator"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/exporter"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/orchestrator"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/session"
)

func postReport(body string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Post("/api/v1/export", HandleExportReport)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/export", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestHandleExportReport_OK(t *testing.T) {
	in := exporter.ReportInput{
		SelectedProviders: []string{"aws"},
		Findings: []calculator.FindingRow{{
			Provider: "aws", Source: "acct-1", Category: calculator.CategoryDDIObjects,
			Item: "records", Count: 100, TokensPerUnit: 25, ManagementTokens: 4,
		}},
		Totals:         exporter.TokenTotals{DDITokens: 4, GrandTotal: 4},
		ProviderTotals: map[string]int{"aws": 4},
	}
	b, _ := json.Marshal(in)
	rec := postReport(string(b))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "spreadsheetml.sheet") {
		t.Errorf("Content-Type = %q, want an xlsx media type", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, ".xlsx") {
		t.Errorf("Content-Disposition = %q, want an .xlsx filename", cd)
	}
	// A real xlsx is a zip: it starts with "PK".
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte("PK")) {
		t.Error("body is not a zip archive — legacy HTML export leaked through")
	}
}

func TestHandleExportReport_MalformedBody(t *testing.T) {
	if rec := postReport("{not json"); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleExportReport_OversizeBody(t *testing.T) {
	huge := `{"findings":[` + strings.Repeat(`{"item":"`+strings.Repeat("x", 1024)+`"},`, 40000)
	huge = strings.TrimSuffix(huge, ",") + `]}`
	if rec := postReport(huge); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}

// The scan-scoped export is gone: one export path means one set of numbers.
//
// A status-code probe against the removed route can't tell "route absent"
// from "route present but the session store returned 404 for an unknown
// scan ID" — both look like 404. Walking the router's actual route table
// is unambiguous either way. The same walk also asserts that the unified
// POST /api/v1/export route IS registered on the real, application-built
// router — a typo in the route string at server.go would 404 for every
// user while every other test (which mounts its own router) stays green.
func TestScanScopedExportRouteIsRemoved(t *testing.T) {
	r := NewRouter(http.NotFoundHandler(), session.NewStore(), orchestrator.New(nil))

	var found bool
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if route == "/api/v1/scan/{scanId}/export" {
			t.Errorf("route %s %s still registered", method, route)
		}
		if method == http.MethodPost && route == "/api/v1/export" {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	if !found {
		t.Error("route POST /api/v1/export not registered on the application router")
	}
}
