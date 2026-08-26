package exporter_test

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/calculator"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/exporter"
	"github.com/xuri/excelize/v2"
)

// testInput builds a minimal exporter.ReportInput for exporter tests. Totals are
// derived from findings the way a completed scan session derived them, so these
// fixtures render the same workbook the session-backed ones did.
func testInput(findings []calculator.FindingRow, errors []exporter.ProviderError) exporter.ReportInput {
	tr := calculator.Calculate(findings)
	byProvider := make(map[string][]calculator.FindingRow)
	for _, row := range findings {
		byProvider[row.Provider] = append(byProvider[row.Provider], row)
	}
	providerTotals := make(map[string]int, len(byProvider))
	for provider, rows := range byProvider {
		providerTotals[provider] = calculator.Calculate(rows).GrandTotal
	}
	return exporter.ReportInput{
		GeneratedAt: time.Now(),
		Findings:    findings,
		Errors:      errors,
		Totals: exporter.TokenTotals{
			DDITokens:   tr.DDITokens,
			IPTokens:    tr.IPTokens,
			AssetTokens: tr.AssetTokens,
			GrandTotal:  tr.GrandTotal,
		},
		ProviderTotals:        providerTotals,
		GrowthBufferPct:       0.20,
		ServerGrowthBufferPct: 0.20,
	}
}

// awsFindings returns a slice of FindingRow with a single AWS DDI-Objects row.
func awsFindings() []calculator.FindingRow {
	return []calculator.FindingRow{
		{
			Provider:      "aws",
			Source:        "123456789",
			Region:        "us-east-1",
			Category:      calculator.CategoryDDIObjects,
			Item:          "vpc",
			Count:         50,
			TokensPerUnit: calculator.TokensPerDDIObject,
		},
	}
}

// threeFindings returns 3 FindingRow entries for row-count tests.
func threeFindings() []calculator.FindingRow {
	return []calculator.FindingRow{
		{
			Provider:      "aws",
			Source:        "111",
			Category:      calculator.CategoryDDIObjects,
			Item:          "vpc",
			Count:         25,
			TokensPerUnit: calculator.TokensPerDDIObject,
		},
		{
			Provider:      "aws",
			Source:        "111",
			Category:      calculator.CategoryActiveIPs,
			Item:          "ec2_ip",
			Count:         13,
			TokensPerUnit: calculator.TokensPerActiveIP,
		},
		{
			Provider:      "aws",
			Source:        "111",
			Category:      calculator.CategoryManagedAssets,
			Item:          "ec2_instance",
			Count:         3,
			TokensPerUnit: calculator.TokensPerManagedAsset,
		},
	}
}

// openResult calls exporter.Build and opens the resulting bytes with excelize.OpenReader.
// Returns the opened file and the bytes buffer for further inspection, or fails the test.
func openResult(t *testing.T, in exporter.ReportInput) *excelize.File {
	t.Helper()
	var buf bytes.Buffer
	if err := exporter.Build(&buf, in); err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}
	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatalf("excelize.OpenReader() returned error: %v", err)
	}
	return f
}

// sheetExists returns true if name appears in f.GetSheetList().
func sheetExists(f *excelize.File, name string) bool {
	for _, s := range f.GetSheetList() {
		if s == name {
			return true
		}
	}
	return false
}

// TestBuild_SummarySheet asserts that Build produces a sheet named "Summary".
func TestBuild_SummarySheet(t *testing.T) {
	f := openResult(t, testInput(awsFindings(), nil))
	if !sheetExists(f, "Summary") {
		t.Errorf("expected sheet %q to exist; got sheets: %v", "Summary", f.GetSheetList())
	}
}

