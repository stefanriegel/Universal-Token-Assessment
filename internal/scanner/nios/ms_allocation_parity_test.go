// Package nios_test — cross-surface parity harness for the Microsoft
// allocation diagnostic (D-01..D-16, phase 08-01). Every hop below decodes
// its own snapshot into msParityWireShape and compares it against the same
// checked-in golden — never hop-to-hop — so a bug that only shows up on one
// surface (e.g. the exporter's independently-declared MicrosoftAllocation
// type drifting from the scanner's) fails at the surface that introduced it.
package nios_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/calculator"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/exporter"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/orchestrator"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/scanner"
	niosscanner "github.com/stefanriegel/Universal-Token-Assessment/internal/scanner/nios"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/session"
	"github.com/stefanriegel/Universal-Token-Assessment/server"
	"github.com/xuri/excelize/v2"
)

// updateMSAllocationGolden regenerates the checked-in testdata/ms-allocation/*.json
// goldens from a real Scan(). Run once per case, inspect the diff before
// committing — never rebaseline a failing assertion without reading it.
var updateMSAllocationGolden = flag.Bool("update", false, "regenerate the checked-in MS allocation parity goldens")

// msParityCase names one synthetic onedb.xml fixture, its checked-in golden,
// and the SelectedMSScenario values the export/workbook hops must be driven
// through for it.
//
// snapshotOnly marks a case whose scanner hop cannot be reproduced by driving
// xml through a real Scan() here — currently only the "unavailable" case,
// because msLedgerState.build's conservation gate cannot be broken by a
// well-formed onedb.xml (see 08-02-PLAN.md's architectural_note). For such a
// case, xml is left empty and the checked-in golden itself stands in for the
// scanner hop; the case's own comment in msParityCases names the generator
// test file that proves the golden was actually derived from the source
// constant/derivation layer, not hand-authored. TestMSAllocation_Parity
// asserts that file exists on disk so this escape hatch can never silently
// skip a hop.
type msParityCase struct {
	name         string
	xml          string
	json         string
	selections   []string
	snapshotOnly bool
}

// msParityCases is the shared fixture table every task in this plan appends
// to; Task 1 seeds it with the single "both" case.
var msParityCases = []msParityCase{
	{
		name:       "both",
		xml:        "../../../testdata/ms-allocation/both.xml",
		json:       "../../../testdata/ms-allocation/both.json",
		selections: []string{"both"},
	},
	{
		name:       "dns-only",
		xml:        "../../../testdata/ms-allocation/dns-only.xml",
		json:       "../../../testdata/ms-allocation/dns-only.json",
		selections: []string{"both"},
	},
	{
		name:       "dhcp-only",
		xml:        "../../../testdata/ms-allocation/dhcp-only.xml",
		json:       "../../../testdata/ms-allocation/dhcp-only.json",
		selections: []string{"both"},
	},
	{
		name:       "held-back",
		xml:        "../../../testdata/ms-allocation/held-back.xml",
		json:       "../../../testdata/ms-allocation/held-back.json",
		selections: []string{"none", "dns-only", "dhcp-only", "both"},
	},
	{
		name:       "absent",
		xml:        "../../../testdata/ms-allocation/absent.xml",
		json:       "../../../testdata/ms-allocation/absent.json",
		selections: []string{"both"},
	},
	// unavailable is snapshot-only: see ms_allocation_unavailable_golden_test.go
	// (package nios, same directory) for how testdata/ms-allocation/unavailable.json
	// is derived from the msAllocationUnavailableMessage constant via
	// DeriveMSAllocationScenarios, rather than from a real Scan() of xml.
	{
		name:         "unavailable",
		json:         "../../../testdata/ms-allocation/unavailable.json",
		selections:   []string{"both"},
		snapshotOnly: true,
	},
	{
		name:       "boundary-exact",
		xml:        "../../../testdata/ms-allocation/boundary-exact.xml",
		json:       "../../../testdata/ms-allocation/boundary-exact.json",
		selections: []string{"both"},
	},
	{
		name:       "boundary-plus-one",
		xml:        "../../../testdata/ms-allocation/boundary-plus-one.xml",
		json:       "../../../testdata/ms-allocation/boundary-plus-one.json",
		selections: []string{"both"},
	},
}

