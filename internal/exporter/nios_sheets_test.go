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

func indexOfString(hay []string, needle string) int {
	for i, value := range hay {
		if value == needle {
			return i
		}
	}
	return -1
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
	// Pool first: 2+2+2=6, then apply exactly one 20% growth ceiling.
	got := applyManagementGrowth(calcUddiTokensAggregated(findings), 0.20)
	if got != 8 {
		t.Errorf("pooled UDDI total with 20%% buffer = %d, want 8", got)
	}
}

func suppliedCaseInput() ReportInput {
	// These counts produce the supplied backup's exact pooled scenario totals:
	// Current=15,823; Hybrid=18,135; Full=31,002 before MS allocation.
	findings := []calculator.FindingRow{
		{Provider: "nios", Source: "migrate.example", Category: calculator.CategoryDDIObjects, Count: 115600, TokensPerUnit: calculator.NIOSTokensPerDDIObject},
		{Provider: "nios", Source: "stay.example", Category: calculator.CategoryDDIObjects, Count: 256950, TokensPerUnit: calculator.NIOSTokensPerDDIObject},
		{Provider: "nios", Source: "stay.example", Category: calculator.CategoryActiveIPs, Count: 209300, TokensPerUnit: calculator.NIOSTokensPerActiveIP},
	}
	return ReportInput{
		Findings:       findings,
		Totals:         TokenTotals{GrandTotal: 18013},
		ProviderTotals: map[string]int{"nios": 31002},
		NiosServerMetrics: []NiosServerMetricFull{
			{MemberName: "migrate.example", Role: "Member", QPS: 5000, LPS: 75, ObjectCount: 1000},
			{MemberName: "stay.example", Role: "Member", QPS: 10000, LPS: 150, ObjectCount: 5000},
		},
		NiosMigrationMap:   map[string]string{"migrate.example": "nios-x"},
		GrowthBufferPct:    0,
		SelectedMSScenario: "dns-only",
		MicrosoftAllocation: &MicrosoftAllocation{
			Diagnostic: MSAllocationDiagnostic{Available: true},
			Scenarios: []MSAllocationScenario{
				{ID: "none", DeltaTokens: 0},
				{ID: "dns-only", DeltaTokens: 2190},
				{ID: "dhcp-only", DeltaTokens: 0},
				{ID: "both", DeltaTokens: 2190},
			},
		},
	}
}

func TestBuild_NiosMigrationScenarios_SuppliedCaseParity(t *testing.T) {
	in := suppliedCaseInput()
	f := buildToExcelize(t, in)

	for _, tc := range []struct {
		row          int
		wantScenario string
		wantMgmt     string
		wantServer   string
	}{
		{2, "Current", "18,013", "0"},
		{3, "Hybrid", "20,325", "130"},
		{4, "Full", "31,002", "380"},
	} {
		scenario, _ := f.GetCellValue("NIOS Migration Scenarios", fmt.Sprintf("A%d", tc.row))
		mgmt, _ := f.GetCellValue("NIOS Migration Scenarios", fmt.Sprintf("B%d", tc.row))
		server, _ := f.GetCellValue("NIOS Migration Scenarios", fmt.Sprintf("C%d", tc.row))
		if scenario != tc.wantScenario || mgmt != tc.wantMgmt || server != tc.wantServer {
			t.Errorf("row %d = (%q, %q, %q), want (%q, %q, %q)", tc.row, scenario, mgmt, server, tc.wantScenario, tc.wantMgmt, tc.wantServer)
		}
	}

	summaryRows, err := f.GetRows("Summary")
	if err != nil {
		t.Fatal(err)
	}
	wantSummary := false
	for _, row := range summaryRows {
		if len(row) > 1 && row[0] == "NIOS" && row[1] == "18,013" {
			wantSummary = true
		}
	}
	if !wantSummary {
		t.Errorf("Summary must show selected current-state NIOS total 18,013: %v", summaryRows)
	}

	tokenCalcTotal, _ := f.GetCellValue("Token Calculation", "H2")
	providerTotal, _ := f.GetCellValue("NIOS", "H2")
	if tokenCalcTotal != "18,013" || providerTotal != "31,002" {
		t.Errorf("traceability totals = current %q, full provider %q; want 18,013 and 31,002", tokenCalcTotal, providerTotal)
	}
}