// TestBuild_SummarySubtotalAggregateThenDivide asserts that the per-provider subtotal
// on the Summary sheet uses aggregate-then-divide (calculator.Calculate), not a sum of
// per-row ManagementTokens. Five nios Active-IP rows of Count=5 (25 total) diverge under
// the two methods: per-row-sum = 5 (5 rows x ceil(5/13)=1), aggregate-then-divide = ceil(25/13) = 2.
func TestBuild_SummarySubtotalAggregateThenDivide(t *testing.T) {
	var findings []calculator.FindingRow
	for i := 0; i < 5; i++ {
		findings = append(findings, calculator.FindingRow{
			Provider:         "nios",
			Source:           "member",
			Category:         calculator.CategoryActiveIPs,
			Item:             "lease",
			Count:            5,
			TokensPerUnit:    calculator.TokensPerActiveIP,
			ManagementTokens: calculator.CeilDiv(5, calculator.TokensPerActiveIP),
		})
	}

	oracle := calculator.Calculate(findings).GrandTotal

	f := openResult(t, testInput(findings, nil))

	rows, err := f.GetRows("Summary")
	if err != nil {
		t.Fatalf("GetRows(Summary) error: %v", err)
	}

	var got string
	found := false
	for _, r := range rows {
		if len(r) > 0 && r[0] == "NIOS" {
			if len(r) > 1 {
				got = r[1]
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a NIOS subtotal row in Summary sheet, got rows: %v", rows)
	}

	gotInt, err := strconv.Atoi(got)
	if err != nil {
		t.Fatalf("NIOS subtotal cell %q is not an int: %v", got, err)
	}
	if gotInt != oracle {
		t.Errorf("NIOS subtotal = %d, want %d (aggregate-then-divide)", gotInt, oracle)
	}
}

// TestBuild_TokenCalcSheet asserts that Build produces a sheet named "Token Calculation".
func TestBuild_TokenCalcSheet(t *testing.T) {
	f := openResult(t, testInput(awsFindings(), nil))
	if !sheetExists(f, "Token Calculation") {
		t.Errorf("expected sheet %q to exist; got sheets: %v", "Token Calculation", f.GetSheetList())
	}
}

// TestBuild_TokenCalcRowCount asserts that the "Token Calculation" sheet has
// 1 header row + len(findings) data rows = 4 rows total for 3 findings.
func TestBuild_TokenCalcRowCount(t *testing.T) {
	findings := threeFindings()
	f := openResult(t, testInput(findings, nil))
	rows, err := f.GetRows("Token Calculation")
	if err != nil {
		t.Fatalf("GetRows(Token Calculation): %v", err)
	}
	want := len(findings) + 1 // 1 header + n data rows
	if len(rows) != want {
		t.Errorf("expected %d rows in Token Calculation, got %d", want, len(rows))
	}
}

// TestBuild_ProviderTab asserts that when findings include "aws" rows,
// Build produces a sheet named "AWS".
func TestBuild_ProviderTab(t *testing.T) {
	f := openResult(t, testInput(awsFindings(), nil))
	if !sheetExists(f, "AWS") {
		t.Errorf("expected sheet %q to exist; got sheets: %v", "AWS", f.GetSheetList())
	}
}

// TestBuild_ProviderTabOmitted asserts that when findings contain no "azure" rows,
// no "Azure" sheet is created.
func TestBuild_ProviderTabOmitted(t *testing.T) {
	// Only AWS findings — no Azure data.
	f := openResult(t, testInput(awsFindings(), nil))
	if sheetExists(f, "Azure") {
		t.Errorf("expected sheet %q to NOT exist; got sheets: %v", "Azure", f.GetSheetList())
	}
}

// TestBuild_ErrorsTab asserts that when in.Errors is non-empty,
// Build produces a sheet named "Errors".
func TestBuild_ErrorsTab(t *testing.T) {
	errors := []exporter.ProviderError{
		{Provider: "aws", Resource: "ec2", Message: "access denied"},
	}
	f := openResult(t, testInput(awsFindings(), errors))
	if !sheetExists(f, "Errors") {
		t.Errorf("expected sheet %q to exist; got sheets: %v", "Errors", f.GetSheetList())
	}
}

// TestBuild_ErrorsTabOmitted asserts that when in.Errors is empty,
// no "Errors" sheet is created.
func TestBuild_ErrorsTabOmitted(t *testing.T) {
	f := openResult(t, testInput(awsFindings(), nil))
	if sheetExists(f, "Errors") {
		t.Errorf("expected sheet %q to NOT exist; got sheets: %v", "Errors", f.GetSheetList())
	}
}

// TestBuild_ADMigrationPlannerSheet asserts that when ADServerMetrics is set,
// the "AD Migration Planner" sheet is created with expected headers and data.
func TestBuild_ADMigrationPlannerSheet(t *testing.T) {
	adFindings := []calculator.FindingRow{
		{Provider: "ad", Source: "DC01", Category: calculator.CategoryDDIObjects, Item: "dns_zone", Count: 10, TokensPerUnit: calculator.TokensPerDDIObject, ManagementTokens: 1},
		{Provider: "ad", Source: "DC01", Category: calculator.CategoryManagedAssets, Item: "user_account", Count: 500, TokensPerUnit: calculator.TokensPerManagedAsset, ManagementTokens: 5},
		{Provider: "ad", Source: "DC01", Category: calculator.CategoryManagedAssets, Item: "computer_count", Count: 200, TokensPerUnit: calculator.TokensPerManagedAsset, ManagementTokens: 2},
		{Provider: "ad", Source: "DC01", Category: calculator.CategoryActiveIPs, Item: "static_ip_count", Count: 50, TokensPerUnit: calculator.TokensPerActiveIP, ManagementTokens: 1},
	}

	in := testInput(adFindings, nil)
	in.ADServerMetrics = []exporter.ADServerMetric{
		{Hostname: "DC01", DNSObjects: 100, DHCPObjects: 50, DHCPObjectsWithOverhead: 60, Tier: "2XS", ServerTokens: 130},
		{Hostname: "DC02", DNSObjects: 5000, DHCPObjects: 3000, DHCPObjectsWithOverhead: 3600, QPS: 1000, LPS: 50, Tier: "XS", ServerTokens: 250},
	}

	f := openResult(t, in)
	if !sheetExists(f, "AD Migration Planner") {
		t.Fatalf("expected sheet %q to exist; got sheets: %v", "AD Migration Planner", f.GetSheetList())
	}

	// Verify header row
	cellA1, _ := f.GetCellValue("AD Migration Planner", "A1")
	if cellA1 != "DC Hostname" {
		t.Errorf("A1 = %q, want 'DC Hostname'", cellA1)
	}
	cellG1, _ := f.GetCellValue("AD Migration Planner", "G1")
	if cellG1 != "Form Factor" {
		t.Errorf("G1 = %q, want 'Form Factor'", cellG1)
	}
	cellH1, _ := f.GetCellValue("AD Migration Planner", "H1")
	if cellH1 != "NIOS-X Tier" {
		t.Errorf("H1 = %q, want 'NIOS-X Tier'", cellH1)
	}
	cellI1, _ := f.GetCellValue("AD Migration Planner", "I1")
	if cellI1 != "Server Tokens" {
		t.Errorf("I1 = %q, want 'Server Tokens'", cellI1)
	}

	// Verify data rows
	cellA2, _ := f.GetCellValue("AD Migration Planner", "A2")
	if cellA2 != "DC01" {
		t.Errorf("A2 = %q, want 'DC01'", cellA2)
	}
	cellA3, _ := f.GetCellValue("AD Migration Planner", "A3")
	if cellA3 != "DC02" {
		t.Errorf("A3 = %q, want 'DC02'", cellA3)
	}
	cellG2, _ := f.GetCellValue("AD Migration Planner", "G2")
	if cellG2 != "NIOS-X" {
		t.Errorf("G2 (form factor) = %q, want 'NIOS-X'", cellG2)
	}
	cellH2, _ := f.GetCellValue("AD Migration Planner", "H2")
	if cellH2 != "2XS" {
		t.Errorf("H2 (tier) = %q, want '2XS'", cellH2)
	}
}

// TestBuild_ADMigrationPlannerOmitted asserts that when ADServerMetricsJSON is nil/empty,
// no "AD Migration Planner" sheet is created.
func TestBuild_ADMigrationPlannerOmitted(t *testing.T) {
	f := openResult(t, testInput(awsFindings(), nil))
	if sheetExists(f, "AD Migration Planner") {
		t.Errorf("expected sheet %q to NOT exist when ADServerMetricsJSON is empty; got sheets: %v", "AD Migration Planner", f.GetSheetList())
	}
}

// TestBuild_SKUSheet_WithServerMetrics asserts that the "Recommended SKUs" sheet
// contains correct MGMT and SERV pack counts when AD server metrics are present.
func TestBuild_SKUSheet_WithServerMetrics(t *testing.T) {
	// Use testInput with findings, then override the grand total directly
	// for deterministic pack count testing.
	in := testInput(awsFindings(), nil)
	in.Totals.GrandTotal = 2500 // → ceil(2500/1000) = 3 MGMT packs
	// AD server metrics: presentation data for the AD Migration Planner sheet.
	// The SKU sheet reads the report's own TotalServerTokens (already
	// XaaS-consolidated), not these raw per-DC metrics.
	in.ADServerMetrics = []exporter.ADServerMetric{
		{Hostname: "DC01", DNSObjects: 100, DHCPObjects: 50, DHCPObjectsWithOverhead: 60, Tier: "2XS", ServerTokens: 800},
	}
	in.TotalServerTokens = 800 // → ceil(800/500) = 2 SERV packs

	f := openResult(t, in)

	if !sheetExists(f, "Recommended SKUs") {
		t.Fatalf("expected sheet 'Recommended SKUs' to exist; got sheets: %v", f.GetSheetList())
	}

	// Header row
	a1, _ := f.GetCellValue("Recommended SKUs", "A1")
	if a1 != "SKU Code" {
		t.Errorf("A1 = %q, want 'SKU Code'", a1)
	}

	// MGMT row
	a2, _ := f.GetCellValue("Recommended SKUs", "A2")
	if a2 != "IB-TOKENS-UDDI-MGMT-1000" {
		t.Errorf("A2 (SKU Code) = %q, want 'IB-TOKENS-UDDI-MGMT-1000'", a2)
	}
	c2, _ := f.GetCellValue("Recommended SKUs", "C2")
	if c2 != "3" {
		t.Errorf("C2 (MGMT packs) = %q, want '3'", c2)
	}

	// SERV row
	a3, _ := f.GetCellValue("Recommended SKUs", "A3")
	if a3 != "IB-TOKENS-UDDI-SERV-500" {
		t.Errorf("A3 (SKU Code) = %q, want 'IB-TOKENS-UDDI-SERV-500'", a3)
	}
	c3, _ := f.GetCellValue("Recommended SKUs", "C3")
	if c3 != "2" {
		t.Errorf("C3 (SERV packs) = %q, want '2'", c3)
	}
}

// TestBuild_SKUSheet_NoServerMetrics asserts that the SERV row is absent
// when no server metrics JSON is provided.
func TestBuild_SKUSheet_NoServerMetrics(t *testing.T) {
	f := openResult(t, testInput(awsFindings(), nil))

	if !sheetExists(f, "Recommended SKUs") {
		t.Fatalf("expected sheet 'Recommended SKUs' to exist; got sheets: %v", f.GetSheetList())
	}

	// MGMT row should still be present
	a2, _ := f.GetCellValue("Recommended SKUs", "A2")
	if a2 != "IB-TOKENS-UDDI-MGMT-1000" {
		t.Errorf("A2 (SKU Code) = %q, want 'IB-TOKENS-UDDI-MGMT-1000'", a2)
	}

	// SERV row should be absent (A3 empty)
	a3, _ := f.GetCellValue("Recommended SKUs", "A3")
	if a3 != "" {
		t.Errorf("A3 should be empty when no server metrics; got %q", a3)
	}
}

// TestBuild_SKUSheet_WithNIOSMetrics asserts NIOS server token tier calculation
// produces correct SERV pack count.
func TestBuild_SKUSheet_WithNIOSMetrics(t *testing.T) {
	findings := []calculator.FindingRow{
		{
			Provider:         "nios",
			Source:           "grid01",
			Category:         calculator.CategoryDDIObjects,
			Item:             "dns_zone",
			Count:            1000,
			TokensPerUnit:    1,
			ManagementTokens: 1000,
		},
	}
	in := testInput(findings, nil)
	// NIOS metrics: presentation data for the NIOS migration sheets. The SKU
	// sheet reads the report's own TotalServerTokens, not these raw metrics.
	in.NiosServerMetrics = []exporter.NiosServerMetricFull{
		{QPS: 5000, LPS: 75, ObjectCount: 3000},
		{QPS: 15000, LPS: 150, ObjectCount: 20000},
	}
	in.TotalServerTokens = 600 // → ceil(600/500) = 2 SERV packs

	f := openResult(t, in)

	// MGMT: ceil(1000/1000) = 1
	c2, _ := f.GetCellValue("Recommended SKUs", "C2")
	if c2 != "1" {
		t.Errorf("C2 (MGMT packs) = %q, want '1'", c2)
	}

	// SERV: ceil(600/500) = 2
	a3, _ := f.GetCellValue("Recommended SKUs", "A3")
	if a3 != "IB-TOKENS-UDDI-SERV-500" {
		t.Errorf("A3 = %q, want 'IB-TOKENS-UDDI-SERV-500'", a3)
	}
	c3, _ := f.GetCellValue("Recommended SKUs", "C3")
	if c3 != "2" {
		t.Errorf("C3 (SERV packs) = %q, want '2'", c3)
	}
}

// TestBuild_SKUPacksComeFromSuppliedTotals asserts the SKU sheet sizes MGMT,
// SERV and REPORTING packs directly from the report's own totals, not from a
// recomputation over raw per-member metrics.
func TestBuild_SKUPacksComeFromSuppliedTotals(t *testing.T) {
	in := testInput(awsFindings(), nil)
	in.Totals.GrandTotal = 2500 // → 3 mgmt packs
	in.TotalServerTokens = 1200 // → 3 serv packs
	in.ReportingTokens = 90     // → 3 reporting packs

	f := openResult(t, in)

	want := [][2]string{
		{"A2", "IB-TOKENS-UDDI-MGMT-1000"},
		{"C2", "3"},
		{"A3", "IB-TOKENS-UDDI-SERV-500"},
		{"C3", "3"},
		{"A4", "IB-TOKENS-REPORTING-40"},
		{"C4", "3"},
	}
	for _, w := range want {
		got, err := f.GetCellValue("Recommended SKUs", w[0])
		if err != nil {
			t.Fatalf("GetCellValue(%s): %v", w[0], err)
		}
		if got != w[1] {
			t.Errorf("Recommended SKUs!%s = %q, want %q", w[0], got, w[1])
		}
	}
}

// TestBuild_SKUOmitsZeroRows asserts SERV and REPORTING rows are omitted
// when their supplied totals are zero.
func TestBuild_SKUOmitsZeroRows(t *testing.T) {
	in := testInput(awsFindings(), nil)
	in.Totals.GrandTotal = 100
	in.TotalServerTokens = 0
	in.ReportingTokens = 0

	f := openResult(t, in)

	if got, _ := f.GetCellValue("Recommended SKUs", "A3"); got != "" {
		t.Errorf("Recommended SKUs!A3 = %q, want empty (no server tokens)", got)
	}
}

// niosTestInput builds a ReportInput with NIOS findings and NiosServerMetrics for
// testing the three NIOS migration sheets.
// NIOS findings: 1000 DDI Objects, 500 Active IPs, 100 Managed Assets.
// Members: GM (infra-only), member1, member2 (workload).
func niosTestInput() exporter.ReportInput {
	findings := []calculator.FindingRow{
		{Provider: "nios", Source: "grid01", Category: calculator.CategoryDDIObjects, Item: "dns_zone", Count: 1000, TokensPerUnit: calculator.NIOSTokensPerDDIObject, ManagementTokens: 20},
		{Provider: "nios", Source: "grid01", Category: calculator.CategoryActiveIPs, Item: "active_ip", Count: 500, TokensPerUnit: calculator.NIOSTokensPerActiveIP, ManagementTokens: 20},
		{Provider: "nios", Source: "grid01", Category: calculator.CategoryManagedAssets, Item: "managed_asset", Count: 100, TokensPerUnit: calculator.NIOSTokensPerManagedAsset, ManagementTokens: 8},
	}
	in := testInput(findings, nil)
	in.NiosServerMetrics = []exporter.NiosServerMetricFull{
		{MemberID: "gm-01", MemberName: "gm.grid.local", Role: "GM", Model: "IB-4030", Platform: "PHYSICAL"},
		{MemberID: "m1-01", MemberName: "member1.grid.local", Role: "Member", Model: "IB-4030", Platform: "PHYSICAL",
			QPS: 5000, LPS: 75, ObjectCount: 2000, ActiveIPCount: 1000, ManagedIPCount: 500, StaticHosts: 100, DynamicHosts: 200, DHCPUtilization: 450},
		{MemberID: "m2-01", MemberName: "member2.grid.local", Role: "Member", Model: "CP-V2200", Platform: "VIRTUAL",
			QPS: 10000, LPS: 150, ObjectCount: 5000, ActiveIPCount: 2500, ManagedIPCount: 1200, StaticHosts: 300, DynamicHosts: 500, DHCPUtilization: 750},
	}
	return in
}

// TestBuild_NiosMigrationScenarios verifies the NIOS Migration Scenarios sheet exists
// with correct scenario rows and token math.
func TestBuild_NiosMigrationScenarios(t *testing.T) {
	f := openResult(t, niosTestInput())

	if !sheetExists(f, "NIOS Migration Scenarios") {
		t.Fatalf("expected sheet 'NIOS Migration Scenarios' to exist; got sheets: %v", f.GetSheetList())
	}

	// Verify header row
	a1, _ := f.GetCellValue("NIOS Migration Scenarios", "A1")
	if a1 != "Scenario" {
		t.Errorf("A1 = %q, want 'Scenario'", a1)
	}

	// Row 2: Current (NIOS Only) -- NIOS tokens = CeilDiv(1000,50) + CeilDiv(500,25) + CeilDiv(100,13)
	// = 20 + 20 + 8 = 48 (SUM-native)
	a2, _ := f.GetCellValue("NIOS Migration Scenarios", "A2")
	if a2 != "Current (NIOS Only)" {
		t.Errorf("A2 = %q, want 'Current (NIOS Only)'", a2)
	}
	b2, _ := f.GetCellValue("NIOS Migration Scenarios", "B2")
	if b2 != "48" {
		t.Errorf("B2 (current NIOS tokens) = %q, want '48'", b2)
	}

	// Row 3: Full Universal DDI -- UDDI tokens with 20% buffer
	// 1000*1.2=1200 DDI, 500*1.2=600 IPs, 100*1.2=120 Assets
	// CeilDiv(1200,25) + CeilDiv(600,13) + CeilDiv(120,3) = 48+47+40 = 135
	a3, _ := f.GetCellValue("NIOS Migration Scenarios", "A3")
	if a3 != "Full Universal DDI" {
		t.Errorf("A3 = %q, want 'Full Universal DDI'", a3)
	}
	b3, _ := f.GetCellValue("NIOS Migration Scenarios", "B3")
	if b3 != "135" {
		t.Errorf("B3 (full UDDI tokens) = %q, want '135'", b3)
	}

	// Verify server tokens in Full UDDI row
	// member1: sizingObjects = 2000+1000=3000, tier match: qps=5000, lps=75, obj=3000 -> 2XS (130)
	// member2: sizingObjects = 5000+2500=7500, tier match: qps=10000, lps=150, obj=7500 -> XS (250)
	// GM is infra-only, excluded. Total = 130+250 = 380
	c3, _ := f.GetCellValue("NIOS Migration Scenarios", "C3")
	if c3 != "380" {
		t.Errorf("C3 (full UDDI server tokens) = %q, want '380'", c3)
	}
}

// TestBuild_NiosServerTokens verifies the NIOS Server Tokens sheet excludes infra-only GM.
func TestBuild_NiosServerTokens(t *testing.T) {
	f := openResult(t, niosTestInput())

	if !sheetExists(f, "NIOS Server Tokens") {
		t.Fatalf("expected sheet 'NIOS Server Tokens' to exist; got sheets: %v", f.GetSheetList())
	}

	// Header
	a1, _ := f.GetCellValue("NIOS Server Tokens", "A1")
	if a1 != "Grid Member" {
		t.Errorf("A1 = %q, want 'Grid Member'", a1)
	}

	// Row 2 should be member1 (infra-only GM excluded)
	a2, _ := f.GetCellValue("NIOS Server Tokens", "A2")
	if a2 != "member1.grid.local" {
		t.Errorf("A2 = %q, want 'member1.grid.local'", a2)
	}

	// Row 3 should be member2
	a3, _ := f.GetCellValue("NIOS Server Tokens", "A3")
	if a3 != "member2.grid.local" {
		t.Errorf("A3 = %q, want 'member2.grid.local'", a3)
	}

	// Row 4 blank, Row 5 TOTAL
	a5, _ := f.GetCellValue("NIOS Server Tokens", "A5")
	if a5 != "TOTAL" {
		t.Errorf("A5 = %q, want 'TOTAL'", a5)
	}

	// TOTAL server tokens = 130 + 250 = 380
	i5, _ := f.GetCellValue("NIOS Server Tokens", "I5")
	if i5 != "380" {
		t.Errorf("I5 (total server tokens) = %q, want '380'", i5)
	}
}

// TestBuild_NiosMemberDetails verifies ALL members are included (including infra-only GM).
func TestBuild_NiosMemberDetails(t *testing.T) {
	f := openResult(t, niosTestInput())

	if !sheetExists(f, "NIOS Member Details") {
		t.Fatalf("expected sheet 'NIOS Member Details' to exist; got sheets: %v", f.GetSheetList())
	}

	// Header
	a1, _ := f.GetCellValue("NIOS Member Details", "A1")
	if a1 != "Grid Member" {
		t.Errorf("A1 = %q, want 'Grid Member'", a1)
	}

	// ALL 3 members should be present (including infra-only GM)
	a2, _ := f.GetCellValue("NIOS Member Details", "A2")
	if a2 != "gm.grid.local" {
		t.Errorf("A2 = %q, want 'gm.grid.local'", a2)
	}
	a3, _ := f.GetCellValue("NIOS Member Details", "A3")
	if a3 != "member1.grid.local" {
		t.Errorf("A3 = %q, want 'member1.grid.local'", a3)
	}
	a4, _ := f.GetCellValue("NIOS Member Details", "A4")
	if a4 != "member2.grid.local" {
		t.Errorf("A4 = %q, want 'member2.grid.local'", a4)
	}

	// Verify Model column (C)
	c2, _ := f.GetCellValue("NIOS Member Details", "C2")
	if c2 != "IB-4030" {
		t.Errorf("C2 (model) = %q, want 'IB-4030'", c2)
	}
	c4, _ := f.GetCellValue("NIOS Member Details", "C4")
	if c4 != "CP-V2200" {
		t.Errorf("C4 (model) = %q, want 'CP-V2200'", c4)
	}

	// Verify Platform column (D)
	d2, _ := f.GetCellValue("NIOS Member Details", "D2")
	if d2 != "PHYSICAL" {
		t.Errorf("D2 (platform) = %q, want 'PHYSICAL'", d2)
	}

	// Verify Status column (N) for GM
	m2, _ := f.GetCellValue("NIOS Member Details", "N2")
	if m2 != "Retained on NIOS" {
		t.Errorf("N2 (status for GM) = %q, want 'Retained on NIOS'", m2)
	}
	m3, _ := f.GetCellValue("NIOS Member Details", "N3")
	if m3 != "" {
		t.Errorf("N3 (status for member1) = %q, want empty", m3)
	}
}

// TestBuild_NiosSheetsOmitted asserts that when NiosServerMetricsJSON is empty,
// none of the three NIOS migration sheets appear.
func TestBuild_NiosSheetsOmitted(t *testing.T) {
	f := openResult(t, testInput(awsFindings(), nil))

	for _, sheet := range []string{"NIOS Migration Scenarios", "NIOS Server Tokens", "NIOS Member Details"} {
		if sheetExists(f, sheet) {
			t.Errorf("expected sheet %q to NOT exist when NiosServerMetricsJSON is empty; got sheets: %v", sheet, f.GetSheetList())
		}
	}
}

// getCellNumFmt returns the NumFmt ID for a cell. Helper for formatting tests.
func getCellNumFmt(t *testing.T, f *excelize.File, sheet, cell string) int {
	t.Helper()
	styleID, err := f.GetCellStyle(sheet, cell)
	if err != nil {
		t.Fatalf("GetCellStyle(%s, %s): %v", sheet, cell, err)
	}
	style, err := f.GetStyle(styleID)
	if err != nil {
		t.Fatalf("GetStyle(%d): %v", styleID, err)
	}
	return style.NumFmt
}

// TestBuild_NumberFormatting verifies that numeric cells in Summary and Token Calculation
// sheets use NumFmt 3 (#,##0 thousand separator format).
func TestBuild_NumberFormatting(t *testing.T) {
	f := openResult(t, testInput(awsFindings(), nil))

	// Summary B2 = Grand Total — should have NumFmt 3
	numFmt := getCellNumFmt(t, f, "Summary", "B2")
	if numFmt != 3 {
		t.Errorf("Summary B2 NumFmt = %d, want 3 (#,##0)", numFmt)
	}

	// Token Calculation E2 = Count column — should have NumFmt 3
	numFmt = getCellNumFmt(t, f, "Token Calculation", "E2")
	if numFmt != 3 {
		t.Errorf("Token Calculation E2 NumFmt = %d, want 3 (#,##0)", numFmt)
	}

	// Token Calculation G2 = Management Tokens — should have NumFmt 3
	numFmt = getCellNumFmt(t, f, "Token Calculation", "G2")
	if numFmt != 3 {
		t.Errorf("Token Calculation G2 NumFmt = %d, want 3 (#,##0)", numFmt)
	}
}

// TestBuild_FrozenPanes verifies that all sheets have frozen first row (YSplit=1).
func TestBuild_FrozenPanes(t *testing.T) {
	// Build the workbook, write to buffer, re-open from bytes.
	var buf bytes.Buffer
	if err := exporter.Build(&buf, testInput(awsFindings(), nil)); err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}
	b := buf.Bytes()
	f, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("excelize.OpenReader() returned error: %v", err)
	}

	for _, sheet := range f.GetSheetList() {
		// Force worksheet XML load by reading a cell.
		_, _ = f.GetCellValue(sheet, "A1")

		panes, err := f.GetPanes(sheet)
		if err != nil {
			t.Fatalf("GetPanes(%s): %v", sheet, err)
		}
		if !panes.Freeze {
			t.Errorf("sheet %q: Freeze = false, want true", sheet)
		}
		if panes.YSplit != 1 {
			t.Errorf("sheet %q: YSplit = %d, want 1", sheet, panes.YSplit)
		}
	}
}