// msAllocationUnavailableGoldenGeneratorFile is the file the "unavailable"
// snapshot-only case's scanner hop is proven by, instead of a real Scan()
// call in this file. TestMSAllocation_Parity asserts this file exists on
// disk for every snapshotOnly case, so the escape hatch cannot stand in for
// missing coverage (T-08-16).
const msAllocationUnavailableGoldenGeneratorFile = "ms_allocation_unavailable_golden_test.go"

// msParityAssertGeneratorExists fails the test if path does not exist,
// naming the escape hatch it would otherwise silently permit.
func msParityAssertGeneratorExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("snapshot-only case requires %s to exist and prove the scanner hop independently: %v", path, err)
	}
}

// msParityRawGolden reads a checked-in golden's raw bytes, for a
// snapshot-only case where the golden itself stands in for a scanner
// snapshot rather than being decoded and re-marshalled.
func msParityRawGolden(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	return data
}

// msParityWireShape is a full-fidelity mirror of the flat JSON every hop
// produces (niosMicrosoftAllocationJSON on the scanner side, MicrosoftAllocation
// on the server/exporter side) — every field name below is copied from the
// real wire shape, not abbreviated.
type msParityWireShape struct {
	Diagnostic struct {
		Available bool   `json:"available"`
		Code      string `json:"code"`
		Message   string `json:"message"`
	} `json:"diagnostic"`
	BaselineTokens int `json:"baselineTokens"`
	Scenarios      []struct {
		ID          string `json:"id"`
		DNSEnabled  bool   `json:"dnsEnabled"`
		DHCPEnabled bool   `json:"dhcpEnabled"`
		Categories  [3]struct {
			Category          string `json:"category"`
			NIOSCount         int    `json:"niosCount"`
			NIOSRate          int    `json:"niosRate"`
			NativeCount       int    `json:"nativeCount"`
			NativeRate        int    `json:"nativeRate"`
			NIOSSubtotalNum   int    `json:"niosSubtotalNum"`
			NativeSubtotalNum int    `json:"nativeSubtotalNum"`
			SubtotalDen       int    `json:"subtotalDen"`
			Tokens            int    `json:"tokens"`
		} `json:"categories"`
		EffectiveTokens int `json:"effectiveTokens"`
		DeltaTokens     int `json:"deltaTokens"`
	} `json:"scenarios"`
	Evidence struct {
		RelationshipRows          int `json:"relationshipRows"`
		DuplicateRelationshipRows int `json:"duplicateRelationshipRows"`
		RelationshipAnomalies     int `json:"relationshipAnomalies"`
	} `json:"evidence"`
}

// msParityLoadXML reads a checked-in onedb.xml fixture, failing the test
// (never skipping) if it is missing.
func msParityLoadXML(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return string(data)
}

// msParityScanSnapshot drives xmlBody through a real Scan() and returns the
// scanner's own Microsoft allocation JSON (hop 0) plus the whole-grid finding
// rows the export hop needs.
func msParityScanSnapshot(t *testing.T, xmlBody string) ([]byte, []calculator.FindingRow) {
	t.Helper()
	path := writeMSLedgerBackup(t, xmlBody)
	s := niosscanner.New()
	rows, err := s.Scan(context.Background(), scanner.ScanRequest{
		Provider:    "nios",
		Credentials: map[string]string{"backup_path": path},
	}, func(scanner.Event) {})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	data := s.GetNiosMicrosoftAllocationJSON()
	if data == nil {
		t.Fatalf("GetNiosMicrosoftAllocationJSON() returned nil")
	}
	return data, rows
}

// msParityGolden reads and decodes a checked-in golden. Never t.Skip — a
// missing or malformed golden is a hard failure.
func msParityGolden(t *testing.T, path string) msParityWireShape {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	var shape msParityWireShape
	if err := json.Unmarshal(data, &shape); err != nil {
		t.Fatalf("decode golden %s: %v", path, err)
	}
	return shape
}

