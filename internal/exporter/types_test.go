package exporter

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/calculator"
)

// The JSON field names are the wire contract with the frontend. Renaming the Go
// field is fine; renaming the tag silently breaks the payload.
func TestNiosServerMetricFull_JSONFieldNames(t *testing.T) {
	b, err := json.Marshal(NiosServerMetricFull{MemberName: "gm1", ServerObjectCount: 7})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"memberId", "memberName", "role", "model", "platform",
		"qps", "lps", "objectCount", "serverObjectCount", "activeIPCount",
		"managedIPCount", "staticHosts", "dynamicHosts", "dhcpUtilization", "runsDnsDhcp"} {
		if _, ok := got[key]; !ok {
			t.Errorf("NiosServerMetricFull JSON is missing key %q", key)
		}
	}
}

func TestProviderErrorAndTotals_JSONFieldNames(t *testing.T) {
	b, _ := json.Marshal(ProviderError{Provider: "aws", Resource: "vpc", Message: "denied"})
	if string(b) != `{"provider":"aws","resource":"vpc","message":"denied"}` {
		t.Errorf("ProviderError JSON = %s", b)
	}
	b, _ = json.Marshal(TokenTotals{DDITokens: 1, IPTokens: 2, AssetTokens: 3, GrandTotal: 6})
	if string(b) != `{"ddiTokens":1,"ipTokens":2,"assetTokens":3,"grandTotal":6}` {
		t.Errorf("TokenTotals JSON = %s", b)
	}
}

// buildMinimalInput is the smallest ReportInput that must produce a valid
// workbook: no NIOS, no AD, no errors. Shared by later tests.
func buildMinimalInput() ReportInput {
	return ReportInput{
		GeneratedAt:       time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC),
		SelectedProviders: []string{"aws"},
		Findings: []calculator.FindingRow{{
			Provider: "aws", Source: "acct-1", Region: "us-east-1",
			Category: calculator.CategoryDDIObjects, Item: "Route53 records",
			Count: 100, TokensPerUnit: 25, ManagementTokens: 4,
		}},
		Totals:                TokenTotals{DDITokens: 4, GrandTotal: 4},
		ProviderTotals:        map[string]int{"aws": 4},
		GrowthBufferPct:       0.20,
		ServerGrowthBufferPct: 0.20,
	}
}

func TestBuild_AcceptsReportInput(t *testing.T) {
	var buf bytes.Buffer
	if err := Build(&buf, buildMinimalInput()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("Build wrote an empty workbook")
	}
}