// TestBuild_PercentageFormatting verifies that the DHCP Utilization column
// in NIOS Member Details uses a percentage format (custom 0.0%).
func TestBuild_PercentageFormatting(t *testing.T) {
	f := openResult(t, niosTestInput())

	// NIOS Member Details M3 = DHCP Util % for member1 (dhcpUtilization=450 → 0.45)
	styleID, err := f.GetCellStyle("NIOS Member Details", "M3")
	if err != nil {
		t.Fatalf("GetCellStyle(NIOS Member Details, M3): %v", err)
	}
	style, err := f.GetStyle(styleID)
	if err != nil {
		t.Fatalf("GetStyle(%d): %v", styleID, err)
	}
	// Custom percentage format — NumFmt should be >= 164 (custom) or have CustomNumFmt set
	if style.CustomNumFmt == nil || *style.CustomNumFmt != "0.0%" {
		customStr := "<nil>"
		if style.CustomNumFmt != nil {
			customStr = *style.CustomNumFmt
		}
		t.Errorf("NIOS Member Details M3 CustomNumFmt = %s, want '0.0%%'", customStr)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Phase 28 — Resource Savings sheet tests (D-20)
// ─────────────────────────────────────────────────────────────────────────────

// buildAndOpen calls exporter.Build with the given variantOverrides map and
// re-opens the resulting bytes via excelize.OpenReader.
func buildAndOpen(t *testing.T, in exporter.ReportInput, overrides map[string]int) *excelize.File {
	t.Helper()
	in.VariantOverrides = overrides
	var buf bytes.Buffer
	if err := exporter.Build(&buf, in); err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}
	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatalf("excelize.OpenReader() returned error: %v", err)
	}
	return f
}