// msParityAssertSnapshotEqual compares a hop's decoded snapshot against the
// golden, field by field, reporting every mismatch under label so a failure
// names exactly which hop and which field diverged.
func msParityAssertSnapshotEqual(t *testing.T, label string, want, got msParityWireShape) {
	t.Helper()
	if got.Diagnostic != want.Diagnostic {
		t.Errorf("%s: diagnostic = %+v, want %+v", label, got.Diagnostic, want.Diagnostic)
	}
	if got.BaselineTokens != want.BaselineTokens {
		t.Errorf("%s: baselineTokens = %d, want %d", label, got.BaselineTokens, want.BaselineTokens)
	}
	if len(got.Scenarios) != len(want.Scenarios) {
		t.Fatalf("%s: len(scenarios) = %d, want %d", label, len(got.Scenarios), len(want.Scenarios))
	}
	for i := range want.Scenarios {
		wc, gc := want.Scenarios[i], got.Scenarios[i]
		prefix := fmt.Sprintf("%s: scenarios[%d] (%s)", label, i, wc.ID)
		if gc.ID != wc.ID || gc.DNSEnabled != wc.DNSEnabled || gc.DHCPEnabled != wc.DHCPEnabled ||
			gc.EffectiveTokens != wc.EffectiveTokens || gc.DeltaTokens != wc.DeltaTokens {
			t.Errorf("%s = %+v, want %+v", prefix, gc, wc)
		}
		for c := range wc.Categories {
			if gc.Categories[c] != wc.Categories[c] {
				t.Errorf("%s.categories[%d] = %+v, want %+v", prefix, c, gc.Categories[c], wc.Categories[c])
			}
		}
	}
	if got.Evidence != want.Evidence {
		t.Errorf("%s: evidence = %+v, want %+v", label, got.Evidence, want.Evidence)
	}
}

// msParityAPISnapshot carries scannerBytes through a session and the real
// GET /api/v1/scan/{id}/results handler, returning the session-carriage
// snapshot (hop 1) and the decoded MicrosoftAllocation payload the API
// actually served over HTTP (hop 2), so the caller can assert each against
// the golden under its own label.
func msParityAPISnapshot(t *testing.T, scannerBytes []byte) (sessionSnapshot, apiSnapshot msParityWireShape) {
	t.Helper()

	store := session.NewStore()
	sess := store.New()
	now := time.Now()
	sess.State = session.ScanStateComplete
	sess.CompletedAt = &now
	sess.SetNiosMicrosoftAllocationJSON(scannerBytes)

	// Hop 1: session carriage — the bytes must round-trip unchanged.
	if err := json.Unmarshal(sess.NiosMicrosoftAllocationJSON, &sessionSnapshot); err != nil {
		t.Fatalf("decode session snapshot: %v", err)
	}

	orch := orchestrator.New(nil)
	router := server.NewRouter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}), store, orch)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scan/"+sess.ID+"/results", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET .../results: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp server.ScanResultsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode ScanResultsResponse: %v", err)
	}
	if resp.MicrosoftAllocation == nil {
		t.Fatalf("ScanResultsResponse.MicrosoftAllocation is nil")
	}

	data, err := json.Marshal(resp.MicrosoftAllocation)
	if err != nil {
		t.Fatalf("re-marshal MicrosoftAllocation: %v", err)
	}
	if err := json.Unmarshal(data, &apiSnapshot); err != nil {
		t.Fatalf("decode MicrosoftAllocation into wire shape: %v", err)
	}
	return sessionSnapshot, apiSnapshot
}

// msParityWorkbookRows builds the real .xlsx via exporter.Build and returns
// the "Microsoft Allocation" sheet's cell values (hop 4).
func msParityWorkbookRows(t *testing.T, in exporter.ReportInput) [][]string {
	t.Helper()
	var buf bytes.Buffer
	if err := exporter.Build(&buf, in); err != nil {
		t.Fatalf("exporter.Build: %v", err)
	}
	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatalf("excelize.OpenReader: %v", err)
	}
	rows, err := f.GetRows("Microsoft Allocation")
	if err != nil {
		t.Fatalf("GetRows(Microsoft Allocation): %v", err)
	}
	return rows
}