func TestBuild_NiosMigrationScenarios_AllSelectedOmitsHybridAndIgnoresStaleKeys(t *testing.T) {
	in := suppliedCaseInput()
	in.NiosMigrationMap = map[string]string{
		"migrate.example": "nios-x",
		"stay.example":    "nios-x",
		"stale.example":   "nios-x",
	}
	in.Findings = append(in.Findings, calculator.FindingRow{
		Provider: "nios", Source: "gm.example", Category: calculator.CategoryDDIObjects, Count: 50,
	})
	in.NiosServerMetrics = append(in.NiosServerMetrics, NiosServerMetricFull{
		MemberName: "gm.example", Role: "GM", RunsDnsDhcp: false,
	})

	f := buildToExcelize(t, in)
	rows, err := f.GetRows("NIOS Migration Scenarios")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if len(row) > 0 && row[0] == "Hybrid" {
			t.Fatalf("all selectable members selected must omit Hybrid: %v", rows)
		}
	}
	full, _ := f.GetCellValue("NIOS Migration Scenarios", "B3")
	if full != "31,004" {
		t.Errorf("Full = %q, want 31004 (native rates including infra-only GM, no MS delta)", full)
	}
}

func TestBuild_NiosMigrationScenarios_PartialIgnoresInfraOnlyMapEntry(t *testing.T) {
	in := suppliedCaseInput()
	in.NiosMigrationMap["gm.example"] = "nios-x"
	in.Findings = append(in.Findings, calculator.FindingRow{
		Provider: "nios", Source: "gm.example", Category: calculator.CategoryDDIObjects, Count: 50,
	})
	in.NiosServerMetrics = append(in.NiosServerMetrics, NiosServerMetricFull{
		MemberName: "gm.example", Role: "GM", RunsDnsDhcp: false,
	})

	f := buildToExcelize(t, in)
	hybrid, _ := f.GetCellValue("NIOS Migration Scenarios", "B3")
	if hybrid != "20,326" {
		t.Errorf("Hybrid = %q, want 20,326 (infra-only GM remains at current NIOS rate)", hybrid)
	}
}

func TestBuild_NiosMigrationScenarios_IncludesPooledNonNiosManagementTokens(t *testing.T) {
	in := suppliedCaseInput()
	// These two rows pool to one UDDI token. Summing per-provider totals would
	// incorrectly add two tokens, while omitting non-NIOS workload adds none.
	in.Findings = append(in.Findings,
		calculator.FindingRow{Provider: "aws", Source: "aws.example", Category: calculator.CategoryDDIObjects, Count: 13},
		calculator.FindingRow{Provider: "azure", Source: "azure.example", Category: calculator.CategoryDDIObjects, Count: 12},
	)

	f := buildToExcelize(t, in)
	for _, tc := range []struct {
		cell string
		want string
	}{
		{"B2", "18,014"},
		{"B3", "20,326"},
		{"B4", "31,003"},
	} {
		got, _ := f.GetCellValue("NIOS Migration Scenarios", tc.cell)
		if got != tc.want {
			t.Errorf("%s = %q, want %q (one pooled non-NIOS token included)", tc.cell, got, tc.want)
		}
	}
}

func TestBuild_NiosMigrationScenarios_PoolsMixedEffectiveRatesBeforeCeiling(t *testing.T) {
	in := ReportInput{
		Findings: []calculator.FindingRow{
			{Provider: "aws", Source: "aws.example", Category: calculator.CategoryDDIObjects, Count: 1, TokensPerUnit: calculator.TokensPerDDIObject},
			{Provider: "nios", Source: "stay.example", Category: calculator.CategoryDDIObjects, Count: 1, TokensPerUnit: calculator.NIOSTokensPerDDIObject},
		},
		NiosServerMetrics: []NiosServerMetricFull{
			{MemberName: "stay.example", Role: "Member"},
			{MemberName: "migrate.example", Role: "Member"},
		},
		NiosMigrationMap: map[string]string{"migrate.example": "nios-x"},
	}

	f := buildToExcelize(t, in)
	for _, tc := range []struct {
		cell     string
		scenario string
	}{
		{"B2", "Current"},
		{"B3", "Hybrid"},
		{"B4", "Full"},
	} {
		got, _ := f.GetCellValue("NIOS Migration Scenarios", tc.cell)
		if got != "1" {
			t.Errorf("%s management tokens = %q, want 1 for %s exact mixed-rate category pool", tc.scenario, got, tc.cell)
		}
	}
}