// indexOf returns the position of want in xs, or -1.
func indexOf(xs []string, want string) int {
	for i, x := range xs {
		if x == want {
			return i
		}
	}
	return -1
}

// findRowContaining scans column `col` (e.g. "A") on `sheet` for the literal
// `value` and returns its 1-based row number. Fails the test on miss.
func findRowContaining(t *testing.T, f *excelize.File, sheet, col, value string) int {
	t.Helper()
	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("GetRows(%s): %v", sheet, err)
	}
	colIdx := int(col[0]-'A') + 1
	for i, r := range rows {
		if len(r) >= colIdx && r[colIdx-1] == value {
			return i + 1
		}
	}
	t.Fatalf("findRowContaining(%s, %s, %q): not found", sheet, col, value)
	return -1
}

// buildInputWithNiosMetrics builds a ReportInput whose NiosServerMetrics
// exercises every Resource Savings code path:
//   - 1 infra-only GM (excluded from totals/table)
//   - 1 physical workload member (IB-4030 / Physical) — physicalDecommission
//   - 1 X6 VMware member (IB-V2326 / VMware) — normal NIOS-X path
//   - 1 X5 AWS member (IB-V2225 / AWS) — exercises 3-variant path
//   - 1 unknown model (IB-NOTREAL / AWS) — lookupMissing
//   - 1 VMware-only model on Azure (IB-V2215 / Azure) — invalidPlatformForModel
//     (Azure is used here instead of AWS because the AWS legacy alias now
//     resolves IB-V2215 → IB-V2225 on AWS via lookupAwsLegacy.)
func buildInputWithNiosMetrics(t *testing.T) exporter.ReportInput {
	t.Helper()
	findings := []calculator.FindingRow{
		{Provider: "nios", Source: "grid01", Category: calculator.CategoryDDIObjects, Item: "dns_zone", Count: 1000, TokensPerUnit: calculator.NIOSTokensPerDDIObject, ManagementTokens: 20},
	}
	in := testInput(findings, nil)
	in.NiosServerMetrics = []exporter.NiosServerMetricFull{
		{MemberID: "gm", MemberName: "gm.grid.local", Role: "GM", Model: "IB-4030", Platform: "Physical"},
		{MemberID: "phys", MemberName: "phys.grid.local", Role: "Member", Model: "IB-4030", Platform: "Physical",
			QPS: 1000, LPS: 50, ObjectCount: 2000, ActiveIPCount: 500, ManagedIPCount: 300, StaticHosts: 50, DynamicHosts: 100, DHCPUtilization: 300},
		{MemberID: "vmw", MemberName: "vmw.grid.local", Role: "Member", Model: "IB-V2326", Platform: "VMware",
			QPS: 2000, LPS: 75, ObjectCount: 1500, ActiveIPCount: 800, ManagedIPCount: 400, StaticHosts: 80, DynamicHosts: 150, DHCPUtilization: 400},
		{MemberID: "aws", MemberName: "aws.grid.local", Role: "Member", Model: "IB-V2225", Platform: "AWS",
			QPS: 3000, LPS: 100, ObjectCount: 2500, ActiveIPCount: 1200, ManagedIPCount: 600, StaticHosts: 120, DynamicHosts: 200, DHCPUtilization: 500},
		{MemberID: "unk", MemberName: "unk.grid.local", Role: "Member", Model: "IB-NOTREAL", Platform: "AWS",
			QPS: 500, LPS: 25, ObjectCount: 1000, ActiveIPCount: 400, ManagedIPCount: 200, StaticHosts: 40, DynamicHosts: 80, DHCPUtilization: 200},
		{MemberID: "bad", MemberName: "bad.grid.local", Role: "Member", Model: "IB-V2215", Platform: "Azure",
			QPS: 600, LPS: 30, ObjectCount: 1100, ActiveIPCount: 450, ManagedIPCount: 220, StaticHosts: 45, DynamicHosts: 85, DHCPUtilization: 250},
	}
	return in
}