// msParityWorkbookHeaders is copied verbatim from
// internal/exporter/ms_allocation_sheet.go's header row so a header-order
// regression there fails this test rather than silently drifting.
var msParityWorkbookHeaders = []string{
	"Scenario", "Manage MS DNS", "Manage MS DHCP",
	"DDI Objects (NIOS)", "DDI Objects (Universal DDI)", "DDI Object Tokens",
	"Active IPs (NIOS)", "Active IPs (Universal DDI)", "Active IP Tokens",
	"Managed Asset Tokens", "Effective Tokens",
	"Additional Tokens vs all-NIOS", "Selected",
}

// msParityScenarioLabels mirrors msScenarioLabel in ms_allocation_sheet.go —
// unexported there, so re-declared here rather than imported.
var msParityScenarioLabels = map[string]string{
	"none":      "All NIOS (baseline)",
	"dns-only":  "Microsoft DNS on Universal DDI",
	"dhcp-only": "Microsoft DHCP on Universal DDI",
	"both":      "Microsoft DNS + DHCP on Universal DDI",
}

// msParityFindRow returns the first row whose first cell equals label, or
// nil if none matches.
func msParityFindRow(rows [][]string, label string) []string {
	for _, r := range rows {
		if len(r) > 0 && r[0] == label {
			return r
		}
	}
	return nil
}

