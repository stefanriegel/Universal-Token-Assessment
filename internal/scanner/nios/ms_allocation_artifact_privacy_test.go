package nios_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/calculator"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/exporter"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/scanner"
	niosscanner "github.com/stefanriegel/Universal-Token-Assessment/internal/scanner/nios"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/session"
	"github.com/stefanriegel/Universal-Token-Assessment/server"
)

// Synthetic identifiers planted by msPrivacyMarkerBackup. All addresses stay
// inside the 192.0.2.0/24 documentation range (RFC 5737) and the marker
// carries a reserved ".test.local" suffix — this fixture never contains a
// real backup-derived value, so any of these four strings surviving into a
// generated artifact proves a real leak path, not a false positive.
const (
	msPrivacyMarker         = "zzz-artifact-privacy-marker"
	msPrivacyServerOID      = "951"
	msPrivacyServerAddress  = "192.0.2.210"
	msPrivacyFixedAddress   = "192.0.2.211"
	msPrivacyNetworkAddress = "192.0.2.208"
	msPrivacyNetworkCIDR    = "29"
)

// msPrivacyMarkerBackup builds a synthetic onedb.xml backup (via the shared
// msLedgerXML/writeMSLedgerBackup helpers from ms_ledger_fixtures_test.go)
// that plants the marker in the Microsoft server's resolved_name, the DNS
// zone reference, and the DHCP network view name, alongside three distinct
// reserved-range addresses for the server, a fixed address, and the network.
// msServerXML hardcodes its own resolved_name and cannot carry a marker, so
// this defines its own single ms_server OBJECT block, the same technique
// TestMSAllocation_NoIdentifiers already uses.
func msPrivacyMarkerBackup(t *testing.T) string {
	t.Helper()

	serverXML := `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.one.ms_server"/>
<PROPERTY NAME="ms_oid" VALUE="` + msPrivacyServerOID + `"/>
<PROPERTY NAME="resolved_name" VALUE="` + msPrivacyMarker + `.test.local"/>
<PROPERTY NAME="address" VALUE="` + msPrivacyServerAddress + `"/>
</OBJECT>`

	zoneRef := ".1.dns." + msPrivacyMarker + ".default"
	netKey := niosNetworkKey(msPrivacyNetworkAddress, msPrivacyNetworkCIDR, msPrivacyMarker)

	return writeMSLedgerBackup(t, msLedgerXML(
		serverXML,
		msServerDNSPropertiesXML(msPrivacyServerOID, "true"),
		msServerDHCPPropertiesXML(msPrivacyServerOID, "true"),
		zoneMSPrimaryServerXML(zoneRef, msPrivacyServerOID),
		zoneXML(zoneRef),
		bindARecordXML(zoneRef),
		dhcpMemberXML(netKey, msPrivacyServerOID),
		networkXML(msPrivacyNetworkAddress, msPrivacyNetworkCIDR, msPrivacyMarker),
		dhcpRangeXML(msPrivacyServerOID),
		fixedAddressXML(msPrivacyFixedAddress, msPrivacyServerOID),
	))
}

// msPrivacyForbiddenValues returns the four values msPrivacyMarkerBackup
// plants: the marker, the server address, the fixed address, and the network
// address. None of these may appear in either generated artifact's raw bytes.
func msPrivacyForbiddenValues() []string {
	return []string{
		msPrivacyMarker,
		msPrivacyServerAddress,
		msPrivacyFixedAddress,
		msPrivacyNetworkAddress,
	}
}

// msPrivacyAssertAbsent fails the test for each value in forbidden found in
// data, naming only the artifact label and the value itself — never the
// surrounding bytes, so a genuine failure does not become a leak in CI logs
// in its own right (every value here is a synthetic marker/address the test
// planted, not a real secret, but the discipline is the point).
func msPrivacyAssertAbsent(t *testing.T, label string, data []byte, forbidden []string) {
	t.Helper()
	s := string(data)
	for _, v := range forbidden {
		if strings.Contains(s, v) {
			t.Errorf("%s contains forbidden value %q", label, v)
		}
	}
}