// TestBuild_ResourceSavingsSheetCreated asserts that when NIOS server metrics are
// present, the Resource Savings sheet is added to the workbook in the right
// position (immediately after "Token Calculation" — see D-08).
func TestBuild_ResourceSavingsSheetCreated(t *testing.T) {
	f := buildAndOpen(t, buildInputWithNiosMetrics(t), nil)
	if !sheetExists(f, "Resource Savings") {
		t.Fatalf("expected sheet %q to exist; got: %v", "Resource Savings", f.GetSheetList())
	}
	sheets := f.GetSheetList()
	rsIdx := indexOf(sheets, "Resource Savings")
	tcIdx := indexOf(sheets, "Token Calculation")
	scIdx := indexOf(sheets, "NIOS Migration Scenarios")
	if rsIdx <= tcIdx {
		t.Errorf("Resource Savings (%d) must come after Token Calculation (%d)", rsIdx, tcIdx)
	}
	if scIdx >= 0 && rsIdx >= scIdx {
		t.Errorf("Resource Savings (%d) must come before NIOS Migration Scenarios (%d)", rsIdx, scIdx)
	}
}

// TestBuild_ResourceSavingsDetailTableHeaders verifies D-11 column ordering.
func TestBuild_ResourceSavingsDetailTableHeaders(t *testing.T) {
	f := buildAndOpen(t, buildInputWithNiosMetrics(t), nil)
	headerRow := findRowContaining(t, f, "Resource Savings", "A", "Member Name")
	wantHeaders := []string{
		"Member Name", "Role", "QPS", "LPS",
		"Old Model", "Old Platform", "Old Variant",
		"Old vCPU", "Old RAM", "Target", "New Tier", "New vCPU", "New RAM",
		"Δ vCPU", "Δ RAM", "Notes",
	}
	for i, want := range wantHeaders {
		colName, _ := excelize.ColumnNumberToName(i + 1)
		cell := colName + strconv.Itoa(headerRow)
		got, _ := f.GetCellValue("Resource Savings", cell)
		if got != want {
			t.Errorf("header col %d (%s%d): want %q got %q", i+1, colName, headerRow, want, got)
		}
	}
}