// msParityAssertWorkbookRows walks the "Microsoft Allocation" sheet's cells
// and asserts every one against the golden — header labels, the four
// scenario data rows (including which one carries "Selected" for sel), the
// baseline row, the absence of any held-back block
// requires it appear in the golden's exact array order), and the evidence
// rows.
//
// D-07 limitation: buildMicrosoftAllocationSheet never writes
// golden.Diagnostic.Message anywhere on the sheet — that text is an
// API/session-surface-only field (D-01..D-16's workbook block never had a
// diagnostic-message cell to begin with). So this function does not, and
// cannot, assert message parity on the workbook hop; message parity for the
// "unavailable" case is instead proven at the scanner/session/API hops by
// msParityAssertSnapshotEqual, and independently by
// TestMSAllocationUnavailableGolden's round trip against the source
// constant.
func msParityAssertWorkbookRows(t *testing.T, sel string, golden msParityWireShape, rows [][]string) {
	t.Helper()

	// cellAt returns "" for a column past the row's last populated cell:
	// excelize's GetRows trims trailing empty cells, so an unmarked
	// "Selected" column legitimately makes a row shorter than the header.
	cellAt := func(r []string, i int) string {
		if i >= len(r) {
			return ""
		}
		return r[i]
	}

	if len(rows) == 0 || len(rows[0]) < len(msParityWorkbookHeaders) {
		t.Fatalf("workbook header row too short: %v", rows)
	}
	for i, want := range msParityWorkbookHeaders {
		if rows[0][i] != want {
			t.Errorf("workbook header[%d] = %q, want %q", i, rows[0][i], want)
		}
	}

	if len(rows) < 1+len(golden.Scenarios) {
		t.Fatalf("expected %d scenario data rows, got %d: %v", len(golden.Scenarios), len(rows)-1, rows)
	}
	selectedCount := 0
	for i, sc := range golden.Scenarios {
		r := rows[1+i]
		if cellAt(r, 0) != msParityScenarioLabels[sc.ID] {
			t.Errorf("workbook scenario row %d label = %q, want %q", i, cellAt(r, 0), msParityScenarioLabels[sc.ID])
		}
		ddi, ip, asset := sc.Categories[0], sc.Categories[1], sc.Categories[2]
		wantNums := []string{
			fmt.Sprint(ddi.NIOSCount), fmt.Sprint(ddi.NativeCount), fmt.Sprint(ddi.Tokens),
			fmt.Sprint(ip.NIOSCount), fmt.Sprint(ip.NativeCount), fmt.Sprint(ip.Tokens),
			fmt.Sprint(asset.Tokens), fmt.Sprint(sc.EffectiveTokens), fmt.Sprint(sc.DeltaTokens),
		}
		for c, want := range wantNums {
			if got := cellAt(r, c+3); got != want {
				t.Errorf("workbook scenario row %d cell %d = %q, want %q", i, c+3, got, want)
			}
		}
		if sc.ID == sel {
			if got := cellAt(r, 12); got != "Selected" {
				t.Errorf("workbook scenario row %d (selection %q) Selected cell = %q, want %q", i, sel, got, "Selected")
			}
			selectedCount++
		} else if cellAt(r, 12) == "Selected" {
			t.Errorf("workbook scenario row %d unexpectedly marked Selected (selection was %q)", i, sel)
		}
	}
	// The unavailable diagnostic reports zero scenarios (D-07): no row can
	// carry "Selected" when there is nothing to select, so only enforce
	// exactly-one when the golden actually has scenarios to mark.
	if len(golden.Scenarios) > 0 && selectedCount != 1 {
		t.Errorf("workbook marked %d rows Selected for selection %q, want exactly 1", selectedCount, sel)
	}
	// No fabricated number: when the golden carries zero scenarios, the
	// sheet must not fill in a phantom "All NIOS (baseline)"/etc. scenario
	// row with invented zeroes — every one of buildMicrosoftAllocationSheet's
	// scenario rows comes from ranging over ma.Scenarios, so an empty golden
	// must produce zero rows bearing any known scenario label.
	if len(golden.Scenarios) == 0 {
		for _, r := range rows {
			for _, label := range msParityScenarioLabels {
				if len(r) > 0 && r[0] == label {
					t.Errorf("workbook has scenario row %q but golden.Scenarios is empty", label)
				}
			}
		}
	}

	if baseline := msParityFindRow(rows, "All-NIOS Baseline Tokens"); baseline == nil || len(baseline) < 2 || baseline[1] != fmt.Sprint(golden.BaselineTokens) {
		t.Errorf("workbook baseline row = %v, want [All-NIOS Baseline Tokens, %d]", baseline, golden.BaselineTokens)
	}

	// The held-back-for-review disclosure was removed from every surface;
	// its section header must not reappear in the workbook.
	if r := msParityFindRow(rows, "Held-Back for Review"); r != nil {
		t.Errorf("workbook held-back section = %v, want none", r)
	}
	if r := msParityFindRow(rows, "Relationship rows observed"); r == nil || len(r) < 2 || r[1] != fmt.Sprint(golden.Evidence.RelationshipRows) {
		t.Errorf("workbook 'Relationship rows observed' = %v, want %d", r, golden.Evidence.RelationshipRows)
	}
	if r := msParityFindRow(rows, "Duplicate relationship rows"); r == nil || len(r) < 2 || r[1] != fmt.Sprint(golden.Evidence.DuplicateRelationshipRows) {
		t.Errorf("workbook 'Duplicate relationship rows' = %v, want %d", r, golden.Evidence.DuplicateRelationshipRows)
	}
	if r := msParityFindRow(rows, "Relationship anomalies"); r == nil || len(r) < 2 || r[1] != fmt.Sprint(golden.Evidence.RelationshipAnomalies) {
		t.Errorf("workbook 'Relationship anomalies' = %v, want %d", r, golden.Evidence.RelationshipAnomalies)
	}
}

