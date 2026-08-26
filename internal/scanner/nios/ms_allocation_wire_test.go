package nios_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/scanner"
	niosscanner "github.com/stefanriegel/Universal-Token-Assessment/internal/scanner/nios"
)

// scanNiosMicrosoftAllocationJSON runs a real Scan over objects, exactly like
// deriveMSAllocationScenarios (ms_allocation_fixtures_test.go), but returns
// the scanner's own marshalled GetNiosMicrosoftAllocationJSON() output
// instead of calling DeriveMSAllocationScenarios directly — proving the wire
// getter end to end through the real backup-parsing pipeline.
func scanNiosMicrosoftAllocationJSON(t *testing.T, objects ...string) []byte {
	t.Helper()
	path := writeMSLedgerBackup(t, msLedgerXML(objects...))
	s := niosscanner.New()
	_, err := s.Scan(context.Background(), scanner.ScanRequest{
		Provider:    "nios",
		Credentials: map[string]string{"backup_path": path},
	}, func(scanner.Event) {})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	return s.GetNiosMicrosoftAllocationJSON()
}

// niosMicrosoftAllocationWireShape mirrors the JSON shape
// GetNiosMicrosoftAllocationJSON marshals: MSAllocationScenarioSet's fields
// inlined, plus the sibling evidence field.
type niosMicrosoftAllocationWireShape struct {
	Diagnostic struct {
		Available bool   `json:"available"`
		Code      string `json:"code"`
		Message   string `json:"message"`
	} `json:"diagnostic"`
	BaselineTokens int `json:"baselineTokens"`
	Scenarios      []struct {
		ID          string `json:"id"`
		DeltaTokens int    `json:"deltaTokens"`
	} `json:"scenarios"`
	Evidence struct {
		RelationshipRows int `json:"relationshipRows"`
	} `json:"evidence"`
}

// TestScanner_GetNiosMicrosoftAllocationJSON proves the scan-commit call site
// (Task 1) produces a marshalled allocation snapshot with all four scenarios
// in their fixed order and a present evidence sibling field.
func TestScanner_GetNiosMicrosoftAllocationJSON(t *testing.T) {
	const zoneRef = ".1.dns.zone_ref_placeholder.wire_test"
	const serverOID = "911"
	const address, cidr, view = "192.0.2.64", "26", "0"
	netKey := niosNetworkKey(address, cidr, view)

	data := scanNiosMicrosoftAllocationJSON(t,
		msServerXML(serverOID),
		msServerDNSPropertiesXML(serverOID, "true"),
		msServerDHCPPropertiesXML(serverOID, "true"),
		zoneMSPrimaryServerXML(zoneRef, serverOID),
		zoneXML(zoneRef),
		bindARecordXML(zoneRef),
		dhcpMemberXML(netKey, serverOID),
		networkXML(address, cidr, view),
		dhcpRangeXML(serverOID),
		fixedAddressXML("192.0.2.65", serverOID),
	)
	if data == nil {
		t.Fatalf("GetNiosMicrosoftAllocationJSON() returned nil, want a marshalled snapshot")
	}

	var got niosMicrosoftAllocationWireShape
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v (data=%s)", err, data)
	}

	if !got.Diagnostic.Available {
		t.Fatalf("diagnostic unavailable: %+v", got.Diagnostic)
	}
	if len(got.Scenarios) != 4 {
		t.Fatalf("len(Scenarios) = %d, want 4", len(got.Scenarios))
	}
	wantOrder := []string{
		niosscanner.MSScenarioNone,
		niosscanner.MSScenarioDNSOnly,
		niosscanner.MSScenarioDHCPOnly,
		niosscanner.MSScenarioBoth,
	}
	for i, want := range wantOrder {
		if got.Scenarios[i].ID != want {
			t.Errorf("Scenarios[%d].ID = %q, want %q", i, got.Scenarios[i].ID, want)
		}
	}
	if got.BaselineTokens <= 0 {
		t.Errorf("BaselineTokens = %d, want > 0", got.BaselineTokens)
	}
	// The evidence key must be present in the JSON — presence, not a
	// specific count, is what this getter contributes over
	// DeriveMSAllocationScenarios's own return type.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal (raw): %v", err)
	}
	if _, ok := raw["evidence"]; !ok {
		t.Fatalf("marshalled snapshot has no top-level %q key: %s", "evidence", data)
	}
	var evidence map[string]json.RawMessage
	if err := json.Unmarshal(raw["evidence"], &evidence); err != nil {
		t.Fatalf("json.Unmarshal (evidence): %v", err)
	}
	if _, ok := evidence["relationshipRows"]; !ok {
		t.Errorf("evidence object missing %q key: %s", "relationshipRows", raw["evidence"])
	}
}

// TestScanner_GetNiosMicrosoftAllocationJSON_Absent proves a scan with no
// ms_server object still returns a marshalled snapshot (not nil) whose
// diagnostic carries D-07's MS_ALLOCATION_ABSENT code and whose four
// scenarios all report zero movement.
func TestScanner_GetNiosMicrosoftAllocationJSON_Absent(t *testing.T) {
	data := scanNiosMicrosoftAllocationJSON(t, zoneXML(".1.dns.zone_ref_placeholder.no_ms_server"))
	if data == nil {
		t.Fatalf("GetNiosMicrosoftAllocationJSON() returned nil, want a marshalled snapshot (D-07 absent state)")
	}

	var got niosMicrosoftAllocationWireShape
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v (data=%s)", err, data)
	}

	if got.Diagnostic.Available {
		t.Fatalf("diagnostic available = true, want false (no ms_server present)")
	}
	if got.Diagnostic.Code != niosscanner.MSAllocationAbsentCode {
		t.Errorf("diagnostic.Code = %q, want %q", got.Diagnostic.Code, niosscanner.MSAllocationAbsentCode)
	}
	if len(got.Scenarios) != 4 {
		t.Fatalf("len(Scenarios) = %d, want 4", len(got.Scenarios))
	}
	for _, sc := range got.Scenarios {
		if sc.DeltaTokens != 0 {
			t.Errorf("scenario %q DeltaTokens = %d, want 0 (absent state moves nothing)", sc.ID, sc.DeltaTokens)
		}
	}
}