// TestBuild_ResourceSavingsXaasMember asserts the literal Notes string for XaaS.
//
// Limitation acknowledged in resource_savings.go package doc: the exporter
// hardcodes formFactor="nios-x" for every NIOS member because the wizard's
// per-member form-factor map isn't yet plumbed through the export request.
// To still cover the FullyManaged code path inside the writer helper this test
// uses a calculator.MemberSavings with FullyManaged=true via the documented
// "nios-xaas" form factor on a known appliance — call CalcMemberSavings
// directly. The full end-to-end XaaS render through Build() is gated on the
// follow-up plan that plumbs the form-factor map.
func TestBuild_ResourceSavingsXaasMember(t *testing.T) {
	in := calculator.MemberSavingsInput{
		MemberID:   "xaas-test",
		MemberName: "xaas.grid.local",
		Model:      "IB-V2326",
		Platform:   calculator.PlatformVMware,
	}
	s := calculator.CalcMemberSavings(in, "2XS", "nios-xaas", -1)
	if !s.FullyManaged {
		t.Fatalf("CalcMemberSavings did not flag FullyManaged for nios-xaas target")
	}
	if s.NewVCPU != 0 || s.NewRamGB != 0 {
		t.Errorf("XaaS target should yield 0/0; got %d vCPU / %g GB", s.NewVCPU, s.NewRamGB)
	}
	if s.DeltaVCPU != -s.OldVCPU {
		t.Errorf("DeltaVCPU %d, want %d", s.DeltaVCPU, -s.OldVCPU)
	}
}