// TestMSAllocation_Parity drives every msParityCases entry through all Go
// hops — scanner, session carriage, the real results API, the export
// payload, and the workbook — asserting each hop's decoded snapshot against
// the same checked-in golden.
func TestMSAllocation_Parity(t *testing.T) {
	for _, tc := range msParityCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var scannerBytes []byte
			var rows []calculator.FindingRow

			if tc.snapshotOnly {
				// The scanner hop is proven independently by the generator
				// test named below; this harness only threads the
				// resulting golden through the remaining hops. rows stays
				// nil: the Microsoft Allocation sheet reads only ma, never
				// findings, so no calculator rows are needed here.
				msParityAssertGeneratorExists(t, msAllocationUnavailableGoldenGeneratorFile)
				scannerBytes = msParityRawGolden(t, tc.json)
				if *updateMSAllocationGolden {
					return // golden is (re)generated by the file above, not here
				}
			} else {
				xmlBody := msParityLoadXML(t, tc.xml)
				scannerBytes, rows = msParityScanSnapshot(t, xmlBody)

				if *updateMSAllocationGolden {
					var buf bytes.Buffer
					if err := json.Indent(&buf, scannerBytes, "", "  "); err != nil {
						t.Fatalf("json.Indent: %v", err)
					}
					if err := os.WriteFile(tc.json, append(buf.Bytes(), '\n'), 0o644); err != nil {
						t.Fatalf("write golden %s: %v", tc.json, err)
					}
					return
				}
			}

			golden := msParityGolden(t, tc.json)

			// Hop 0: scanner.
			var gotScan msParityWireShape
			if err := json.Unmarshal(scannerBytes, &gotScan); err != nil {
				t.Fatalf("decode scanner snapshot: %v", err)
			}
			msParityAssertSnapshotEqual(t, "scanner", golden, gotScan)

			// Hop 1: session carriage. Hop 2: the real results API.
			gotSession, gotAPI := msParityAPISnapshot(t, scannerBytes)
			msParityAssertSnapshotEqual(t, "session carriage", golden, gotSession)
			msParityAssertSnapshotEqual(t, "API response", golden, gotAPI)

			// Hop 3: export payload — decode the golden into the exporter
			// package's own independently-declared MicrosoftAllocation type,
			// then re-marshal it and assert the round trip is lossless.
			goldenJSON, err := json.Marshal(golden)
			if err != nil {
				t.Fatalf("re-marshal golden: %v", err)
			}
			var exportMA exporter.MicrosoftAllocation
			if err := json.Unmarshal(goldenJSON, &exportMA); err != nil {
				t.Fatalf("decode golden into exporter.MicrosoftAllocation: %v", err)
			}

			tr := calculator.Calculate(rows)
			selections := tc.selections
			if len(selections) == 0 {
				selections = []string{"both"}
			}
			for _, sel := range selections {
				in := exporter.ReportInput{
					GeneratedAt: time.Now(),
					Findings:    rows,
					Totals: exporter.TokenTotals{
						DDITokens:   tr.DDITokens,
						IPTokens:    tr.IPTokens,
						AssetTokens: tr.AssetTokens,
						GrandTotal:  tr.GrandTotal,
					},
					MicrosoftAllocation: &exportMA,
					SelectedMSScenario:  sel,
				}

				exportBytes, err := json.Marshal(in.MicrosoftAllocation)
				if err != nil {
					t.Fatalf("marshal export payload: %v", err)
				}
				var gotExport msParityWireShape
				if err := json.Unmarshal(exportBytes, &gotExport); err != nil {
					t.Fatalf("decode export payload: %v", err)
				}
				msParityAssertSnapshotEqual(t, fmt.Sprintf("export payload (selection=%s)", sel), golden, gotExport)

				// Hop 4: workbook cells.
				workbookRows := msParityWorkbookRows(t, in)
				msParityAssertWorkbookRows(t, sel, golden, workbookRows)
			}
		})
	}
}

// msParityScenarioByID returns the scenario with the given id, failing the
// test if none matches.
func msParityScenarioByID(t *testing.T, shape msParityWireShape, id string) int {
	t.Helper()
	for i, sc := range shape.Scenarios {
		if sc.ID == id {
			return i
		}
	}
	t.Fatalf("no scenario with id %q in %+v", id, shape.Scenarios)
	return -1
}

// TestMSAllocation_Parity_Adjacency proves the DNS and DHCP switches move
// DDI Objects independently: dns-only.xml carries no DHCP objects at all and
// dhcp-only.xml carries DNS objects that are deliberately never attributed
// (D-01's "neither switch drags the other's resources" claim), so summing
// each single-service fixture's own "both" scenario DDI Objects NativeCount
// must equal both.xml's "both" scenario NativeCount exactly.
func TestMSAllocation_Parity_Adjacency(t *testing.T) {
	both := msParityGolden(t, "../../../testdata/ms-allocation/both.json")
	dnsOnly := msParityGolden(t, "../../../testdata/ms-allocation/dns-only.json")
	dhcpOnly := msParityGolden(t, "../../../testdata/ms-allocation/dhcp-only.json")

	bothDDI := both.Scenarios[msParityScenarioByID(t, both, "both")].Categories[0].NativeCount
	dnsOnlyDDI := dnsOnly.Scenarios[msParityScenarioByID(t, dnsOnly, "both")].Categories[0].NativeCount
	dhcpOnlyDDI := dhcpOnly.Scenarios[msParityScenarioByID(t, dhcpOnly, "both")].Categories[0].NativeCount

	if dnsOnlyDDI == 0 {
		t.Fatalf("dns-only DDI Objects NativeCount = 0, want > 0")
	}
	if dhcpOnlyDDI == 0 {
		t.Fatalf("dhcp-only DDI Objects NativeCount = 0, want > 0")
	}
	if bothDDI != dnsOnlyDDI+dhcpOnlyDDI {
		t.Errorf("both.nativeCount = %d, want dns-only.nativeCount(%d) + dhcp-only.nativeCount(%d) = %d",
			bothDDI, dnsOnlyDDI, dhcpOnlyDDI, dnsOnlyDDI+dhcpOnlyDDI)
	}
}

