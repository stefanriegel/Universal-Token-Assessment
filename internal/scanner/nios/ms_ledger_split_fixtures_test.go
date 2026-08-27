package nios_test

import (
	"context"
	"testing"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/scanner"
	niosscanner "github.com/stefanriegel/Universal-Token-Assessment/internal/scanner/nios"
)

// scanForServiceSplit runs Scan against a fixture assembled from objects and
// returns the resulting ledger and diagnostic, failing the test on any scan
// error. Reuses msLedgerXML / writeMSLedgerBackup from ms_ledger_fixtures_test.go
// (same package) rather than reimplementing fixture assembly.
func scanForServiceSplit(t *testing.T, objects ...string) (*niosscanner.MicrosoftAllocationLedger, niosscanner.MSAllocationDiagnostic) {
	t.Helper()
	path := writeMSLedgerBackup(t, msLedgerXML(objects...))
	s := niosscanner.New()
	if _, err := s.Scan(context.Background(), scanner.ScanRequest{
		Provider:    "nios",
		Credentials: map[string]string{"backup_path": path},
	}, func(scanner.Event) {}); err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	return s.MicrosoftLedgerForTest()
}

// TestMSAllocation_ServiceSplit proves the per-service DDI split
// (MSServiceSplit / DDIObjectsDNS / DDIObjectsDHCP, added in ms_ledger.go)
// sums to the unchanged combined DDIObjects count through the real
// end-to-end Scan pipeline. Every identifier below is a synthetic
// placeholder — a zone reference key that never resembles a real hostname,
// and RFC 5737 TEST-NET-1 addresses for the DHCP side.
func TestMSAllocation_ServiceSplit(t *testing.T) {
	const zoneRef = ".1.dns.zone_ref_split_test.default"
	const serverOID = "501"
	const address, cidr, view = "192.0.2.0", "24", "0"
	networkKey := niosNetworkKey(address, cidr, view)

	t.Run("one server managing both services splits DDI Objects by service", func(t *testing.T) {
		ledger, diag := scanForServiceSplit(t,
			msServerXML(serverOID),
			msServerDNSPropertiesXML(serverOID, "true"),
			msServerDHCPPropertiesXML(serverOID, "true"),
			zoneMSPrimaryServerXML(zoneRef, serverOID),
			zoneXML(zoneRef),
			bindARecordXML(zoneRef),
			dhcpMemberXML(networkKey, serverOID),
			networkXML(address, cidr, view),
			dhcpRangeXML(serverOID),
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}

		if got, want := ledger.DDIObjectsDNS.Attributable+ledger.DDIObjectsDHCP.Attributable, ledger.DDIObjects.Attributable; got != want {
			t.Errorf("DDIObjectsDNS.Attributable(%d) + DDIObjectsDHCP.Attributable(%d) = %d, want DDIObjects.Attributable %d",
				ledger.DDIObjectsDNS.Attributable, ledger.DDIObjectsDHCP.Attributable, got, want)
		}
		if got, want := ledger.DDIObjectsDNS.Retained+ledger.DDIObjectsDHCP.Retained, ledger.DDIObjects.Retained; got != want {
			t.Errorf("DDIObjectsDNS.Retained(%d) + DDIObjectsDHCP.Retained(%d) = %d, want DDIObjects.Retained %d",
				ledger.DDIObjectsDNS.Retained, ledger.DDIObjectsDHCP.Retained, got, want)
		}
		if ledger.DDIObjectsDNS.Attributable == 0 {
			t.Errorf("DDIObjectsDNS.Attributable = 0, want non-zero (non-degeneracy guard: identity must not be asserted vacuously on two zeroes)")
		}
		if ledger.DDIObjectsDHCP.Attributable == 0 {
			t.Errorf("DDIObjectsDHCP.Attributable = 0, want non-zero (non-degeneracy guard: identity must not be asserted vacuously on two zeroes)")
		}
	})

	t.Run("DNS-only fixture moves only DNS-side DDI Objects and no Active IPs", func(t *testing.T) {
		ledger, diag := scanForServiceSplit(t,
			msServerXML(serverOID),
			msServerDNSPropertiesXML(serverOID, "true"),
			zoneMSPrimaryServerXML(zoneRef, serverOID),
			zoneXML(zoneRef),
			bindARecordXML(zoneRef),
			bindARecordXML(zoneRef),
			bindARecordXML(zoneRef),
			// No Microsoft-claimed DHCP object at all.
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.DDIObjectsDHCP.Attributable != 0 {
			t.Errorf("DDIObjectsDHCP.Attributable = %d, want 0 (DNS-only fixture)", ledger.DDIObjectsDHCP.Attributable)
		}
		if ledger.DDIObjectsDNS.Attributable != 4 {
			t.Errorf("DDIObjectsDNS.Attributable = %d, want 4 (1 zone + 3 A records)", ledger.DDIObjectsDNS.Attributable)
		}
		if ledger.ActiveIPs.Attributable != 0 {
			t.Errorf("ActiveIPs.Attributable = %d, want 0 — enabling DNS must never move an Active IP", ledger.ActiveIPs.Attributable)
		}
	})

	t.Run("DHCP-only fixture moves only DHCP-side DDI Objects and no DNS Objects", func(t *testing.T) {
		parentRef := ".com.infoblox.dns.network$" + networkKey
		ledger, diag := scanForServiceSplit(t,
			msServerXML(serverOID),
			msServerDHCPPropertiesXML(serverOID, "true"),
			dhcpMemberXML(networkKey, serverOID),
			networkXML(address, cidr, view),
			dhcpRangeXML(serverOID),
			// Parent exact-match short-circuits containment entirely (mirrors
			// TestMSLedger_DHCPContainmentIntegration's "parent exact match"
			// subtest), so no start_address/network_view is needed here.
			exclusionRangeXMLFull(parentRef, "", ""),
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.DDIObjectsDNS.Attributable != 0 {
			t.Errorf("DDIObjectsDNS.Attributable = %d, want 0 (DHCP-only fixture)", ledger.DDIObjectsDNS.Attributable)
		}
		if ledger.DDIObjectsDHCP.Attributable != 3 {
			t.Errorf("DDIObjectsDHCP.Attributable = %d, want 3 (network + dhcp_range + exclusion_range)", ledger.DDIObjectsDHCP.Attributable)
		}
		if ledger.ActiveIPs.Attributable != 2 {
			t.Errorf("ActiveIPs.Attributable = %d, want 2 (the network's two reserved addresses, D-02)", ledger.ActiveIPs.Attributable)
		}
	})
}