// TestBuild_ResourceSavingsExcludedMember asserts the warning text for unknowns.
func TestBuild_ResourceSavingsExcludedMember(t *testing.T) {
	f := buildAndOpen(t, buildInputWithNiosMetrics(t), nil)

	memberRow := findRowContaining(t, f, "Resource Savings", "A", "unk.grid.local")
	colName, _ := excelize.ColumnNumberToName(16) // Notes column (shifted +2 for QPS/LPS)
	notes, _ := f.GetCellValue("Resource Savings", colName+strconv.Itoa(memberRow))
	if notes != "Unknown model — verify member configuration" {
		t.Errorf("unk member Notes = %q, want 'Unknown model — verify member configuration'", notes)
	}

	// VMware-only model on AWS should yield the invalid combination warning.
	badRow := findRowContaining(t, f, "Resource Savings", "A", "bad.grid.local")
	badNotes, _ := f.GetCellValue("Resource Savings", colName+strconv.Itoa(badRow))
	if !strings.Contains(badNotes, "is not supported on Azure") {
		t.Errorf("bad member Notes = %q, want to contain 'is not supported on Azure'", badNotes)
	}
	if !strings.Contains(badNotes, "VMware-only") {
		t.Errorf("bad member Notes = %q, want to contain 'VMware-only'", badNotes)
	}
}