// TestMSAllocation_Parity_Distinguishable proves the "absent" and
// "unavailable" diagnostics — both ledger == nil, both Available == false —
// remain distinguishable by Code and Message alone (MSPAR-04's degraded-path
// requirement). "absent" still reports four zero-movement scenarios
// (D-14: a switch that truthfully shows no movement) with no message set;
// "unavailable" suppresses all scenarios and carries the fixed
// msAllocationUnavailableMessage. If a future change ever made these two
// diagnostics converge, every other assertion in this file would still pass
// (each golden is only ever compared against itself), so this is the one
// place that would catch it.
func TestMSAllocation_Parity_Distinguishable(t *testing.T) {
	absent := msParityGolden(t, "../../../testdata/ms-allocation/absent.json")
	unavailable := msParityGolden(t, "../../../testdata/ms-allocation/unavailable.json")

	if absent.Diagnostic.Code == unavailable.Diagnostic.Code {
		t.Errorf("absent and unavailable diagnostics share Code %q, want distinct codes", absent.Diagnostic.Code)
	}
	if absent.Diagnostic.Message != "" {
		t.Errorf("absent diagnostic message = %q, want empty (D-14 sets Code only)", absent.Diagnostic.Message)
	}
	if unavailable.Diagnostic.Message == "" {
		t.Errorf("unavailable diagnostic message is empty, want the fixed suppression message")
	}
	if len(absent.Scenarios) != 4 {
		t.Errorf("absent len(Scenarios) = %d, want 4 (a flipped switch still reports zero movement)", len(absent.Scenarios))
	}
	if len(unavailable.Scenarios) != 0 {
		t.Errorf("unavailable len(Scenarios) = %d, want 0 (suppressed)", len(unavailable.Scenarios))
	}
}

// TestMSAllocation_Parity_Boundary proves the pooled-then-ceiled DDI Object
// Tokens formula steps by exactly one token when the pooled numerator
// crosses a multiple of the category's LCM denominator (T-08-17):
// boundary-exact.xml is sized so numerator == den exactly (tokens == 1);
// boundary-plus-one.xml adds one more attributable object so numerator
// exceeds den by one unit (tokens == 2). Both fixtures are all-attributable
// both-services scenarios, so Categories[0] (DDI Objects) is read directly
// off the "both" scenario in each golden — no recomputation, per this file's
// own prohibition on re-deriving numbers the scanner already produced.
func TestMSAllocation_Parity_Boundary(t *testing.T) {
	exact := msParityGolden(t, "../../../testdata/ms-allocation/boundary-exact.json")
	plusOne := msParityGolden(t, "../../../testdata/ms-allocation/boundary-plus-one.json")

	exactTokens := exact.Scenarios[msParityScenarioByID(t, exact, "both")].Categories[0].Tokens
	plusOneTokens := plusOne.Scenarios[msParityScenarioByID(t, plusOne, "both")].Categories[0].Tokens

	if exactTokens != 1 {
		t.Errorf("boundary-exact DDI Object Tokens = %d, want 1 (numerator exactly divides the denominator)", exactTokens)
	}
	if plusOneTokens != exactTokens+1 {
		t.Errorf("boundary-plus-one DDI Object Tokens = %d, want exactly %d (one token higher than boundary-exact)", plusOneTokens, exactTokens+1)
	}
}