func TestNiosSheets_UseActualMigrationStatusForGmMembers(t *testing.T) {
	in := ReportInput{
		Findings: []calculator.FindingRow{
			{Provider: "nios", Source: "gm-migrated.example", Category: calculator.CategoryDDIObjects, Count: 25},
			{Provider: "nios", Source: "gm-retained.example", Category: calculator.CategoryDDIObjects, Count: 25},
		},
		NiosServerMetrics: []NiosServerMetricFull{
			{MemberName: "gm-migrated.example", Role: "GM", RunsDnsDhcp: true, QPS: 5_000, LPS: 75, ObjectCount: 1_000},
			{MemberName: "gm-management.example", Role: "GMC", RunsDnsDhcp: false},
			{MemberName: "gm-retained.example", Role: "GM", RunsDnsDhcp: true, QPS: 5_000, LPS: 75, ObjectCount: 1_000},
			{MemberName: "member-retained.example", Role: "Member", QPS: 5_000, LPS: 75, ObjectCount: 1_000},
		},
		NiosMigrationMap: map[string]string{
			"gm-migrated.example":   "nios-x",
			"gm-management.example": "nios-x",
		},
	}

	f := buildToExcelize(t, in)
	serverRows, err := f.GetRows("NIOS Server Tokens")
	if err != nil {
		t.Fatal(err)
	}
	serverText := fmt.Sprint(serverRows)
	if !strings.Contains(serverText, "gm-migrated.example") {
		t.Errorf("migrated workload-bearing GM must be sized: %v", serverRows)
	}
	if strings.Contains(serverText, "gm-management.example") || strings.Contains(serverText, "gm-retained.example") || strings.Contains(serverText, "member-retained.example") {
		t.Errorf("management-only and all retained members must be excluded from selected-plan server sizing: %v", serverRows)
	}

	detailRows, err := f.GetRows("NIOS Member Details")
	if err != nil {
		t.Fatal(err)
	}
	statuses := make(map[string]string)
	for _, row := range detailRows[1:] {
		if len(row) == 0 {
			continue
		}
		status := ""
		if len(row) > 13 {
			status = row[13]
		}
		statuses[row[0]] = status
	}
	if got := statuses["gm-migrated.example"]; got != "" {
		t.Errorf("migrated workload-bearing GM status = %q, want blank", got)
	}
	if got := statuses["gm-management.example"]; got != "Replaced by Infoblox Portal" {
		t.Errorf("migrated management-only GMC status = %q, want replacement label", got)
	}
	if got := statuses["gm-retained.example"]; got != "Retained on NIOS" {
		t.Errorf("unmigrated workload-bearing GM status = %q, want retained label", got)
	}
}

func TestCalcScenarioServerTokens_ConsolidatesMultipleXaasMembers(t *testing.T) {
	in := ReportInput{
		NiosServerMetrics: []NiosServerMetricFull{
			{MemberName: "xaas-1.example", Role: "DNS", QPS: 5_000, LPS: 75, ObjectCount: 1_000},
			{MemberName: "xaas-2.example", Role: "DHCP", QPS: 5_000, LPS: 75, ObjectCount: 1_000},
			{MemberName: "nios-x.example", Role: "DNS/DHCP", QPS: 5_000, LPS: 75, ObjectCount: 1_000},
			{MemberName: "unselected.example", Role: "DNS", QPS: 5_000, LPS: 75, ObjectCount: 1_000},
		},
		NiosMigrationMap: map[string]string{
			"xaas-1.example": "nios-xaas",
			"xaas-2.example": "nios-xaas",
			"nios-x.example": "nios-x",
		},
	}

	if got := calcScenarioServerTokens(in.NiosServerMetrics, in, false); got != 2_530 {
		t.Errorf("Hybrid server tokens = %d, want 2530 (one pooled XaaS S tier + one NIOS-X 2XS)", got)
	}
	if got := calcScenarioServerTokens(in.NiosServerMetrics, in, true); got != 2_660 {
		t.Errorf("Full server tokens = %d, want 2660 (one pooled XaaS S tier + two default NIOS-X 2XS members)", got)
	}
}