// TestBuild_ResourceSavingsVariantOverride asserts that a per-member variant
// override flows through Build() and is reflected in the Old Variant + Old vCPU
// cells of that member's row.
//
// IB-V2225 on AWS has 3 variants: r6i (default, 8 vCPU), m5 (8 vCPU, 32 GB),
// r4 (8 vCPU, 61 GB). All three have the same vCPU so we assert the variant
// CONFIG NAME and the RAM value (which differs).
func TestBuild_ResourceSavingsVariantOverride(t *testing.T) {
	overrides := map[string]int{"aws": 1} // m5 variant for the AWS member
	f := buildAndOpen(t, buildInputWithNiosMetrics(t), overrides)

	memberRow := findRowContaining(t, f, "Resource Savings", "A", "aws.grid.local")
	variantCol, _ := excelize.ColumnNumberToName(7) // Old Variant (shifted +2 for QPS/LPS)
	ramCol, _ := excelize.ColumnNumberToName(9)     // Old RAM (shifted +2 for QPS/LPS)

	gotVariant, _ := f.GetCellValue("Resource Savings", variantCol+strconv.Itoa(memberRow))
	if gotVariant != "m5" {
		t.Errorf("aws member Old Variant = %q, want 'm5'", gotVariant)
	}
	gotRam, _ := f.GetCellValue("Resource Savings", ramCol+strconv.Itoa(memberRow))
	if gotRam != "32" {
		t.Errorf("aws member Old RAM = %q, want '32' (m5 variant)", gotRam)
	}
}

// TestBuild_ResourceSavingsEmptyFleet asserts an input with an empty NIOS
// metrics slice still produces a valid sheet (no NaN, no panic) with the
// summary block reading "Total members analyzed: 0".
func TestBuild_ResourceSavingsEmptyFleet(t *testing.T) {
	in := testInput(awsFindings(), nil)
	in.NiosServerMetrics = []exporter.NiosServerMetricFull{}

	f := buildAndOpen(t, in, nil)
	if !sheetExists(f, "Resource Savings") {
		t.Fatalf("expected Resource Savings sheet for empty fleet; got: %v", f.GetSheetList())
	}

	// Find the "Total members analyzed:" row and verify column B reads "0".
	row := findRowContaining(t, f, "Resource Savings", "A", "Total members analyzed:")
	got, _ := f.GetCellValue("Resource Savings", "B"+strconv.Itoa(row))
	if got != "0" {
		t.Errorf("Total members analyzed = %q, want '0'", got)
	}
}

func TestBuild_NiosMicrosoftServersSheet(t *testing.T) {
	in := testInput(awsFindings(), nil)
	in.NiosMicrosoftServers = &exporter.NiosMicrosoftServers{
		Servers: []exporter.NiosMicrosoftServer{{
			FQDN: "dc01.contoso.local", Address: "10.0.0.11", OS: "Windows Server 2019",
			ADDomain: "contoso.local", DNSManaged: true, ReadOnly: true,
		}},
		ManagedZones: 3,
	}
	f := openResult(t, in)
	if !sheetExists(f, "NIOS Microsoft Servers") {
		t.Fatalf("expected 'NIOS Microsoft Servers' sheet; got: %v", f.GetSheetList())
	}
	// Cell-content checks: server row (FQDN + DNS-managed yesNo mapping) and the zone total row.
	if v, _ := f.GetCellValue("NIOS Microsoft Servers", "A2"); v != "dc01.contoso.local" {
		t.Errorf("A2 (server FQDN) = %q, want dc01.contoso.local", v)
	}
	if v, _ := f.GetCellValue("NIOS Microsoft Servers", "E2"); v != "Yes" {
		t.Errorf("E2 (DNS Managed) = %q, want Yes", v)
	}
	if v, _ := f.GetCellValue("NIOS Microsoft Servers", "B4"); v != "3" {
		t.Errorf("B4 (MS-managed DNS zones total) = %q, want 3", v)
	}
}

// The screen applies a growth buffer to the grand total. If the Summary sheet
// recomputes from findings it silently drops that buffer, and the workbook
// disagrees with the report the user just looked at.
func TestBuild_SummaryUsesSuppliedTotalsNotRecomputed(t *testing.T) {
	in := testInput(awsFindings(), nil)
	// Deliberately inconsistent with Findings (whose calculator.Calculate
	// output testInput would otherwise have used).
	in.Totals = exporter.TokenTotals{DDITokens: 40, IPTokens: 41, AssetTokens: 42, GrandTotal: 999}
	in.ProviderTotals = map[string]int{"aws": 777}

	f := openResult(t, in)

	for cell, want := range map[string]string{
		"B2": "999", "B3": "40", "B4": "41", "B5": "42",
	} {
		got, err := f.GetCellValue("Summary", cell)
		if err != nil {
			t.Fatalf("GetCellValue(%s): %v", cell, err)
		}
		if got != want {
			t.Errorf("Summary!%s = %q, want %q", cell, got, want)
		}
	}
	if got, _ := f.GetCellValue("Summary", "B10"); got != "777" {
		t.Errorf("Summary!B10 (aws subtotal) = %q, want %q", got, "777")
	}
}

func TestBuild_NoNiosMicrosoftServersSheetWhenEmpty(t *testing.T) {
	f := openResult(t, testInput(awsFindings(), nil))
	if sheetExists(f, "NIOS Microsoft Servers") {
		t.Errorf("did not expect 'NIOS Microsoft Servers' sheet when JSON empty; got: %v", f.GetSheetList())
	}
}
