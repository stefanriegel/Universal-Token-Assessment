package exporter

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/calculator"
	"github.com/xuri/excelize/v2"
)

// contains reports whether needle is present in hay.
func contains(hay []string, needle string) bool {
	for _, v := range hay {
		if v == needle {
			return true
		}
	}
	return false
}

func TestCalcUddiTokensAggregated(t *testing.T) {
	findings := []calculator.FindingRow{
		{Provider: "nios", Category: calculator.CategoryDDIObjects, Item: "a", Count: 50},
		{Provider: "nios", Category: calculator.CategoryActiveIPs, Item: "b", Count: 26},
		{Provider: "nios", Category: calculator.CategoryManagedAssets, Item: "c", Count: 6},
	}
	// CeilDiv(50,25) + CeilDiv(26,13) + CeilDiv(6,3) = 2+2+2 = 6
	got := calcUddiTokensAggregated(findings)
	if got != 6 {
		t.Errorf("calcUddiTokensAggregated() = %d, want 6", got)
	}
}

func TestCalcNiosTokens(t *testing.T) {
	findings := []calculator.FindingRow{
		{Provider: "nios", Category: calculator.CategoryDDIObjects, Item: "a", Count: 50},
		{Provider: "nios", Category: calculator.CategoryActiveIPs, Item: "b", Count: 26},
		{Provider: "nios", Category: calculator.CategoryManagedAssets, Item: "c", Count: 6},
	}
	// CeilDiv(50,50) + CeilDiv(26,25) + CeilDiv(6,13) = 1 + 2 + 1 = 4 (SUM-native)
	got := calcNiosTokens(findings)
	if got != 4 {
		t.Errorf("calcNiosTokens() = %d, want 4", got)
	}
}

func TestCalcUddiTokensAggregated_WithGrowthBuffer(t *testing.T) {
	findings := []calculator.FindingRow{
		{Provider: "nios", Category: calculator.CategoryDDIObjects, Item: "a", Count: 50},
		{Provider: "nios", Category: calculator.CategoryActiveIPs, Item: "b", Count: 26},
		{Provider: "nios", Category: calculator.CategoryManagedAssets, Item: "c", Count: 6},
	}
	// Apply 20% growth: 50*1.2=60, 26*1.2=32 (ceil), 6*1.2=8 (ceil)
	buffered := applyGrowthToFindings(findings, 0.20)
	got := calcUddiTokensAggregated(buffered)
	// CeilDiv(60,25) + CeilDiv(32,13) + CeilDiv(8,3) = 3+3+3 = 9
	if got != 9 {
		t.Errorf("calcUddiTokensAggregated with 20%% buffer = %d, want 9", got)
	}
}

func TestGmStatusLabel(t *testing.T) {
	retained := &NiosServerMetricFull{Role: "GM", RunsDnsDhcp: true}
	if got := gmStatusLabel(retained, false); got != "Retained on NIOS" {
		t.Errorf("retained GM label = %q, want %q", got, "Retained on NIOS")
	}

	migratedNoDns := &NiosServerMetricFull{Role: "GM", RunsDnsDhcp: false}
	if got := gmStatusLabel(migratedNoDns, true); got != "Replaced by Infoblox Portal" {
		t.Errorf("migrated management-only GM label = %q, want %q", got, "Replaced by Infoblox Portal")
	}

	migratedDns := &NiosServerMetricFull{Role: "GM", RunsDnsDhcp: true}
	if got := gmStatusLabel(migratedDns, true); got != "" {
		t.Errorf("migrated GM with DNS/DHCP label = %q, want empty (sized normally)", got)
	}

	nonGm := &NiosServerMetricFull{Role: "DNS/DHCP", RunsDnsDhcp: true}
	if got := gmStatusLabel(nonGm, true); got != "" {
		t.Errorf("non-GM label = %q, want empty", got)
	}
}

func TestServerSizingObjects(t *testing.T) {
	m := &NiosServerMetricFull{ObjectCount: 1000, ActiveIPCount: 500}
	got := serverSizingObjects(m)
	if got != 1500 {
		t.Errorf("serverSizingObjects() = %d, want 1500", got)
	}
}

func TestCalcServerTokenTier(t *testing.T) {
	tests := []struct {
		name       string
		qps, lps   int
		sizingObjs int
		wantTier   string
		wantTokens int
	}{
		{"2XS tier", 5000, 75, 3000, "2XS", 130},
		{"XS tier", 10000, 150, 7500, "XS", 250},
		{"S tier", 20000, 200, 29000, "S", 470},
		{"XL cap", 200000, 1000, 1000000, "XL", 2700},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tier, tokens := calcServerTokenTier(tc.qps, tc.lps, tc.sizingObjs)
			if tier != tc.wantTier {
				t.Errorf("tier = %q, want %q", tier, tc.wantTier)
			}
			if tokens != tc.wantTokens {
				t.Errorf("tokens = %d, want %d", tokens, tc.wantTokens)
			}
		})
	}
}