func TestNiosServerTokensSheet_UsesAuthoritativePooledXaasTotal(t *testing.T) {
	in := ReportInput{
		NiosServerMetrics: []NiosServerMetricFull{
			{MemberName: "xaas-1.example", Role: "DNS", QPS: 5_000, LPS: 75, ObjectCount: 1_000},
			{MemberName: "xaas-2.example", Role: "DHCP", QPS: 5_000, LPS: 75, ObjectCount: 1_000},
		},
		NiosMigrationMap: map[string]string{
			"xaas-1.example": "nios-xaas",
			"xaas-2.example": "nios-xaas",
		},
	}

	f := buildToExcelize(t, in)
	rows, err := f.GetRows("NIOS Server Tokens")
	if err != nil {
		t.Fatal(err)
	}

	memberRows := 0
	foundConsolidated := false
	foundTotal := false
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		switch {
		case row[0] == "xaas-1.example" || row[0] == "xaas-2.example":
			memberRows++
			if len(row) < 8 || row[7] != "Pooled XaaS" {
				t.Errorf("XaaS member row must identify pooled treatment: %v", row)
			}
			if len(row) > 8 && row[8] != "" {
				t.Errorf("XaaS member row must not claim individual tokens: %v", row)
			}
		case strings.HasPrefix(row[0], "XaaS CONSOLIDATED TOTAL"):
			foundConsolidated = true
			if len(row) < 9 || row[8] != "2,400" {
				t.Errorf("consolidated XaaS row = %v, want authoritative 2,400 tokens", row)
			}
		case row[0] == "TOTAL":
			foundTotal = true
			if len(row) < 9 || row[8] != "2,400" {
				t.Errorf("sheet TOTAL = %v, want pooled XaaS total 2,400 (not old individual sum 260)", row)
			}
		}
	}
	if memberRows != 2 || !foundConsolidated || !foundTotal {
		t.Errorf("missing XaaS traceability rows: members=%d consolidated=%v total=%v rows=%v", memberRows, foundConsolidated, foundTotal, rows)
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
	tests := []struct {
		name      string
		metric    NiosServerMetricFull
		overrides map[string]int
		want      int
	}{
		{
			name:   "authoritative scanned count",
			metric: NiosServerMetricFull{ObjectCount: 1_000, ServerObjectCount: 2_900, ActiveIPCount: 500},
			want:   2_900,
		},
		{
			name:   "legacy fallback uses DDI objects only",
			metric: NiosServerMetricFull{ObjectCount: 1_000, ActiveIPCount: 500},
			want:   1_000,
		},
		{
			name: "explicit zero override stays zero",
			metric: NiosServerMetricFull{
				MemberID: "m1", ObjectCount: 1_000, ServerObjectCount: 2_900,
			},
			overrides: map[string]int{"m1": 0},
			want:      0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := serverSizingObjects(&tc.metric, tc.overrides); got != tc.want {
				t.Errorf("serverSizingObjects() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCalcScenarioServerTokens_XaasUsesServerObjectCountAcrossTierBoundary(t *testing.T) {
	in := ReportInput{
		NiosServerMetrics: []NiosServerMetricFull{
			{
				MemberName: "dhcp-1.example", Role: "DHCP", QPS: 5_000, LPS: 75,
				ObjectCount: 1_000, ServerObjectCount: 15_000, ActiveIPCount: 14_000,
			},
			{
				MemberName: "dhcp-2.example", Role: "DHCP", QPS: 5_000, LPS: 75,
				ObjectCount: 1_000, ServerObjectCount: 15_000, ActiveIPCount: 14_000,
			},
		},
		NiosMigrationMap: map[string]string{
			"dhcp-1.example": "nios-xaas",
			"dhcp-2.example": "nios-xaas",
		},
	}

	// The pooled authoritative count is 30,000 objects, crossing the XaaS S
	// limit of 29,000 into M. The legacy DDI-only count would incorrectly
	// remain in S at 2,400 tokens.
	for _, tc := range []struct {
		name string
		full bool
	}{
		{name: "hybrid", full: false},
		{name: "full", full: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := calcScenarioServerTokens(in.NiosServerMetrics, in, tc.full); got != 4_100 {
				t.Errorf("calcScenarioServerTokens(full=%v) = %d, want 4100", tc.full, got)
			}
		})
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
	in := ReportInput{
		NiosServerMetrics: []NiosServerMetricFull{
			{MemberID: "m1", MemberName: "m1.test.local", Role: "DHCP", ObjectCount: 8, ServerObjectCount: 12, ActiveIPCount: 4},
			{MemberID: "legacy", MemberName: "legacy.test.local", Role: "DHCP", ObjectCount: 8, ActiveIPCount: 4},
			{MemberID: "zero", MemberName: "zero.test.local", Role: "DHCP", ObjectCount: 8, ServerObjectCount: 12, ActiveIPCount: 4},
		},
		NiosServerObjectOverrides: map[string]int{"zero": 0},
	}

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
	if len(rows) < 4 {
		t.Fatal("expected header + three data rows")
	}
	sizingColumn := indexOfString(rows[0], "Sizing Objects")
	if sizingColumn < 0 {
		t.Errorf("header row missing 'Sizing Objects': %v", rows[0])
		return
	}
	for row, want := range []string{"12", "8", "0"} {
		if got := rows[row+1][sizingColumn]; got != want {
			t.Errorf("row %d sizing objects = %q, want %q", row+2, got, want)
		}
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
