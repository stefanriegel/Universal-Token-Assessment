package nios_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/exporter"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/scanner"
	niosscanner "github.com/stefanriegel/Universal-Token-Assessment/internal/scanner/nios"
	"github.com/stefanriegel/Universal-Token-Assessment/server"

	"encoding/json"
)

// deriveMSAllocationScenarios runs a real Scan over objects, takes the
// Microsoft ledger and diagnostic from MicrosoftLedgerForTest and the
// whole-grid baseline finding rows from Scan's own return value, and calls
// DeriveMSAllocationScenarios on them — proving the derivation layer end to
// end through the real backup-parsing pipeline rather than against
// hand-built ledger/row values. Reuses msLedgerXML / writeMSLedgerBackup from
// ms_ledger_fixtures_test.go (same package); defines no fixture writer of
// its own.
func deriveMSAllocationScenarios(t *testing.T, objects ...string) niosscanner.MSAllocationScenarioSet {
	t.Helper()
	path := writeMSLedgerBackup(t, msLedgerXML(objects...))
	s := niosscanner.New()
	rows, err := s.Scan(context.Background(), scanner.ScanRequest{
		Provider:    "nios",
		Credentials: map[string]string{"backup_path": path},
	}, func(scanner.Event) {})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	ledger, diag := s.MicrosoftLedgerForTest()
	return niosscanner.DeriveMSAllocationScenarios(ledger, diag, rows)
}

// TestMSAllocation_EndToEnd proves the four scenarios derive correctly from
// a real backup scan: one Microsoft server managing both services, one
// claimed DNS zone with two A records, one claimed network, one
// dhcp_range, and one fixed_address carrying the server's own reference.
func TestMSAllocation_EndToEnd(t *testing.T) {
	const zoneRef = ".1.dns.zone_ref_placeholder.allocation_e2e"
	const serverOID = "801"
	const address, cidr, view = "192.0.2.0", "24", "0"
	netKey := niosNetworkKey(address, cidr, view)

	set := deriveMSAllocationScenarios(t,
		msServerXML(serverOID),
		msServerDNSPropertiesXML(serverOID, "true"),
		msServerDHCPPropertiesXML(serverOID, "true"),
		zoneMSPrimaryServerXML(zoneRef, serverOID),
		zoneXML(zoneRef),
		bindARecordXML(zoneRef),
		bindARecordXML(zoneRef),
		dhcpMemberXML(netKey, serverOID),
		networkXML(address, cidr, view),
		dhcpRangeXML(serverOID),
		fixedAddressXML("192.0.2.10", serverOID),
	)

	if !set.Diagnostic.Available {
		t.Fatalf("diagnostic unavailable: %+v", set.Diagnostic)
	}
	if len(set.Scenarios) != 4 {
		t.Fatalf("len(Scenarios) = %d, want 4", len(set.Scenarios))
	}

	wantOrder := []string{
		niosscanner.MSScenarioNone,
		niosscanner.MSScenarioDNSOnly,
		niosscanner.MSScenarioDHCPOnly,
		niosscanner.MSScenarioBoth,
	}
	for i, want := range wantOrder {
		if set.Scenarios[i].ID != want {
			t.Errorf("Scenarios[%d].ID = %q, want %q", i, set.Scenarios[i].ID, want)
		}
	}
	none, dnsOnly, dhcpOnly := set.Scenarios[0], set.Scenarios[1], set.Scenarios[2]

	// Structural figures the fixture pins directly: one zone + two A records
	// on the DNS side, one network + one dhcp_range on the DHCP side. These
	// surface as the DDI Objects category's NativeCount in the scenario
	// where only that service is enabled (msBuildScenarios moves exactly the
	// per-service attributable count when its switch alone is on).
	if dnsOnly.Categories[0].NativeCount != 3 {
		t.Errorf("dns-only DDI Objects NativeCount = %d, want 3 (1 zone + 2 A records)", dnsOnly.Categories[0].NativeCount)
	}
	if dhcpOnly.Categories[0].NativeCount != 2 {
		t.Errorf("dhcp-only DDI Objects NativeCount = %d, want 2 (network + dhcp_range)", dhcpOnly.Categories[0].NativeCount)
	}

	// D-01: Active IPs are credited exclusively via DHCP paths — the
	// DNS-only scenario's Active-IP category must be identical to the
	// both-off scenario's.
	if none.Categories[1] != dnsOnly.Categories[1] {
		t.Errorf("dns-only Active IPs category = %+v, want identical to both-off %+v", dnsOnly.Categories[1], none.Categories[1])
	}
	if dhcpOnly.Categories[1].NativeCount == 0 {
		t.Errorf("dhcp-only Active IPs NativeCount = 0, want non-zero (fixed_address + network reservations)")
	}

	// D-11: Managed Assets stays an explicit zero row in every scenario.
	for _, sc := range set.Scenarios {
		if sc.Categories[2].NIOSCount != 0 || sc.Categories[2].NativeCount != 0 || sc.Categories[2].Tokens != 0 {
			t.Errorf("scenario %q Managed Assets = %+v, want a zero row", sc.ID, sc.Categories[2])
		}
	}

	// D-10: allocation only ever costs tokens, never saves them.
	for _, sc := range set.Scenarios {
		if sc.DeltaTokens < 0 {
			t.Errorf("scenario %q DeltaTokens = %d, want >= 0", sc.ID, sc.DeltaTokens)
		}
	}

	// The both-off scenario moves nothing, so its effective total must equal
	// the reported whole-grid baseline exactly.
	if none.EffectiveTokens != set.BaselineTokens {
		t.Errorf("both-off EffectiveTokens = %d, want equal to BaselineTokens %d", none.EffectiveTokens, set.BaselineTokens)
	}
}