func TestNiosSheets_SizingObjectsColumn(t *testing.T) {
	in := ReportInput{NiosServerMetrics: []NiosServerMetricFull{
		{MemberName: "m1.test.local", Role: "DHCP", ObjectCount: 8, ServerObjectCount: 12, ActiveIPCount: 4},
	}}

	f := excelize.NewFile()
	if _, err := f.NewSheet("NIOS Member Details"); err != nil {
		t.Fatal(err)
	}
	if err := buildNiosMemberDetailsSheet(f, sheetStyles{}, in); err != nil {
		t.Fatal(err)
	}

	rows, err := f.GetRows("NIOS Member Details")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 {
		t.Fatal("expected header + data rows")
	}
	if !contains(rows[0], "Sizing Objects") {
		t.Errorf("header row missing 'Sizing Objects': %v", rows[0])
	}
}

// A hardcoded buffer produces the same workbook for every buffer setting. This
// test fails the moment the constant comes back.
func TestBuild_GrowthBufferIsReadFromInput(t *testing.T) {
	base := buildMinimalInput()
	base.NiosServerMetrics = []NiosServerMetricFull{{
		MemberID: "m1", MemberName: "gm1", Role: "GM",
		QPS: 1000, LPS: 100, ObjectCount: 5000, ServerObjectCount: 5000,
		ActiveIPCount: 200, RunsDnsDhcp: true,
	}}
	base.Findings = append(base.Findings, calculator.FindingRow{
		Provider: "nios", Source: "gm1", Category: calculator.CategoryDDIObjects,
		Item: "DNS records", Count: 5000, TokensPerUnit: 50, ManagementTokens: 100,
	})

	low := base
	low.GrowthBufferPct = 0.20
	high := base
	high.GrowthBufferPct = 0.50

	lowCell := cellOf(t, buildToExcelize(t, low), "NIOS Migration Scenarios")
	highCell := cellOf(t, buildToExcelize(t, high), "NIOS Migration Scenarios")

	if lowCell == highCell {
		t.Fatalf("NIOS Migration Scenarios identical at 20%% and 50%% buffer (%q) — buffer is being ignored", lowCell)
	}
}

// The assumption row must disclose both buffers even though only the
// management buffer feeds this sheet's numbers.
func TestBuild_AssumptionRowDisclosesBothBuffers(t *testing.T) {
	base := buildMinimalInput()
	base.NiosServerMetrics = []NiosServerMetricFull{{
		MemberID: "m1", MemberName: "gm1", Role: "GM",
		QPS: 1000, LPS: 100, ObjectCount: 5000, ServerObjectCount: 5000,
		ActiveIPCount: 200, RunsDnsDhcp: true,
	}}
	base.Findings = append(base.Findings, calculator.FindingRow{
		Provider: "nios", Source: "gm1", Category: calculator.CategoryDDIObjects,
		Item: "DNS records", Count: 5000, TokensPerUnit: 50, ManagementTokens: 100,
	})
	base.GrowthBufferPct = 0.20
	base.ServerGrowthBufferPct = 0.35

	cell := cellOf(t, buildToExcelize(t, base), "NIOS Migration Scenarios")
	if !strings.Contains(cell, "20%") {
		t.Errorf("assumption row missing management buffer 20%%: %q", cell)
	}
	if !strings.Contains(cell, "35%") {
		t.Errorf("assumption row missing server buffer 35%%: %q", cell)
	}

	other := base
	other.ServerGrowthBufferPct = 0.50
	otherCell := cellOf(t, buildToExcelize(t, other), "NIOS Migration Scenarios")
	if otherCell == cell {
		t.Fatalf("assumption row unchanged when ServerGrowthBufferPct changed (%q) — not tracking the field", cell)
	}
	if !strings.Contains(otherCell, "50%") {
		t.Errorf("assumption row missing updated server buffer 50%%: %q", otherCell)
	}
}

// buildToExcelize runs the full Build pipeline and reopens the result, so the
// test exercises the same path production export does. package exporter has
// no black-box openResult helper (that lives in exporter_test's package
// exporter_test), so this is a local equivalent.
func buildToExcelize(t *testing.T, in ReportInput) *excelize.File {
	t.Helper()
	var buf bytes.Buffer
	if err := Build(&buf, in); err != nil {
		t.Fatalf("Build: %v", err)
	}
	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	return f
}

// cellOf returns the sheet's cells joined, so any numeric difference trips the
// comparison without pinning this test to one cell address.
func cellOf(t *testing.T, f *excelize.File, sheet string) string {
	t.Helper()
	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("GetRows(%s): %v", sheet, err)
	}
	return fmt.Sprint(rows)
}
