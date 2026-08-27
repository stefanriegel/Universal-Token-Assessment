package exporter_test

import (
	"strings"
	"testing"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/exporter"
)

// msAllocationFixture returns a fully populated exporter.MicrosoftAllocation
// with four scenarios carrying distinct counts/tokens, a two-entry held-back
// breakdown using real service/family/reason values, and non-zero evidence
// counts. All numeric values stay below 1000 so GetCellValue's #,##0 number
// format never inserts a thousands separator into the expected strings.
func msAllocationFixture() *exporter.MicrosoftAllocation {
	return &exporter.MicrosoftAllocation{
		Diagnostic:     exporter.MSAllocationDiagnostic{Available: true},
		BaselineTokens: 56,
		Scenarios: []exporter.MSAllocationScenario{
			{
				ID: "none", DNSEnabled: false, DHCPEnabled: false,
				Categories: [3]exporter.MSCategoryTokens{
					{NIOSCount: 800, NativeCount: 0, Tokens: 32},
					{NIOSCount: 400, NativeCount: 0, Tokens: 16},
					{Tokens: 8},
				},
				EffectiveTokens: 56, DeltaTokens: 0,
			},
			{
				ID: "dns-only", DNSEnabled: true, DHCPEnabled: false,
				Categories: [3]exporter.MSCategoryTokens{
					{NIOSCount: 700, NativeCount: 100, Tokens: 40},
					{NIOSCount: 400, NativeCount: 0, Tokens: 16},
					{Tokens: 9},
				},
				EffectiveTokens: 65, DeltaTokens: 9,
			},
			{
				ID: "dhcp-only", DNSEnabled: false, DHCPEnabled: true,
				Categories: [3]exporter.MSCategoryTokens{
					{NIOSCount: 800, NativeCount: 0, Tokens: 32},
					{NIOSCount: 300, NativeCount: 100, Tokens: 20},
					{Tokens: 11},
				},
				EffectiveTokens: 63, DeltaTokens: 7,
			},
			{
				ID: "both", DNSEnabled: true, DHCPEnabled: true,
				Categories: [3]exporter.MSCategoryTokens{
					{NIOSCount: 700, NativeCount: 100, Tokens: 40},
					{NIOSCount: 300, NativeCount: 100, Tokens: 20},
					{Tokens: 12},
				},
				EffectiveTokens: 72, DeltaTokens: 16,
			},
		},
		Evidence: exporter.MSEvidenceCounts{
			RelationshipRows:          42,
			DuplicateRelationshipRows: 7,
			RelationshipAnomalies:     2,
		},
	}
}

func TestBuildMicrosoftAllocationSheet(t *testing.T) {
	in := testInput(awsFindings(), nil)
	in.MicrosoftAllocation = msAllocationFixture()
	in.SelectedMSScenario = "dhcp-only"

	f := openResult(t, in)

	if !sheetExists(f, "Microsoft Allocation") {
		t.Fatalf("expected 'Microsoft Allocation' sheet; got: %v", f.GetSheetList())
	}

	rows, err := f.GetRows("Microsoft Allocation")
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}

	wantHeaders := []string{
		"Scenario", "Manage MS DNS", "Manage MS DHCP",
		"DDI Objects (NIOS)", "DDI Objects (Universal DDI)", "DDI Object Tokens",
		"Active IPs (NIOS)", "Active IPs (Universal DDI)", "Active IP Tokens",
		"Managed Asset Tokens", "Effective Tokens",
		"Additional Tokens vs all-NIOS", "Selected",
	}
	if len(rows) < 1 || len(rows[0]) < len(wantHeaders) {
		t.Fatalf("header row too short: %v", rows)
	}
	for i, want := range wantHeaders {
		if rows[0][i] != want {
			t.Errorf("header[%d] = %q, want %q", i, rows[0][i], want)
		}
	}

	wantScenarioLabels := []string{
		"All NIOS (baseline)",
		"Microsoft DNS on Universal DDI",
		"Microsoft DHCP on Universal DDI",
		"Microsoft DNS + DHCP on Universal DDI",
	}
	dataRows := rows[1:5]
	if len(dataRows) != 4 {
		t.Fatalf("expected exactly 4 scenario data rows, got %d: %v", len(dataRows), dataRows)
	}

	selectedCount := 0
	var selectedRow []string
	for i, r := range dataRows {
		if r[0] != wantScenarioLabels[i] {
			t.Errorf("scenario row %d Scenario cell = %q, want %q", i, r[0], wantScenarioLabels[i])
		}
		if len(r) > 12 && r[12] == "Selected" {
			selectedCount++
			selectedRow = r
		}
		if len(r) > 11 && strings.HasPrefix(r[11], "-") {
			t.Errorf("scenario row %d Additional Tokens cell = %q, must never carry a minus sign", i, r[11])
		}
	}
	if selectedCount != 1 {
		t.Fatalf("expected exactly one row marked Selected, got %d", selectedCount)
	}
	if selectedRow[0] != "Microsoft DHCP on Universal DDI" {
		t.Errorf("Selected marker landed on %q, want the dhcp-only row", selectedRow[0])
	}
	if selectedRow[10] != "63" {
		t.Errorf("Effective Tokens on selected row = %q, want 63", selectedRow[10])
	}
	if selectedRow[11] != "7" {
		t.Errorf("Additional Tokens vs all-NIOS on selected row = %q, want 7", selectedRow[11])
	}

	findRow := func(label string) []string {
		for _, r := range rows {
			if len(r) > 0 && r[0] == label {
				return r
			}
		}
		return nil
	}

	if baseline := findRow("All-NIOS Baseline Tokens"); baseline == nil || len(baseline) < 2 || baseline[1] != "56" {
		t.Errorf("baseline row = %v, want [\"All-NIOS Baseline Tokens\", \"56\"]", baseline)
	}

	// The held-back-for-review disclosure was removed: none of it may reappear.
	if r := findRow("Held-Back for Review"); r != nil {
		t.Errorf("held-back section row = %v, want none", r)
	}

	if r := findRow("Relationship rows observed"); r == nil || len(r) < 2 || r[1] != "42" {
		t.Errorf("evidence row 'Relationship rows observed' = %v, want value 42", r)
	}
	if r := findRow("Duplicate relationship rows"); r == nil || len(r) < 2 || r[1] != "7" {
		t.Errorf("evidence row 'Duplicate relationship rows' = %v, want value 7", r)
	}
	if r := findRow("Relationship anomalies"); r == nil || len(r) < 2 || r[1] != "2" {
		t.Errorf("evidence row 'Relationship anomalies' = %v, want value 2", r)
	}

	// Privacy sweep (T-07-11): plant a marker no fixture field contains, then
	// walk every cell asserting neither the marker nor an IP-looking value
	// reached the sheet.
	const marker = "zzz-ms-export-privacy-marker"
	const ipLike = "192.0.2.77"
	for _, r := range rows {
		for _, cell := range r {
			if strings.Contains(cell, marker) {
				t.Errorf("cell %q contains planted marker %q", cell, marker)
			}
			if strings.Contains(cell, ipLike) {
				t.Errorf("cell %q contains IP-looking value %q", cell, ipLike)
			}
		}
	}
}