// TestMSAllocation_Deterministic carries TestMSLedger_Deterministic's
// order-invariance guarantee forward through the derivation layer: the same
// objects, in a different onedb.xml order that places a relationship row
// (ms_dhcp_member, zone_ms_primary_server) before the object it references,
// must derive byte-identical scenario sets.
func TestMSAllocation_Deterministic(t *testing.T) {
	const zoneRef = ".1.dns.zone_ref_placeholder.allocation_deterministic"
	const serverOID = "802"
	const address, cidr, view = "192.0.2.128", "25", "0"
	netKey := niosNetworkKey(address, cidr, view)

	natural := []string{
		msServerXML(serverOID),
		msServerDNSPropertiesXML(serverOID, "true"),
		msServerDHCPPropertiesXML(serverOID, "true"),
		zoneMSPrimaryServerXML(zoneRef, serverOID),
		zoneXML(zoneRef),
		bindARecordXML(zoneRef),
		dhcpMemberXML(netKey, serverOID),
		networkXML(address, cidr, view),
		dhcpRangeXML(serverOID),
	}
	refFirst := []string{
		dhcpMemberXML(netKey, serverOID),
		networkXML(address, cidr, view),
		zoneMSPrimaryServerXML(zoneRef, serverOID),
		bindARecordXML(zoneRef),
		zoneXML(zoneRef),
		dhcpRangeXML(serverOID),
		msServerXML(serverOID),
		msServerDNSPropertiesXML(serverOID, "true"),
		msServerDHCPPropertiesXML(serverOID, "true"),
	}

	setNatural := deriveMSAllocationScenarios(t, natural...)
	setRefFirst := deriveMSAllocationScenarios(t, refFirst...)

	if !reflect.DeepEqual(setNatural, setRefFirst) {
		t.Errorf("scenario set differs by XML order:\nnatural:   %+v\nref-first: %+v", setNatural, setRefFirst)
	}
	// Non-degeneracy: the fixture must actually move both categories, or an
	// empty derivation could pass the comparison vacuously.
	both := setNatural.Scenarios[3]
	if both.Categories[0].Tokens == 0 || both.Categories[1].Tokens == 0 {
		t.Fatalf("fixture produced a degenerate scenario set: %+v", both)
	}
}

