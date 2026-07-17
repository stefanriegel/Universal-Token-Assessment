package exporter

import (
	"encoding/json"
	"testing"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/calculator"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/session"
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
	retained := &niosServerMetricFull{Role: "GM", RunsDnsDhcp: true}
	if got := gmStatusLabel(retained, false); got != "Retained on NIOS" {
		t.Errorf("retained GM label = %q, want %q", got, "Retained on NIOS")
	}

	migratedNoDns := &niosServerMetricFull{Role: "GM", RunsDnsDhcp: false}
	if got := gmStatusLabel(migratedNoDns, true); got != "Replaced by Infoblox Portal" {
		t.Errorf("migrated management-only GM label = %q, want %q", got, "Replaced by Infoblox Portal")
	}

	migratedDns := &niosServerMetricFull{Role: "GM", RunsDnsDhcp: true}
	if got := gmStatusLabel(migratedDns, true); got != "" {
		t.Errorf("migrated GM with DNS/DHCP label = %q, want empty (sized normally)", got)
	}

	nonGm := &niosServerMetricFull{Role: "DNS/DHCP", RunsDnsDhcp: true}
	if got := gmStatusLabel(nonGm, true); got != "" {
		t.Errorf("non-GM label = %q, want empty", got)
	}
}

func TestServerSizingObjects(t *testing.T) {
	m := &niosServerMetricFull{ObjectCount: 1000, ActiveIPCount: 500}
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
	metrics := []niosServerMetricFull{
		{MemberName: "m1.test.local", Role: "DHCP", ObjectCount: 8, ServerObjectCount: 12, ActiveIPCount: 4},
	}
	data, err := json.Marshal(metrics)
	if err != nil {
		t.Fatal(err)
	}
	sess := &session.Session{NiosServerMetricsJSON: data}

	f := excelize.NewFile()
	if _, err := f.NewSheet("NIOS Member Details"); err != nil {
		t.Fatal(err)
	}
	if err := buildNiosMemberDetailsSheet(f, sheetStyles{}, sess); err != nil {
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