// TestBuildMicrosoftAllocationSheetUnavailable covers the D-08 "unavailable"
// state: the consistency gate failed, so the ledger produced no scenarios, but
// MicrosoftAllocation is still non-nil and BaselineTokens is still valid.
//
// This is the third of the three states that must stay distinct (available /
// absent / unavailable) and the only one with no scenario rows to render. The
// nil case is TestBuildMicrosoftAllocationSheetOmitted; the populated case is
// TestBuildMicrosoftAllocationSheet.
func TestBuildMicrosoftAllocationSheetUnavailable(t *testing.T) {
	in := testInput(awsFindings(), nil)
	in.MicrosoftAllocation = &exporter.MicrosoftAllocation{
		Diagnostic: exporter.MSAllocationDiagnostic{
			Available: false,
			Code:      "MS_ALLOCATION_UNAVAILABLE",
			Message:   "The Microsoft allocation snapshot is unavailable for this scan. The baseline NIOS scan results remain valid and usable.",
		},
		BaselineTokens: 56,
		Scenarios:      nil,
		Evidence: exporter.MSEvidenceCounts{
			RelationshipRows:          42,
			DuplicateRelationshipRows: 7,
			RelationshipAnomalies:     2,
		},
	}
	// A stale selection must not resurrect a row that does not exist.
	in.SelectedMSScenario = "dhcp-only"

	f := openResult(t, in)

	if !sheetExists(f, "Microsoft Allocation") {
		t.Fatalf("expected 'Microsoft Allocation' sheet even when unavailable; got: %v", f.GetSheetList())
	}

	rows, err := f.GetRows("Microsoft Allocation")
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	if len(rows) == 0 || len(rows[0]) == 0 || rows[0][0] != "Scenario" {
		t.Fatalf("expected the header row to still render; got: %v", rows)
	}

	// Zero scenario data rows: none of the four labels may appear anywhere.
	for _, label := range []string{
		"All NIOS (baseline)",
		"Microsoft DNS on Universal DDI",
		"Microsoft DHCP on Universal DDI",
		"Microsoft DNS + DHCP on Universal DDI",
	} {
		for r, row := range rows {
			for c, cell := range row {
				if cell == label {
					t.Errorf("scenario label %q rendered at row %d col %d despite empty Scenarios", label, r+1, c+1)
				}
			}
		}
	}

	// No row may be marked Selected when there is nothing to select.
	for r, row := range rows {
		for c, cell := range row {
			if cell == "Selected" && r > 0 {
				t.Errorf("found a 'Selected' marker at row %d col %d with no scenarios", r+1, c+1)
			}
		}
	}

	// The baseline stays valid and usable — that is the whole point of D-08.
	var sawBaseline bool
	for _, row := range rows {
		if len(row) >= 2 && row[0] == "All-NIOS Baseline Tokens" {
			sawBaseline = true
			if row[1] != "56" {
				t.Errorf("baseline = %q, want %q", row[1], "56")
			}
		}
	}
	if !sawBaseline {
		t.Errorf("expected the All-NIOS baseline row to render when allocation is unavailable; got: %v", rows)
	}

	// The internal reason code must never reach a cell (privacy + D-08).
	for r, row := range rows {
		for c, cell := range row {
			if strings.Contains(cell, "MS_ALLOCATION_UNAVAILABLE") {
				t.Errorf("diagnostic code leaked into row %d col %d: %q", r+1, c+1, cell)
			}
		}
	}
}

func TestBuildMicrosoftAllocationSheetOmitted(t *testing.T) {
	in := testInput(awsFindings(), nil)
	// in.MicrosoftAllocation left nil — no allocation snapshot on this payload.
	f := openResult(t, in)
	if sheetExists(f, "Microsoft Allocation") {
		t.Errorf("did not expect 'Microsoft Allocation' sheet when MicrosoftAllocation is nil; got: %v", f.GetSheetList())
	}
}