// TestMSAllocation_NoIdentifiers proves the derived scenario set carries no
// backup-derived identifier into its serialized form: a distinctive
// placeholder marker planted in the zone name, server host name, and
// network view must not survive into the JSON encoding, nor may any fixture
// address, and reflection over the domain result types — plus the
// server and exporter wire copies of the same shape — must find no
// string-typed field beyond the seven the design allows.
func TestMSAllocation_NoIdentifiers(t *testing.T) {
	const marker = "zzz-privacy-probe-marker"
	const serverOID = "901"
	const serverAddress = "192.0.2.200"
	const fixedAddress = "192.0.2.201"
	const networkAddress, cidr = "192.0.2.192", "26"

	// msServerXML hardcodes its resolved_name; this test needs the marker in
	// the host name, so it defines its own single ms_server object block
	// rather than extending the shared helper's fixed placeholder.
	serverXML := `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.one.ms_server"/>
<PROPERTY NAME="ms_oid" VALUE="` + serverOID + `"/>
<PROPERTY NAME="resolved_name" VALUE="` + marker + `.test.local"/>
<PROPERTY NAME="address" VALUE="` + serverAddress + `"/>
</OBJECT>`

	zoneRef := ".1.dns." + marker + ".default"
	netKey := niosNetworkKey(networkAddress, cidr, marker)

	set := deriveMSAllocationScenarios(t,
		serverXML,
		msServerDNSPropertiesXML(serverOID, "true"),
		msServerDHCPPropertiesXML(serverOID, "true"),
		zoneMSPrimaryServerXML(zoneRef, serverOID),
		zoneXML(zoneRef),
		bindARecordXML(zoneRef),
		dhcpMemberXML(netKey, serverOID),
		networkXML(networkAddress, cidr, marker),
		dhcpRangeXML(serverOID),
		fixedAddressXML(fixedAddress, serverOID),
	)
	if !set.Diagnostic.Available {
		t.Fatalf("diagnostic unavailable: %+v", set.Diagnostic)
	}

	data, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	encoded := string(data)

	for _, forbidden := range []string{marker, serverAddress, fixedAddress, networkAddress} {
		if strings.Contains(encoded, forbidden) {
			t.Errorf("marshalled scenario set contains forbidden value %q", forbidden)
		}
	}

	allowed := map[string]bool{
		"ID": true, "Category": true, "Code": true, "Message": true,
	}
	got := map[string]bool{}
	seen := map[reflect.Type]bool{}
	for _, sample := range []any{
		niosscanner.MSAllocationScenarioSet{},
		niosscanner.MSAllocationScenario{},
		niosscanner.MSCategoryTokens{},
		server.MicrosoftAllocation{},
		exporter.MicrosoftAllocation{},
	} {
		collectStringFields(reflect.TypeOf(sample), seen, got)
	}
	for name := range got {
		if !allowed[name] {
			t.Errorf("unexpected string-typed field %q reachable from MSAllocationScenarioSet/MSAllocationScenario/MSCategoryTokens — review for privacy before adding", name)
		}
	}
	for name := range allowed {
		if !got[name] {
			t.Errorf("expected string-typed field %q not found — result type shape changed", name)
		}
	}
}

// collectStringFields walks t's reachable struct/slice/array/pointer graph
// and records the name of every string-kinded field it finds into out.
// seen prevents revisiting a struct type already walked (both a dedup and a
// cycle guard).
func collectStringFields(t reflect.Type, seen map[reflect.Type]bool, out map[string]bool) {
	switch t.Kind() {
	case reflect.Pointer:
		collectStringFields(t.Elem(), seen, out)
	case reflect.Slice, reflect.Array:
		collectStringFields(t.Elem(), seen, out)
	case reflect.Struct:
		if seen[t] {
			return
		}
		seen[t] = true
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			switch f.Type.Kind() {
			case reflect.String:
				out[f.Name] = true
			case reflect.Struct, reflect.Slice, reflect.Array, reflect.Pointer, reflect.Map:
				collectStringFields(f.Type, seen, out)
			}
		}
	}
}