// TestMSAllocation_NoIdentifiersInArtifacts extends TestMSAllocation_NoIdentifiers
// outward from the in-memory scenario set to the two artifacts a user can
// actually walk away with: the session blob a saved session file carries, and
// the raw bytes of a generated .xlsx workbook. It does not modify
// TestMSAllocation_NoIdentifiers or ms_allocation_fixtures_test.go — that test
// owns the in-memory boundary and the reflection allow-set; this file owns the
// artifact boundary.
func TestMSAllocation_NoIdentifiersInArtifacts(t *testing.T) {
	forbidden := msPrivacyForbiddenValues()
	path := msPrivacyMarkerBackup(t)

	s := niosscanner.New()
	rows, err := s.Scan(context.Background(), scanner.ScanRequest{
		Provider:    "nios",
		Credentials: map[string]string{"backup_path": path},
	}, func(scanner.Event) {})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	allocationJSON := s.GetNiosMicrosoftAllocationJSON()
	if allocationJSON == nil {
		t.Fatalf("GetNiosMicrosoftAllocationJSON() = nil, want a snapshot")
	}

	// Fail immediately unless the diagnostic reports available: a suppressed
	// snapshot carries almost nothing and would let this test pass without
	// proving anything (D-16 / MSAllocationUnavailableCode).
	var diagProbe struct {
		Diagnostic struct {
			Available bool `json:"available"`
		} `json:"diagnostic"`
	}
	if err := json.Unmarshal(allocationJSON, &diagProbe); err != nil {
		t.Fatalf("unmarshal diagnostic probe: %v", err)
	}
	if !diagProbe.Diagnostic.Available {
		t.Fatalf("diagnostic unavailable — this fixture must conserve; suppressed snapshots are out of scope for this probe")
	}

	// ---- Artifact 1: the session-carried snapshot ----
	//
	// This is the honest Go-reachable stand-in for the saved session file's
	// microsoftAllocation field: the browser writes the session file in
	// frontend/src/app/components/session-io.ts's exportSession, copying this
	// exact payload field verbatim, and no Go test can invoke that path. The
	// allocation content of the stored blob and the saved session file is
	// identical by construction, which is an argument, not an assertion —
	// see the backstop truth recorded in 08-03-PLAN.md.
	store := session.NewStore()
	sess := store.New()
	sess.SetNiosMicrosoftAllocationJSON(allocationJSON)
	stored := sess.NiosMicrosoftAllocationJSON
	msPrivacyAssertAbsent(t, "session-stored allocation blob", stored, forbidden)

	var wireAllocation server.MicrosoftAllocation
	if err := json.Unmarshal(stored, &wireAllocation); err != nil {
		t.Fatalf("unmarshal server.MicrosoftAllocation: %v", err)
	}
	results := server.ScanResultsResponse{
		ScanID:               "ms-privacy-probe",
		MicrosoftAllocation:  &wireAllocation,
		NiosMicrosoftServers: nil,
	}
	resultsData, err := json.Marshal(results)
	if err != nil {
		t.Fatalf("json.Marshal(ScanResultsResponse): %v", err)
	}
	msPrivacyAssertAbsent(t, "marshalled ScanResultsResponse", resultsData, forbidden)

	// ---- Artifact 2: the generated workbook ----
	//
	// Raw bytes rather than parsed cells is deliberate: it also covers the
	// shared-strings table and file metadata that a cell-level read never sees.
	var exportAllocation exporter.MicrosoftAllocation
	if err := json.Unmarshal(stored, &exportAllocation); err != nil {
		t.Fatalf("unmarshal exporter.MicrosoftAllocation: %v", err)
	}

	tr := calculator.Calculate(rows)
	in := exporter.ReportInput{
		GeneratedAt: time.Now(),
		Findings:    rows,
		Totals: exporter.TokenTotals{
			DDITokens:   tr.DDITokens,
			IPTokens:    tr.IPTokens,
			AssetTokens: tr.AssetTokens,
			GrandTotal:  tr.GrandTotal,
		},
		MicrosoftAllocation: &exportAllocation,
		SelectedMSScenario:  "both",
	}
	var buf bytes.Buffer
	if err := exporter.Build(&buf, in); err != nil {
		t.Fatalf("exporter.Build: %v", err)
	}
	workbook := buf.Bytes()
	msPrivacyAssertAbsent(t, "generated workbook raw bytes", workbook, forbidden)
}
