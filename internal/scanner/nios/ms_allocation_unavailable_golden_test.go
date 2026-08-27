// Package nios (internal, not nios_test) — the unavailable-branch golden
// generator. This file must live in package nios rather than the external
// nios_test package because msAllocationUnavailableMessage is unexported;
// pinning the checked-in golden's diagnostic.message to that constant
// (rather than a hand-transcribed string) is the whole point of this test
// (D-07, T-08-15): a future wording change to the constant fails this test
// instead of silently diverging from testdata/ms-allocation/unavailable.json.
//
// The unavailable diagnostic cannot be produced by driving a real onedb.xml
// through Scan(): msLedgerState.build only reaches MSAllocationUnavailableCode
// when the conservation gate (Baseline != Attributable + Retained) fails, and
// every classify* method in this package credits exactly one partition per
// baseline unit by construction, so no well-formed backup can break that
// invariant on purpose (see 08-02-PLAN.md's architectural_note). Instead this
// test drives unavailable.xml through a real Scan() to obtain a genuine
// whole-grid baseline, then calls DeriveMSAllocationScenarios directly with a
// nil ledger and a caller-supplied unavailable diagnostic — exactly the "any
// other non-Available diagnostic" branch DeriveMSAllocationScenarios's own
// doc comment describes, and the same construction
// TestMSAllocation_DiagnosticBranching's Unavailable subtest already uses at
// the derivation-layer level. Phase 8's job is threading that same snapshot
// through session, API, export, and workbook — proven by
// ms_allocation_parity_test.go's snapshot-only "unavailable" case.
package nios

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/scanner"
)

// updateMSAllocationUnavailableGolden is named distinctly from
// ms_allocation_parity_test.go's -update flag (that file is package
// nios_test) so both flags can be registered in one `go test` invocation of
// this package without a flag-redefinition panic.
var updateMSAllocationUnavailableGolden = flag.Bool("update-unavailable-golden", false,
	"regenerate the checked-in testdata/ms-allocation/unavailable.json golden")

// writeMSAllocationUnavailableBackup is a local copy of
// ms_ledger_fixtures_test.go's writeMSLedgerBackup (package nios_test),
// which is not visible from this file's package nios. Do not promote that
// helper out of _test.go to unify the two — 08-02-PLAN.md's action text
// explicitly rejects that alternative, since it would let a non-test build
// pull in tar/gzip test scaffolding.
func writeMSAllocationUnavailableBackup(t *testing.T, xmlBody string) string {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	body := []byte(xmlBody)
	if err := tw.WriteHeader(&tar.Header{Name: "onedb.xml", Mode: 0600, Size: int64(len(body))}); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatalf("tar write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	path := filepath.Join(t.TempDir(), "ms-ledger-unavailable.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	return path
}

// TestMSAllocationUnavailableGolden drives testdata/ms-allocation/unavailable.xml
// through a real Scan() to obtain a genuine whole-grid baseline, then calls
// DeriveMSAllocationScenarios directly with a nil ledger and the unavailable
// diagnostic (Message sourced from the in-package constant, never
// hand-typed), and either regenerates or round-trips
// testdata/ms-allocation/unavailable.json. The round trip — reading the
// checked-in golden back and asserting it matches a freshly-derived
// snapshot — is what makes a future change to msAllocationUnavailableMessage
// fail this test instead of leaving the golden's wording to silently drift.
func TestMSAllocationUnavailableGolden(t *testing.T) {
	xmlBody, err := os.ReadFile("../../../testdata/ms-allocation/unavailable.xml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	path := writeMSAllocationUnavailableBackup(t, string(xmlBody))
	s := New()
	baseline, err := s.Scan(context.Background(), scanner.ScanRequest{
		Provider:    "nios",
		Credentials: map[string]string{"backup_path": path},
	}, func(scanner.Event) {})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	diag := MSAllocationDiagnostic{
		Available: false,
		Code:      MSAllocationUnavailableCode,
		Message:   msAllocationUnavailableMessage,
	}
	set := DeriveMSAllocationScenarios(nil, diag, baseline)
	if len(set.Scenarios) != 0 {
		t.Fatalf("len(Scenarios) = %d, want 0 (unavailable branch reports no scenarios)", len(set.Scenarios))
	}
	if set.BaselineTokens <= 0 {
		t.Fatalf("BaselineTokens = %d, want > 0 (the baseline scan must remain usable)", set.BaselineTokens)
	}

	// Mirror scanner.go's niosMicrosoftAllocationJSON wire wrapper exactly —
	// same unexported type, same shape every other hop in
	// ms_allocation_parity_test.go decodes — so this golden is byte-for-byte
	// what a real GetNiosMicrosoftAllocationJSON() call would have produced
	// had the conservation gate actually failed. Evidence is the zero value:
	// GetNiosMicrosoftAllocationJSON only reads s.microsoftLedger.Evidence
	// when the ledger is non-nil, which it never is on this branch.
	wrapped := niosMicrosoftAllocationJSON{
		MSAllocationScenarioSet: set,
		Evidence:                MSEvidenceCounts{},
	}

	golden := "../../../testdata/ms-allocation/unavailable.json"
	if *updateMSAllocationUnavailableGolden {
		data, err := json.Marshal(wrapped)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		var buf bytes.Buffer
		if err := json.Indent(&buf, data, "", "  "); err != nil {
			t.Fatalf("json.Indent: %v", err)
		}
		if err := os.WriteFile(golden, append(buf.Bytes(), '\n'), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	wantData, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden %s: %v", golden, err)
	}
	var want niosMicrosoftAllocationJSON
	if err := json.Unmarshal(wantData, &want); err != nil {
		t.Fatalf("decode golden %s: %v", golden, err)
	}

	if want.Diagnostic != wrapped.Diagnostic {
		t.Errorf("golden diagnostic = %+v, want %+v (source constant drifted from the checked-in golden)", want.Diagnostic, wrapped.Diagnostic)
	}
	if want.BaselineTokens != wrapped.BaselineTokens {
		t.Errorf("golden baselineTokens = %d, want %d", want.BaselineTokens, wrapped.BaselineTokens)
	}
	if len(want.Scenarios) != len(wrapped.Scenarios) {
		t.Errorf("golden len(scenarios) = %d, want %d", len(want.Scenarios), len(wrapped.Scenarios))
	}
	if want.Evidence != wrapped.Evidence {
		t.Errorf("golden evidence = %+v, want %+v", want.Evidence, wrapped.Evidence)
	}
}
