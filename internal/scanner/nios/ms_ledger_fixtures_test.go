package nios_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/calculator"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/scanner"
	niosscanner "github.com/stefanriegel/Universal-Token-Assessment/internal/scanner/nios"
)

// msLedgerBaseXML is the minimal grid preamble every Microsoft ledger fixture
// needs: one Grid Master virtual_node. Plans 05-03, 05-04, and 05-05 concatenate
// Microsoft-specific object blocks onto this via msLedgerXML rather than each
// maintaining their own preamble.
const msLedgerBaseXML = `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.one.virtual_node"/>
<PROPERTY NAME="virtual_oid" VALUE="101"/>
<PROPERTY NAME="host_name" VALUE="gm.test.local"/>
<PROPERTY NAME="is_grid_master" VALUE="true"/>
</OBJECT>`

// msLedgerXML assembles a complete onedb.xml document from the grid preamble
// plus any number of supplied object blocks. Keeping assembly here — rather
// than in each test file — is what makes a later order-invariance fixture
// pair (05-05) a matter of reordering the variadic arguments, not maintaining
// two divergent XML blobs.
func msLedgerXML(objects ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<DATABASE NAME="onedb" VERSION="9.0.6-test">` + "\n")
	b.WriteString(msLedgerBaseXML + "\n")
	for _, obj := range objects {
		b.WriteString(obj + "\n")
	}
	b.WriteString(`</DATABASE>`)
	return b.String()
}

// writeMSLedgerBackup writes xmlBody as onedb.xml inside a gzip+tar archive —
// the format the NIOS scanner accepts for backup_path. This is the single
// fixture writer every integration test in plans 05-03, 05-04, and 05-05
// reuses; do not reimplement it.
func writeMSLedgerBackup(t *testing.T, xmlBody string) string {
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

	path := filepath.Join(t.TempDir(), "ms-ledger.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	return path
}

// TestMSLedger_FixtureSmoke proves writeMSLedgerBackup and msLedgerXML produce
// an archive the existing scanner actually accepts, before any Microsoft
// object type is involved.
func TestMSLedger_FixtureSmoke(t *testing.T) {
	path := writeMSLedgerBackup(t, msLedgerXML())

	rows, err := niosscanner.New().Scan(context.Background(), scanner.ScanRequest{
		Provider:    "nios",
		Credentials: map[string]string{"backup_path": path},
	}, func(scanner.Event) {})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	// A member-only backup with no DDI objects legitimately produces a
	// nil-or-empty rows slice — either is fine here. The point of this test
	// is that assembling and parsing the fixture did not error.
	if len(rows) != 0 {
		t.Errorf("rows = %+v, want none for a member-only backup", rows)
	}
}

// ---- Microsoft ledger fixture object blocks (Task 1: DNS tracer) ----
//
// All identifiers below are synthetic per 05-SCHEMA.md's placeholder rules:
// small round ms_oid numbers, and a zone reference that never appears in any
// real backup.

func msServerXML(oid string) string {
	return `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.one.ms_server"/>
<PROPERTY NAME="ms_oid" VALUE="` + oid + `"/>
<PROPERTY NAME="resolved_name" VALUE="dc01.example.local"/>
<PROPERTY NAME="address" VALUE="192.0.2.0"/>
</OBJECT>`
}

func msServerDNSPropertiesXML(oid, managed string) string {
	return `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.ms_server_dns_properties"/>
<PROPERTY NAME="parent" VALUE="` + oid + `"/>
<PROPERTY NAME="managed" VALUE="` + managed + `"/>
</OBJECT>`
}

func zoneMSPrimaryServerXML(zone, server string) string {
	return `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.zone_ms_primary_server"/>
<PROPERTY NAME="zone" VALUE="` + zone + `"/>
<PROPERTY NAME="ms_server" VALUE="` + server + `"/>
<PROPERTY NAME="is_master" VALUE="true"/>
</OBJECT>`
}

func zoneMSSecondaryServerXML(zone, server string) string {
	return `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.zone_ms_secondary_server"/>
<PROPERTY NAME="zone" VALUE="` + zone + `"/>
<PROPERTY NAME="ms_server" VALUE="` + server + `"/>
<PROPERTY NAME="is_master" VALUE="false"/>
</OBJECT>`
}

// msZoneParentAndFQDN splits a zone's own reference key into the two things a
// real .com.infoblox.dns.zone object actually carries: its PARENT's reference
// and its own fqdn. It is the inverse of msZoneOwnRef, so a fixture can keep
// naming a zone by the single key its relationship rows and records use while
// the zone object it emits has the shape a backup really has.
func msZoneParentAndFQDN(ownRef string) (parent, fqdn string) {
	cut := strings.LastIndex(ownRef, ".")
	if cut <= 0 {
		return ownRef, ""
	}
	parent = ownRef[:cut]
	segments := strings.Split(ownRef, ".")
	labels := segments[2:] // drop the leading empty segment and the view
	for i := len(labels) - 1; i >= 0; i-- {
		if fqdn != "" {
			fqdn += "."
		}
		fqdn += labels[i]
	}
	return parent, fqdn
}

// zoneXML emits the zone object for the zone whose own reference is ownRef.
// A zone object never carries its own reference — the "zone" property on one
// points at its parent — so emitting ownRef here directly would encode the
// exact misreading msZoneOwnRef exists to correct, and no fixture would ever
// catch it.
func zoneXML(ownRef string) string {
	parent, fqdn := msZoneParentAndFQDN(ownRef)
	return `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.zone"/>
<PROPERTY NAME="zone" VALUE="` + parent + `"/>
<PROPERTY NAME="fqdn" VALUE="` + fqdn + `"/>
</OBJECT>`
}

func hostObjectXML(zone string) string {
	return `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.host"/>
<PROPERTY NAME="zone" VALUE="` + zone + `"/>
<PROPERTY NAME="name" VALUE="host1.example.local"/>
</OBJECT>`
}

// ---- Task 3 fixture object blocks: one eligible DNS record family and one
// ineligible (but still DDIFamilies-member) family for record-eligibility tests.

func bindARecordXML(zone string) string {
	return `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.bind_a"/>
<PROPERTY NAME="zone" VALUE="` + zone + `"/>
<PROPERTY NAME="name" VALUE="host2.example.local"/>
<PROPERTY NAME="ip_address" VALUE="192.0.2.0"/>
</OBJECT>`
}

func hostAliasXML(zone string) string {
	return `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.host_alias"/>
<PROPERTY NAME="zone" VALUE="` + zone + `"/>
<PROPERTY NAME="name" VALUE="alias1.example.local"/>
</OBJECT>`
}

// TestMSLedger_DNSTracer covers Task 1's tracer slice: one Microsoft DNS zone
// attributed once, end to end through the real Scan() pipeline. Every subtest
// must credit every DDI object it introduces to exactly one of Attributable
// or Retained (checkMSConservation's invariant).
func TestMSLedger_DNSTracer(t *testing.T) {
	const zoneRef = ".1.dns.zone_ref_placeholder.default"
	const serverOID = "201"

	scan := func(t *testing.T, objects ...string) (*niosscanner.MicrosoftAllocationLedger, niosscanner.MSAllocationDiagnostic, []byte) {
		t.Helper()
		path := writeMSLedgerBackup(t, msLedgerXML(objects...))
		s := niosscanner.New()
		if _, err := s.Scan(context.Background(), scanner.ScanRequest{
			Provider:    "nios",
			Credentials: map[string]string{"backup_path": path},
		}, func(scanner.Event) {}); err != nil {
			t.Fatalf("Scan error: %v", err)
		}
		ledger, diag := s.MicrosoftLedgerForTest()
		return ledger, diag, s.GetNiosMicrosoftServersJSON()
	}

	t.Run("DNS-managed server attributes an eligible DNS record", func(t *testing.T) {
		// A host object, not an eligible typed DNS record: task 3 makes host
		// objects unconditionally ReasonUnsupportedType regardless of their
		// zone's attribution state, so bindARecordXML is what now exercises
		// "a DNS-managed server attributes the zone's records".
		ledger, diag, _ := scan(t,
			msServerXML(serverOID),
			msServerDNSPropertiesXML(serverOID, "true"),
			zoneMSPrimaryServerXML(zoneRef, serverOID),
			bindARecordXML(zoneRef),
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.DDIObjects.Attributable != 1 {
			t.Errorf("Attributable = %d, want 1", ledger.DDIObjects.Attributable)
		}
		if ledger.DDIObjects.Retained != 0 {
			t.Errorf("Retained = %d, want 0", ledger.DDIObjects.Retained)
		}
	})

	t.Run("DNS-unmanaged server retains the host object", func(t *testing.T) {
		ledger, diag, _ := scan(t,
			msServerXML(serverOID),
			msServerDNSPropertiesXML(serverOID, "false"),
			zoneMSPrimaryServerXML(zoneRef, serverOID),
			hostObjectXML(zoneRef),
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.DDIObjects.Attributable != 0 {
			t.Errorf("Attributable = %d, want 0", ledger.DDIObjects.Attributable)
		}
		if ledger.DDIObjects.Retained != 1 {
			t.Errorf("Retained = %d, want 1", ledger.DDIObjects.Retained)
		}
	})

	t.Run("missing primary relationship retains the host object", func(t *testing.T) {
		ledger, diag, _ := scan(t,
			msServerXML(serverOID),
			msServerDNSPropertiesXML(serverOID, "true"),
			hostObjectXML(zoneRef),
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.DDIObjects.Attributable != 0 {
			t.Errorf("Attributable = %d, want 0", ledger.DDIObjects.Attributable)
		}
		if ledger.DDIObjects.Retained != 1 {
			t.Errorf("Retained = %d, want 1", ledger.DDIObjects.Retained)
		}
	})

	t.Run("secondary-only relationship never claims (D-17)", func(t *testing.T) {
		ledger, diag, msJSON := scan(t,
			msServerXML(serverOID),
			msServerDNSPropertiesXML(serverOID, "true"),
			zoneMSSecondaryServerXML(zoneRef, serverOID),
			hostObjectXML(zoneRef),
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.DDIObjects.Attributable != 0 {
			t.Errorf("Attributable = %d, want 0 (secondary must never claim)", ledger.DDIObjects.Attributable)
		}
		if ledger.Evidence.RelationshipRows == 0 {
			t.Errorf("Evidence.RelationshipRows = 0, want > 0 for the secondary row")
		}
		// D-17: a secondary relationship must not affect the pre-existing
		// informational ManagedZones count either — only the primary type does.
		if msJSON != nil {
			var servers niosscanner.NiosMicrosoftServers
			if err := json.Unmarshal(msJSON, &servers); err != nil {
				t.Fatalf("unmarshal NiosMicrosoftServers: %v", err)
			}
			if servers.ManagedZones != 0 {
				t.Errorf("ManagedZones = %d, want 0 (secondary-only must not count)", servers.ManagedZones)
			}
		}
	})

	t.Run("baseline always equals attributable plus retained", func(t *testing.T) {
		ledger, diag, _ := scan(t,
			msServerXML(serverOID),
			msServerDNSPropertiesXML(serverOID, "true"),
			zoneMSPrimaryServerXML(zoneRef, serverOID),
			hostObjectXML(zoneRef),
			zoneXML(zoneRef),
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		got := ledger.DDIObjects.Attributable + ledger.DDIObjects.Retained
		if got != ledger.DDIObjects.Baseline {
			t.Errorf("Attributable(%d) + Retained(%d) = %d, want Baseline %d",
				ledger.DDIObjects.Attributable, ledger.DDIObjects.Retained, got, ledger.DDIObjects.Baseline)
		}
	})
}

// TestMSLedger_DNS covers task 2's zone identity, duplicate collapse,
// ambiguity, and reason-code behavior through the full Scan pipeline.
func TestMSLedger_DNS(t *testing.T) {
	const zoneRef = ".1.dns.zone_ref_placeholder.default"
	const zoneRefOtherView = ".1.dns.zone_ref_placeholder.view2"
	const serverOID = "201"
	const otherServerOID = "202"

	scan := func(t *testing.T, objects ...string) (*niosscanner.MicrosoftAllocationLedger, niosscanner.MSAllocationDiagnostic) {
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

	t.Run("duplicate rows with the same server yield one attributed zone", func(t *testing.T) {
		ledger, diag := scan(t,
			msServerXML(serverOID),
			msServerDNSPropertiesXML(serverOID, "true"),
			zoneMSPrimaryServerXML(zoneRef, serverOID),
			zoneMSPrimaryServerXML(zoneRef, serverOID),
			bindARecordXML(zoneRef),
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.DDIObjects.Attributable != 1 {
			t.Errorf("Attributable = %d, want 1", ledger.DDIObjects.Attributable)
		}
		if ledger.Evidence.RelationshipRows != 2 {
			t.Errorf("RelationshipRows = %d, want 2", ledger.Evidence.RelationshipRows)
		}
		if ledger.Evidence.DuplicateRelationshipRows < 1 {
			t.Errorf("DuplicateRelationshipRows = %d, want >= 1", ledger.Evidence.DuplicateRelationshipRows)
		}
	})

	t.Run("zone references differing only in view segment are two distinct zones", func(t *testing.T) {
		ledger, diag := scan(t,
			msServerXML(serverOID),
			msServerDNSPropertiesXML(serverOID, "true"),
			zoneMSPrimaryServerXML(zoneRef, serverOID),
			zoneMSPrimaryServerXML(zoneRefOtherView, serverOID),
			bindARecordXML(zoneRef),
			bindARecordXML(zoneRefOtherView),
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.DDIObjects.Attributable != 2 {
			t.Errorf("Attributable = %d, want 2", ledger.DDIObjects.Attributable)
		}
	})

	t.Run("valid row plus sentinel row on the same zone still attributes once", func(t *testing.T) {
		ledger, diag := scan(t,
			msServerXML(serverOID),
			msServerDNSPropertiesXML(serverOID, "true"),
			zoneMSPrimaryServerXML(zoneRef, serverOID),
			zoneMSPrimaryServerXML(zoneRef, "."),
			bindARecordXML(zoneRef),
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.DDIObjects.Attributable != 1 {
			t.Errorf("Attributable = %d, want 1", ledger.DDIObjects.Attributable)
		}
		if ledger.Evidence.RelationshipAnomalies < 1 {
			t.Errorf("RelationshipAnomalies = %d, want >= 1", ledger.Evidence.RelationshipAnomalies)
		}
	})

	t.Run("secondary-only zone is retained with no review entry (D-17)", func(t *testing.T) {
		ledger, diag := scan(t,
			msServerXML(serverOID),
			msServerDNSPropertiesXML(serverOID, "true"),
			zoneMSSecondaryServerXML(zoneRef, serverOID),
			bindARecordXML(zoneRef),
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.DDIObjects.Attributable != 0 {
			t.Errorf("Attributable = %d, want 0", ledger.DDIObjects.Attributable)
		}
		if ledger.Evidence.RelationshipAnomalies != 0 {
			t.Errorf("RelationshipAnomalies = %d, want 0", ledger.Evidence.RelationshipAnomalies)
		}
	})

	for _, tc := range []struct {
		name    string
		objects []string
	}{
		{"duplicate rows", []string{msServerXML(serverOID), msServerDNSPropertiesXML(serverOID, "true"), zoneMSPrimaryServerXML(zoneRef, serverOID), zoneMSPrimaryServerXML(zoneRef, serverOID), bindARecordXML(zoneRef)}},
		{"sentinel-only", []string{msServerXML(serverOID), msServerDNSPropertiesXML(serverOID, "true"), zoneMSPrimaryServerXML(zoneRef, "."), bindARecordXML(zoneRef)}},
		{"secondary-only", []string{msServerXML(serverOID), msServerDNSPropertiesXML(serverOID, "true"), zoneMSSecondaryServerXML(zoneRef, serverOID), bindARecordXML(zoneRef)}},
	} {
		t.Run("checkMSConservation holds for the "+tc.name+" fixture", func(t *testing.T) {
			_, diag := scan(t, tc.objects...)
			if !diag.Available {
				t.Fatalf("ledger unavailable (conservation failed): %+v", diag)
			}
		})
	}
}

// TestMSLedger_DNSRecords covers task 3's per-record eligibility: an eligible
// typed DNS record consults its zone's attribution, an ineligible family
// (including host objects) is always ReasonUnsupportedType regardless of zone
// state, and a mixed zone conserves with both partitions non-zero.
func TestMSLedger_DNSRecords(t *testing.T) {
	const zoneRef = ".1.dns.zone_ref_placeholder.default"
	const unattributedZoneRef = ".1.dns.zone_ref_placeholder.no-relationship"
	const serverOID = "201"

	scan := func(t *testing.T, objects ...string) (*niosscanner.MicrosoftAllocationLedger, niosscanner.MSAllocationDiagnostic) {
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

	t.Run("eligible record under an attributed zone is attributable", func(t *testing.T) {
		ledger, diag := scan(t,
			msServerXML(serverOID),
			msServerDNSPropertiesXML(serverOID, "true"),
			zoneMSPrimaryServerXML(zoneRef, serverOID),
			bindARecordXML(zoneRef),
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.DDIObjects.Attributable != 1 {
			t.Errorf("Attributable = %d, want 1", ledger.DDIObjects.Attributable)
		}
		if ledger.DDIObjects.Retained != 0 {
			t.Errorf("Retained = %d, want 0", ledger.DDIObjects.Retained)
		}
	})

	t.Run("eligible record under a non-attributed zone is retained with no review entry", func(t *testing.T) {
		ledger, diag := scan(t,
			msServerXML(serverOID),
			msServerDNSPropertiesXML(serverOID, "true"),
			bindARecordXML(unattributedZoneRef), // no relationship row at all for this zone
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.DDIObjects.Retained != 1 {
			t.Errorf("Retained = %d, want 1", ledger.DDIObjects.Retained)
		}
	})

	t.Run("mixed zone conserves with both partitions non-zero and the zone's own attribution unaffected", func(t *testing.T) {
		ledger, diag := scan(t,
			msServerXML(serverOID),
			msServerDNSPropertiesXML(serverOID, "true"),
			zoneMSPrimaryServerXML(zoneRef, serverOID),
			zoneXML(zoneRef),        // the zone's own record, attributed via the relationship
			bindARecordXML(zoneRef), // eligible, attributed via the zone
			hostAliasXML(zoneRef),   // ineligible, always retained regardless of the zone
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		// zone object + bind_a record are both attributable; the zone stays
		// attributed exactly once despite the unsupported record sharing it.
		if ledger.DDIObjects.Attributable != 2 {
			t.Errorf("Attributable = %d, want 2", ledger.DDIObjects.Attributable)
		}
		if ledger.DDIObjects.Retained != 1 {
			t.Errorf("Retained = %d, want 1", ledger.DDIObjects.Retained)
		}
		if got := ledger.DDIObjects.Attributable + ledger.DDIObjects.Retained; got != ledger.DDIObjects.Baseline {
			t.Errorf("Attributable(%d) + Retained(%d) = %d, want Baseline %d",
				ledger.DDIObjects.Attributable, ledger.DDIObjects.Retained, got, ledger.DDIObjects.Baseline)
		}
	})

	t.Run("checkMSConservation holds for every fixture in this task", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			objects []string
		}{
			{"eligible-attributed", []string{msServerXML(serverOID), msServerDNSPropertiesXML(serverOID, "true"), zoneMSPrimaryServerXML(zoneRef, serverOID), bindARecordXML(zoneRef)}},
			{"eligible-unattributed", []string{msServerXML(serverOID), msServerDNSPropertiesXML(serverOID, "true"), bindARecordXML(unattributedZoneRef)}},
			{"ineligible", []string{msServerXML(serverOID), msServerDNSPropertiesXML(serverOID, "true"), zoneMSPrimaryServerXML(zoneRef, serverOID), hostAliasXML(zoneRef)}},
			{"mixed", []string{msServerXML(serverOID), msServerDNSPropertiesXML(serverOID, "true"), zoneMSPrimaryServerXML(zoneRef, serverOID), zoneXML(zoneRef), bindARecordXML(zoneRef), hostAliasXML(zoneRef)}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, diag := scan(t, tc.objects...)
				if !diag.Available {
					t.Fatalf("ledger unavailable (conservation failed): %+v", diag)
				}
			})
		}
	})
}

// ---- Task 1 (05-04): DHCP ownership fixture object blocks ----
//
// .com.infoblox.dns.network carries no ms_server property of its own
// (05-SCHEMA.md — 0/7306 occurrences on a Microsoft-unconfigured grid).
// Ownership is asserted only through a ms_dhcp_member relationship row, keyed
// by the same composite "address/cidr/view" string the network object itself
// carries as three separate properties (see dhcpNetworkResolutionKey in
// ms_ledger.go). dhcp_range carries its own direct ms_server reference and
// needs no relationship row at all.

// niosNetworkKey builds the composite key a network resolves under, for use
// as a ms_dhcp_member row's "network" property value in these fixtures.
func niosNetworkKey(address, cidr, view string) string {
	return address + "/" + cidr + "/" + view
}

func networkXML(address, cidr, view string) string {
	return `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.network"/>
<PROPERTY NAME="address" VALUE="` + address + `"/>
<PROPERTY NAME="cidr" VALUE="` + cidr + `"/>
<PROPERTY NAME="network_view" VALUE="` + view + `"/>
</OBJECT>`
}

func msServerDHCPPropertiesXML(oid, managed string) string {
	return `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.ms_server_dhcp_properties"/>
<PROPERTY NAME="parent" VALUE="` + oid + `"/>
<PROPERTY NAME="managed" VALUE="` + managed + `"/>
</OBJECT>`
}

func dhcpMemberXML(network, server string) string {
	return `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.ms_dhcp_member"/>
<PROPERTY NAME="network" VALUE="` + network + `"/>
<PROPERTY NAME="ms_server" VALUE="` + server + `"/>
</OBJECT>`
}

func dhcpRangeXML(server string) string {
	return `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.dhcp_range"/>
<PROPERTY NAME="ms_server" VALUE="` + server + `"/>
</OBJECT>`
}

func exclusionRangeXML() string {
	return `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.exclusion_range"/>
</OBJECT>`
}

// exclusionRangeXMLFull is Task 2's richer exclusion_range fixture, carrying
// the "parent" relationship reference and the "start_address"/"network_view"
// properties classifyDHCPExclusionRange reads. Pass "" for any property that
// a given test does not need — an absent XML property and a present-but-empty
// one parse identically into props[key].
func exclusionRangeXMLFull(parent, startAddress, view string) string {
	return `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.exclusion_range"/>
<PROPERTY NAME="parent" VALUE="` + parent + `"/>
<PROPERTY NAME="start_address" VALUE="` + startAddress + `"/>
<PROPERTY NAME="network_view" VALUE="` + view + `"/>
</OBJECT>`
}

// TestMSLedger_DHCPOwnership covers Task 1's DHCP-side attribution: a network
// object's relationship-only path through ms_dhcp_member (no direct
// ms_server property exists on the network itself, D-08), a dhcp_range
// object's direct ms_server reference, and the exclusion_range interim stub
// that Task 2's D-06 containment resolver will replace.
func TestMSLedger_DHCPOwnership(t *testing.T) {
	const address, cidr, view = "192.0.2.0", "24", "0"
	networkKey := niosNetworkKey(address, cidr, view)
	const serverOID = "301"
	const otherServerOID = "302"

	scan := func(t *testing.T, objects ...string) (*niosscanner.MicrosoftAllocationLedger, niosscanner.MSAllocationDiagnostic) {
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

	t.Run("network", func(t *testing.T) {
		t.Run("DHCP-managed server attributes the network", func(t *testing.T) {
			ledger, diag := scan(t,
				msServerXML(serverOID),
				msServerDHCPPropertiesXML(serverOID, "true"),
				dhcpMemberXML(networkKey, serverOID),
				networkXML(address, cidr, view),
			)
			if !diag.Available {
				t.Fatalf("ledger unavailable: %+v", diag)
			}
			if ledger.DDIObjects.Attributable != 1 {
				t.Errorf("Attributable = %d, want 1", ledger.DDIObjects.Attributable)
			}
			if ledger.DDIObjects.Retained != 0 {
				t.Errorf("Retained = %d, want 0", ledger.DDIObjects.Retained)
			}
		})

		t.Run("DHCP-unmanaged server (false) retains with a review entry", func(t *testing.T) {
			ledger, diag := scan(t,
				msServerXML(serverOID),
				msServerDHCPPropertiesXML(serverOID, "false"),
				dhcpMemberXML(networkKey, serverOID),
				networkXML(address, cidr, view),
			)
			if !diag.Available {
				t.Fatalf("ledger unavailable: %+v", diag)
			}
			if ledger.DDIObjects.Attributable != 0 {
				t.Errorf("Attributable = %d, want 0", ledger.DDIObjects.Attributable)
			}
		})

		t.Run("no relationship row at all is retained with no review entry", func(t *testing.T) {
			ledger, diag := scan(t,
				msServerXML(serverOID),
				msServerDHCPPropertiesXML(serverOID, "true"),
				networkXML(address, cidr, view),
			)
			if !diag.Available {
				t.Fatalf("ledger unavailable: %+v", diag)
			}
			if ledger.DDIObjects.Retained != 1 {
				t.Errorf("Retained = %d, want 1", ledger.DDIObjects.Retained)
			}
		})

		t.Run("duplicate rows naming the same server attribute once", func(t *testing.T) {
			ledger, diag := scan(t,
				msServerXML(serverOID),
				msServerDHCPPropertiesXML(serverOID, "true"),
				dhcpMemberXML(networkKey, serverOID),
				dhcpMemberXML(networkKey, serverOID),
				networkXML(address, cidr, view),
			)
			if !diag.Available {
				t.Fatalf("ledger unavailable: %+v", diag)
			}
			if ledger.DDIObjects.Attributable != 1 {
				t.Errorf("Attributable = %d, want 1", ledger.DDIObjects.Attributable)
			}
			if ledger.Evidence.RelationshipRows != 2 {
				t.Errorf("RelationshipRows = %d, want 2", ledger.Evidence.RelationshipRows)
			}
			if ledger.Evidence.DuplicateRelationshipRows < 1 {
				t.Errorf("DuplicateRelationshipRows = %d, want >= 1", ledger.Evidence.DuplicateRelationshipRows)
			}
		})

		t.Run("two networks with different composite keys attribute independently", func(t *testing.T) {
			ledger, diag := scan(t,
				msServerXML(serverOID),
				msServerDHCPPropertiesXML(serverOID, "true"),
				dhcpMemberXML(networkKey, serverOID),
				networkXML(address, cidr, view),
				networkXML("198.51.100.0", cidr, view),
			)
			if !diag.Available {
				t.Fatalf("ledger unavailable: %+v", diag)
			}
			if ledger.DDIObjects.Attributable != 1 {
				t.Errorf("Attributable = %d, want 1 (only the network with a relationship row)", ledger.DDIObjects.Attributable)
			}
			if ledger.DDIObjects.Retained != 1 {
				t.Errorf("Retained = %d, want 1", ledger.DDIObjects.Retained)
			}
		})
	})

	t.Run("dhcp_range", func(t *testing.T) {
		t.Run("valid managed server attributes the range", func(t *testing.T) {
			ledger, diag := scan(t,
				msServerXML(serverOID),
				msServerDHCPPropertiesXML(serverOID, "true"),
				dhcpRangeXML(serverOID),
			)
			if !diag.Available {
				t.Fatalf("ledger unavailable: %+v", diag)
			}
			if ledger.DDIObjects.Attributable != 1 {
				t.Errorf("Attributable = %d, want 1", ledger.DDIObjects.Attributable)
			}
		})

		t.Run("sentinel own reference is retained with no review entry (not claimed)", func(t *testing.T) {
			ledger, diag := scan(t,
				msServerXML(serverOID),
				msServerDHCPPropertiesXML(serverOID, "true"),
				dhcpRangeXML("."),
			)
			if !diag.Available {
				t.Fatalf("ledger unavailable: %+v", diag)
			}
			if ledger.DDIObjects.Retained != 1 {
				t.Errorf("Retained = %d, want 1", ledger.DDIObjects.Retained)
			}
		})

	})

	t.Run("checkMSConservation holds for every fixture in this task", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			objects []string
		}{
			{"network-attributed", []string{msServerXML(serverOID), msServerDHCPPropertiesXML(serverOID, "true"), dhcpMemberXML(networkKey, serverOID), networkXML(address, cidr, view)}},
			{"network-ambiguous", []string{msServerXML(serverOID), msServerXML(otherServerOID), msServerDHCPPropertiesXML(serverOID, "true"), msServerDHCPPropertiesXML(otherServerOID, "true"), dhcpMemberXML(networkKey, serverOID), dhcpMemberXML(networkKey, otherServerOID), networkXML(address, cidr, view)}},
			{"range-attributed", []string{msServerXML(serverOID), msServerDHCPPropertiesXML(serverOID, "true"), dhcpRangeXML(serverOID)}},
			{"range-sentinel", []string{msServerXML(serverOID), msServerDHCPPropertiesXML(serverOID, "true"), dhcpRangeXML(".")}},
			{"exclusion-range", []string{msServerXML(serverOID), msServerDHCPPropertiesXML(serverOID, "true"), exclusionRangeXML()}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, diag := scan(t, tc.objects...)
				if !diag.Available {
					t.Fatalf("ledger unavailable (conservation failed): %+v", diag)
				}
			})
		}
	})
}

// TestMSLedger_DHCPContainmentIntegration covers Task 2's D-06 containment
// fallback end to end through Scan, for the behavior bullets reachable with
// real, geometrically-consistent network objects (equal-length CIDR blocks
// of the same view can never overlap for two distinct networks, so the
// equal-prefix-ambiguity bullet is unit-tested only, against a directly
// constructed resolver — see TestMSLedger_DHCPContainment in
// ms_ledger_test.go).
func TestMSLedger_DHCPContainmentIntegration(t *testing.T) {
	const serverOID = "401"
	const view = "0"
	const address, cidr = "192.0.2.0", "24"
	networkKey := niosNetworkKey(address, cidr, view)

	scan := func(t *testing.T, objects ...string) (*niosscanner.MicrosoftAllocationLedger, niosscanner.MSAllocationDiagnostic) {
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

	msNetwork := func() []string {
		return []string{
			msServerXML(serverOID),
			msServerDHCPPropertiesXML(serverOID, "true"),
			dhcpMemberXML(networkKey, serverOID),
			networkXML(address, cidr, view),
		}
	}

	t.Run("address inside the verified network in the same view is attributed via containment", func(t *testing.T) {
		objects := append(msNetwork(), exclusionRangeXMLFull("", "192.0.2.5", view))
		ledger, diag := scan(t, objects...)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		// network (attributed) + exclusion range (attributed via containment).
		if ledger.DDIObjects.Attributable != 2 {
			t.Errorf("Attributable = %d, want 2", ledger.DDIObjects.Attributable)
		}
	})

	t.Run("address inside the verified network but a different view is retained as ContainmentUnverified", func(t *testing.T) {
		objects := append(msNetwork(), exclusionRangeXMLFull("", "192.0.2.5", "other-view"))
		ledger, diag := scan(t, objects...)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.DDIObjects.Attributable != 1 {
			t.Errorf("Attributable = %d, want 1 (network only)", ledger.DDIObjects.Attributable)
		}
	})

	t.Run("address outside the verified network is retained with no review entry", func(t *testing.T) {
		objects := append(msNetwork(), exclusionRangeXMLFull("", "198.51.100.5", view))
		ledger, diag := scan(t, objects...)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.DDIObjects.Attributable != 1 {
			t.Errorf("Attributable = %d, want 1 (network only)", ledger.DDIObjects.Attributable)
		}
	})

	t.Run("a narrower nested network wins over a wider covering one", func(t *testing.T) {
		wideKey := niosNetworkKey(address, "16", view)
		objects := []string{
			msServerXML(serverOID),
			msServerDHCPPropertiesXML(serverOID, "true"),
			dhcpMemberXML(wideKey, serverOID),
			networkXML(address, "16", view),
			dhcpMemberXML(networkKey, serverOID),
			networkXML(address, cidr, view),
			exclusionRangeXMLFull("", "192.0.2.5", view),
		}
		ledger, diag := scan(t, objects...)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		// both networks attributed + exclusion range attributed via the /24.
		if ledger.DDIObjects.Attributable != 3 {
			t.Errorf("Attributable = %d, want 3", ledger.DDIObjects.Attributable)
		}
	})

	t.Run("a parent exact match attributes without ever consulting containment", func(t *testing.T) {
		parentRef := ".com.infoblox.dns.network$" + networkKey
		// Deliberately no start_address/network_view at all — if containment
		// were consulted with an empty address it would retain-unverified;
		// the parent exact match must short-circuit before that.
		objects := append(msNetwork(), exclusionRangeXMLFull(parentRef, "", ""))
		ledger, diag := scan(t, objects...)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.DDIObjects.Attributable != 2 {
			t.Errorf("Attributable = %d, want 2 (network + exclusion range via parent match)", ledger.DDIObjects.Attributable)
		}
	})

	t.Run("dhcp_range attributes via its own reference even when the containment index is empty", func(t *testing.T) {
		ledger, diag := scan(t,
			msServerXML(serverOID),
			msServerDHCPPropertiesXML(serverOID, "true"),
			dhcpRangeXML(serverOID),
			// No ms_dhcp_member / network objects at all: dhcpNetworkResolution,
			// and therefore the containment resolver, is built empty.
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.DDIObjects.Attributable != 1 {
			t.Errorf("Attributable = %d, want 1 (direct reference never depends on containment)", ledger.DDIObjects.Attributable)
		}
	})

	t.Run("checkMSConservation holds for every fixture in this task", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			objects []string
		}{
			{"same-view-attributed", append(msNetwork(), exclusionRangeXMLFull("", "192.0.2.5", view))},
			{"different-view-unverified", append(msNetwork(), exclusionRangeXMLFull("", "192.0.2.5", "other-view"))},
			{"missing-view-unverified", append(msNetwork(), exclusionRangeXMLFull("", "192.0.2.5", ""))},
			{"outside-boundary-silent", append(msNetwork(), exclusionRangeXMLFull("", "198.51.100.5", view))},
			{"parent-exact-match", append(msNetwork(), exclusionRangeXMLFull(".com.infoblox.dns.network$"+networkKey, "", ""))},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, diag := scan(t, tc.objects...)
				if !diag.Available {
					t.Fatalf("ledger unavailable (conservation failed): %+v", diag)
				}
			})
		}
	})
}

// ---- Microsoft ledger fixture object blocks (Task 3: Active-IP union) ----

// fixedAddressXML is a fixed_address object carrying its own direct
// ms_server reference (05-SCHEMA.md: fixed_address.ms_server).
func fixedAddressXML(ipAddress, server string) string {
	return `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.fixed_address"/>
<PROPERTY NAME="ip_address" VALUE="` + ipAddress + `"/>
<PROPERTY NAME="ms_server" VALUE="` + server + `"/>
</OBJECT>`
}

// leaseXML is a lease object carrying its own direct ms_server_id reference
// (05-SCHEMA.md: lease.ms_server_id — a different property name than
// fixed_address's ms_server).
func leaseXML(ipAddress, bindingState, server string) string {
	return `<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.lease"/>
<PROPERTY NAME="ip_address" VALUE="` + ipAddress + `"/>
<PROPERTY NAME="binding_state" VALUE="` + bindingState + `"/>
<PROPERTY NAME="ms_server_id" VALUE="` + server + `"/>
</OBJECT>`
}

// TestMSLedger_DHCP covers Task 3's D-07 Active-IP union with failover
// collapse end to end through Scan: fixed_address and lease objects
// attribute through their own direct reference (never containment), deduped
// per category first (tier 1, mirroring counter.go's own
// fixedIPSet/activeLeaseIPSet) and then unioned across categories (tier 2),
// so a same-category failover replica credits nothing while a cross-category
// fixed-and-active duplicate surfaces as a retained surplus rather than
// vanishing.
func TestMSLedger_DHCP(t *testing.T) {
	const serverOID = "501"
	const ip = "192.0.2.5"

	scan := func(t *testing.T, objects ...string) (*niosscanner.MicrosoftAllocationLedger, niosscanner.MSAllocationDiagnostic) {
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

	msServer := func() []string {
		return []string{msServerXML(serverOID), msServerDHCPPropertiesXML(serverOID, "true")}
	}

	assertActiveIPs := func(t *testing.T, ledger *niosscanner.MicrosoftAllocationLedger, wantAttributable, wantRetained int) {
		t.Helper()
		if ledger.ActiveIPs.Attributable != wantAttributable {
			t.Errorf("Attributable = %d, want %d", ledger.ActiveIPs.Attributable, wantAttributable)
		}
		if ledger.ActiveIPs.Retained != wantRetained {
			t.Errorf("Retained = %d, want %d", ledger.ActiveIPs.Retained, wantRetained)
		}
		if ledger.ActiveIPs.Baseline != ledger.ActiveIPs.Attributable+ledger.ActiveIPs.Retained {
			t.Errorf("Baseline = %d, want Attributable(%d)+Retained(%d) = %d",
				ledger.ActiveIPs.Baseline, ledger.ActiveIPs.Attributable, ledger.ActiveIPs.Retained,
				ledger.ActiveIPs.Attributable+ledger.ActiveIPs.Retained)
		}
		if ledger.ManagedAssets != (niosscanner.MSCategoryPartition{}) {
			t.Errorf("ManagedAssets = %+v, want all zero", ledger.ManagedAssets)
		}
	}

	t.Run("a Microsoft-verified fixed reservation contributes exactly one address", func(t *testing.T) {
		ledger, diag := scan(t, append(msServer(), fixedAddressXML(ip, serverOID))...)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		assertActiveIPs(t, ledger, 1, 0)
	})

	t.Run("a Microsoft-verified active lease contributes exactly one address", func(t *testing.T) {
		ledger, diag := scan(t, append(msServer(), leaseXML(ip, "active", serverOID))...)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		// Attributable 1 (the union) + Retained 1 (the corporate +1
		// active-lease off-by-one, present because an active lease exists).
		assertActiveIPs(t, ledger, 1, 1)
	})

	t.Run("the same address as both a fixed reservation and an active lease unions to one, surplus in retained", func(t *testing.T) {
		ledger, diag := scan(t, append(msServer(), fixedAddressXML(ip, serverOID), leaseXML(ip, "active", serverOID))...)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		// Retained 2: the cross-category union-surplus (the lease's copy of
		// the address, already claimed by the fixed reservation) plus the
		// corporate +1 active-lease off-by-one. Contrast against the
		// failover-replica case below, where the second unit is a
		// SAME-category duplicate and Retained is 1, not 2.
		assertActiveIPs(t, ledger, 1, 2)
	})

	t.Run("a lease replicated onto two failover peers unions to one, second replica credits nothing", func(t *testing.T) {
		ledger, diag := scan(t, append(msServer(), leaseXML(ip, "active", serverOID), leaseXML(ip, "active", serverOID))...)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		// Retained is the corporate +1 off-by-one ALONE (no network here, so
		// the 2*networkCount() term contributes 0) — the second replica is a
		// SAME-category duplicate and credits neither partition, unlike the
		// cross-category surplus case above where Retained was 2.
		assertActiveIPs(t, ledger, 1, 1)
	})

	t.Run("two fixed reservations at the same address behave identically: the second credits nothing", func(t *testing.T) {
		ledger, diag := scan(t, append(msServer(), fixedAddressXML(ip, serverOID), fixedAddressXML(ip, serverOID))...)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		assertActiveIPs(t, ledger, 1, 0)
	})

	t.Run("a lease whose binding state is not active contributes nothing", func(t *testing.T) {
		ledger, diag := scan(t, append(msServer(), leaseXML(ip, "free", serverOID))...)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		assertActiveIPs(t, ledger, 0, 0)
	})

	t.Run("a network and a Microsoft server present but zero relationship rows conserves and is not suppressed", func(t *testing.T) {
		ledger, diag := scan(t, append(msServer(), networkXML("192.0.2.0", "24", "0"))...)
		if !diag.Available {
			t.Fatalf("ledger unavailable (conservation failed): %+v", diag)
		}
		assertActiveIPs(t, ledger, 0, 2)
	})

	t.Run("checkMSConservation holds for every fixture in this task", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			objects []string
		}{
			{"fixed-attributed", append(msServer(), fixedAddressXML(ip, serverOID))},
			{"lease-attributed", append(msServer(), leaseXML(ip, "active", serverOID))},
			{"cross-category-surplus", append(msServer(), fixedAddressXML(ip, serverOID), leaseXML(ip, "active", serverOID))},
			{"failover-replica", append(msServer(), leaseXML(ip, "active", serverOID), leaseXML(ip, "active", serverOID))},
			{"duplicate-fixed", append(msServer(), fixedAddressXML(ip, serverOID), fixedAddressXML(ip, serverOID))},
			{"non-active-lease", append(msServer(), leaseXML(ip, "free", serverOID))},
			{"sentinel-fixed", append(msServer(), fixedAddressXML(ip, "."))},
			{"unmatched-fixed", append(msServer(), fixedAddressXML(ip, "999"))},
			{"network-only", append(msServer(), networkXML("192.0.2.0", "24", "0"))},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, diag := scan(t, tc.objects...)
				if !diag.Available {
					t.Fatalf("ledger unavailable (conservation failed): %+v", diag)
				}
			})
		}
	})
}

// ---- Task 1 (05-05): full-surface order-invariance fixture ----

// msLedgerFullSurfaceObjects returns ONE shared slice of object-block strings
// exercising every phase-5 attribution path: two DNS/DHCP dual-managed
// servers, a duplicated DNS primary relationship plus a secondary-only
// relationship on the same zone (D-17 evidence, never a claim), a second
// zone differing only in its view segment, one eligible and one ineligible
// DNS record, a duplicated DHCP network membership, a direct-reference
// dhcp_range, a containment-resolved exclusion_range, a direct-reference
// fixed_address, a two-member failover lease replica, and one native
// sentinel-on-own-reference object. TestMSLedger_Deterministic reorders this
// SAME slice for its order-invariance assertions rather than maintaining a
// second full-fixture XML constant (05-05-PLAN.md Task 1 acceptance).
func msLedgerFullSurfaceObjects() []string {
	const (
		serverDNS  = "501"
		serverDHCP = "502"
		zoneA      = ".1.dns.zone_ref_placeholder.default"
		zoneB      = ".1.dns.zone_ref_placeholder.view2"
	)
	const address, cidr, view = "192.0.2.0", "24", "0"
	netKey := niosNetworkKey(address, cidr, view)

	return []string{
		msServerXML(serverDNS),
		msServerDNSPropertiesXML(serverDNS, "true"),
		msServerDHCPPropertiesXML(serverDNS, "true"),
		msServerXML(serverDHCP),
		msServerDNSPropertiesXML(serverDHCP, "true"),
		msServerDHCPPropertiesXML(serverDHCP, "true"),
		zoneMSPrimaryServerXML(zoneA, serverDNS),
		zoneMSPrimaryServerXML(zoneA, serverDNS),    // duplicate row (D-04)
		zoneMSSecondaryServerXML(zoneA, serverDHCP), // D-17: evidence only, never a claim
		zoneXML(zoneA),
		bindARecordXML(zoneA), // eligible DNS record
		zoneMSPrimaryServerXML(zoneB, serverDNS),
		dhcpMemberXML(netKey, serverDHCP),
		dhcpMemberXML(netKey, serverDHCP), // duplicate row (D-04)
		networkXML(address, cidr, view),
		dhcpRangeXML(serverDHCP),                     // direct ms_server reference
		exclusionRangeXMLFull("", "192.0.2.5", view), // D-06 containment fallback
		fixedAddressXML("192.0.2.9", serverDHCP),     // direct ms_server reference
		leaseXML("192.0.2.10", "active", serverDHCP), // failover peer 1
		leaseXML("192.0.2.10", "active", serverDHCP), // failover peer 2 (replica)
		fixedAddressXML("192.0.2.20", "."),           // native sentinel own-reference
	}
}

// TestMSLedger_Deterministic proves order-invariance (05-05-PLAN.md Task 1):
// the same logical objects, presented to the scanner in a different onedb.xml
// order, must produce byte-identical ledgers. This is a regression LOCK on
// the architecture scanner.go already establishes — buildMSDNSResolver,
// buildMSDHCPResolver, and buildMSContainmentResolver all run exactly once,
// after Pass 1 has fully populated every raw relationship map and before
// Pass 2 ever classifies an object — not a search for a bug expected to
// exist. Every value MicrosoftAllocationLedger exposes is a struct of
// scalars, so reflect.DeepEqual on the whole ledger is a valid comparison
// with no per-field ordering to audit here.
func TestMSLedger_Deterministic(t *testing.T) {
	scanOnce := func(t *testing.T, s *niosscanner.Scanner, objects ...string) *niosscanner.MicrosoftAllocationLedger {
		t.Helper()
		path := writeMSLedgerBackup(t, msLedgerXML(objects...))
		if _, err := s.Scan(context.Background(), scanner.ScanRequest{
			Provider:    "nios",
			Credentials: map[string]string{"backup_path": path},
		}, func(scanner.Event) {}); err != nil {
			t.Fatalf("Scan error: %v", err)
		}
		ledger, diag := s.MicrosoftLedgerForTest()
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		return ledger
	}

	t.Run("full-surface fixture is order-invariant", func(t *testing.T) {
		forward := msLedgerFullSurfaceObjects()
		backward := make([]string, len(forward))
		for i, obj := range forward {
			backward[len(forward)-1-i] = obj
		}

		ledgerForward := scanOnce(t, niosscanner.New(), forward...)
		ledgerBackward := scanOnce(t, niosscanner.New(), backward...)

		if !reflect.DeepEqual(ledgerForward, ledgerBackward) {
			t.Errorf("ledger differs by XML order:\nforward:  %+v\nbackward: %+v", ledgerForward, ledgerBackward)
		}
		// Sanity: the fixture must actually exercise both partitions, or an
		// empty-ledger bug could pass this comparison vacuously.
		if ledgerForward.DDIObjects.Attributable == 0 || ledgerForward.ActiveIPs.Attributable == 0 {
			t.Fatalf("fixture produced a degenerate ledger: %+v", ledgerForward)
		}
	})

	t.Run("DNS reference-before-definition: zone_ms_primary_server before its ms_server", func(t *testing.T) {
		const zone, server = ".1.dns.zone_ref_placeholder.refdef", "601"
		natural := []string{
			msServerXML(server),
			msServerDNSPropertiesXML(server, "true"),
			zoneMSPrimaryServerXML(zone, server),
			bindARecordXML(zone),
		}
		refFirst := []string{
			zoneMSPrimaryServerXML(zone, server),
			bindARecordXML(zone),
			msServerXML(server),
			msServerDNSPropertiesXML(server, "true"),
		}

		ledgerNatural := scanOnce(t, niosscanner.New(), natural...)
		ledgerRefFirst := scanOnce(t, niosscanner.New(), refFirst...)

		if !reflect.DeepEqual(ledgerNatural, ledgerRefFirst) {
			t.Errorf("ledger differs when zone_ms_primary_server precedes its ms_server:\nnatural:   %+v\nref-first: %+v", ledgerNatural, ledgerRefFirst)
		}
		if ledgerNatural.DDIObjects.Attributable != 1 {
			t.Fatalf("Attributable = %d, want 1 (both orders must attribute)", ledgerNatural.DDIObjects.Attributable)
		}
	})

	t.Run("DHCP reference-before-definition: ms_dhcp_member before its ms_server", func(t *testing.T) {
		const address, cidr, view = "203.0.113.0", "24", "0"
		const server = "602"
		netKey := niosNetworkKey(address, cidr, view)
		natural := []string{
			msServerXML(server),
			msServerDHCPPropertiesXML(server, "true"),
			dhcpMemberXML(netKey, server),
			networkXML(address, cidr, view),
		}
		refFirst := []string{
			dhcpMemberXML(netKey, server),
			networkXML(address, cidr, view),
			msServerXML(server),
			msServerDHCPPropertiesXML(server, "true"),
		}

		ledgerNatural := scanOnce(t, niosscanner.New(), natural...)
		ledgerRefFirst := scanOnce(t, niosscanner.New(), refFirst...)

		if !reflect.DeepEqual(ledgerNatural, ledgerRefFirst) {
			t.Errorf("ledger differs when ms_dhcp_member precedes its ms_server:\nnatural:   %+v\nref-first: %+v", ledgerNatural, ledgerRefFirst)
		}
		if ledgerNatural.DDIObjects.Attributable != 1 {
			t.Fatalf("Attributable = %d, want 1 (both orders must attribute)", ledgerNatural.DDIObjects.Attributable)
		}
	})

	t.Run("DHCP reference-before-definition: dhcp_range's ms_server before ms_server_dhcp_properties declares it managed", func(t *testing.T) {
		// The managed flag lives on a DIFFERENT object (ms_server_dhcp_properties)
		// than the reference itself (dhcp_range.ms_server) — the named hazard
		// case: dhcp_range must attribute identically whether or not it
		// appears before the object declaring its referenced server managed.
		const server = "603"
		natural := []string{
			msServerXML(server),
			msServerDHCPPropertiesXML(server, "true"),
			dhcpRangeXML(server),
		}
		refFirst := []string{
			dhcpRangeXML(server),
			msServerXML(server),
			msServerDHCPPropertiesXML(server, "true"),
		}

		ledgerNatural := scanOnce(t, niosscanner.New(), natural...)
		ledgerRefFirst := scanOnce(t, niosscanner.New(), refFirst...)

		if !reflect.DeepEqual(ledgerNatural, ledgerRefFirst) {
			t.Errorf("ledger differs when dhcp_range precedes ms_server_dhcp_properties:\nnatural:   %+v\nref-first: %+v", ledgerNatural, ledgerRefFirst)
		}
		if ledgerNatural.DDIObjects.Attributable != 1 {
			t.Fatalf("Attributable = %d, want 1 (both orders must attribute)", ledgerNatural.DDIObjects.Attributable)
		}
	})

	t.Run("same-process double scan on one Scanner instance is deterministic", func(t *testing.T) {
		objects := msLedgerFullSurfaceObjects()
		s := niosscanner.New()
		first := scanOnce(t, s, objects...)
		second := scanOnce(t, s, objects...)
		if !reflect.DeepEqual(first, second) {
			t.Errorf("ledger differs across two Scan calls on the same Scanner instance:\nfirst:  %+v\nsecond: %+v", first, second)
		}
	})

	t.Run("two independent Scanner instances on the same fixture agree", func(t *testing.T) {
		objects := msLedgerFullSurfaceObjects()
		ledgerA := scanOnce(t, niosscanner.New(), objects...)
		ledgerB := scanOnce(t, niosscanner.New(), objects...)
		if !reflect.DeepEqual(ledgerA, ledgerB) {
			t.Errorf("ledger differs across two independent Scanner instances:\nA: %+v\nB: %+v", ledgerA, ledgerB)
		}
	})
}

// ---- Task 2 (05-05): conservation-failure suppression through Scan ----

// TestMSLedger_ConservationSuppressionIntegration proves 05-05-PLAN.md Task
// 2's central claim end to end through Scan: when the conservation gate
// fails, the Microsoft allocation ledger is suppressed (D-16) but the
// baseline NIOS scan results are completely unaffected. A genuinely
// inconsistent fixture cannot be constructed through the real
// object-classification pipeline — every classify* method in ms_ledger.go
// credits exactly one partition per baseline unit, by construction — so this
// test uses SetMSConservationCheckForTest (ms_ledger_test.go, package nios,
// test-only) to force the gate to fail on an otherwise perfectly legitimate
// backup, isolating exactly one variable: whether the gate passes or fails.
func TestMSLedger_ConservationSuppressionIntegration(t *testing.T) {
	const zoneRef, server = ".1.dns.zone_ref_placeholder.suppression", "701"
	xmlBody := msLedgerXML(
		msServerXML(server),
		msServerDNSPropertiesXML(server, "true"),
		zoneMSPrimaryServerXML(zoneRef, server),
		bindARecordXML(zoneRef),
	)

	// Scan deletes backup_path after processing (single-use upload semantics),
	// so each run needs its own copy of the identical XML body — reusing one
	// path across two Scan calls would fail the second with ENOENT.
	runScan := func(t *testing.T) ([]calculator.FindingRow, *niosscanner.MicrosoftAllocationLedger, niosscanner.MSAllocationDiagnostic) {
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
		ledger, diag := s.MicrosoftLedgerForTest()
		return rows, ledger, diag
	}

	healthyRows, healthyLedger, healthyDiag := runScan(t)
	if !healthyDiag.Available || healthyLedger == nil {
		t.Fatalf("healthy run: ledger unavailable: %+v", healthyDiag)
	}

	restore := niosscanner.SetMSConservationCheckForTest(func(*niosscanner.MicrosoftAllocationLedger) []string {
		return []string{"DDI Objects"} // force a failure regardless of the real (conserving) totals
	})
	t.Cleanup(restore)

	suppressedRows, suppressedLedger, suppressedDiag := runScan(t)

	if suppressedLedger != nil {
		t.Errorf("suppressed run: ledger = %+v, want nil", suppressedLedger)
	}
	if suppressedDiag.Available {
		t.Errorf("suppressed run: diag.Available = true, want false")
	}
	if suppressedDiag.Code != niosscanner.MSAllocationUnavailableCode {
		t.Errorf("suppressed run: diag.Code = %q, want %q", suppressedDiag.Code, niosscanner.MSAllocationUnavailableCode)
	}

	if !reflect.DeepEqual(healthyRows, suppressedRows) {
		t.Errorf("baseline NIOS rows differ between the healthy and suppressed runs of the SAME fixture:\nhealthy:    %+v\nsuppressed: %+v", healthyRows, suppressedRows)
	}
	if len(healthyRows) == 0 {
		t.Fatalf("fixture produced zero baseline rows — this proves nothing about D-16 baseline preservation")
	}
}

// ---- Task 3 (05-05): evidence metrics reported separately from token-bearing partitions ----

// TestMSLedger_Evidence covers 05-05-PLAN.md Task 3's behavior bullets that
// survive the held-back removal: D-15 evidence never enters conservation
// arithmetic, a coexisting broken row on an already-attributed resource
// credits evidence only (D-12), duplicate rows are visible as evidence
// without moving partitions, and a zone_ms_secondary_server row is
// evidence-only (D-17).
func TestMSLedger_Evidence(t *testing.T) {
	scan := func(t *testing.T, objects ...string) (*niosscanner.MicrosoftAllocationLedger, niosscanner.MSAllocationDiagnostic) {
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

	t.Run("relationship rows outnumber unique resources, conservation still passes", func(t *testing.T) {
		const zoneRef, server = ".1.dns.zone_ref_placeholder.evidence-rows", "710"
		ledger, diag := scan(t,
			msServerXML(server),
			msServerDNSPropertiesXML(server, "true"),
			zoneMSPrimaryServerXML(zoneRef, server),
			zoneMSPrimaryServerXML(zoneRef, server), // duplicate #1
			zoneMSPrimaryServerXML(zoneRef, server), // duplicate #2
			zoneXML(zoneRef),
			bindARecordXML(zoneRef),
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.Evidence.RelationshipRows != 3 {
			t.Errorf("Evidence.RelationshipRows = %d, want 3", ledger.Evidence.RelationshipRows)
		}
		attributableSum := ledger.DDIObjects.Attributable + ledger.ActiveIPs.Attributable + ledger.ManagedAssets.Attributable
		if attributableSum != 2 {
			t.Fatalf("attributableSum = %d, want 2 (the zone plus the A record)", attributableSum)
		}
		if ledger.Evidence.RelationshipRows <= attributableSum {
			t.Errorf("Evidence.RelationshipRows(%d) not strictly greater than attributableSum(%d) — evidence must sit outside the arithmetic", ledger.Evidence.RelationshipRows, attributableSum)
		}
	})

	t.Run("a broken row coexisting with a valid claim credits evidence only, never review (D-12)", func(t *testing.T) {
		const zoneRef, server = ".1.dns.zone_ref_placeholder.evidence-d12", "713"
		ledger, diag := scan(t,
			msServerXML(server),
			msServerDNSPropertiesXML(server, "true"),
			zoneMSPrimaryServerXML(zoneRef, server), // valid claim
			zoneMSPrimaryServerXML(zoneRef, "."),    // sentinel — broken row on the SAME zone
			zoneXML(zoneRef),
			bindARecordXML(zoneRef),
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.DDIObjects.Attributable != 2 {
			t.Errorf("Attributable = %d, want 2 (zone stays attributed despite the coexisting broken row)", ledger.DDIObjects.Attributable)
		}
		if ledger.Evidence.RelationshipAnomalies != 1 {
			t.Errorf("Evidence.RelationshipAnomalies = %d, want 1", ledger.Evidence.RelationshipAnomalies)
		}
	})

	t.Run("duplicate relationship rows show up in evidence without moving partitions", func(t *testing.T) {
		const zoneRef, server = ".1.dns.zone_ref_placeholder.evidence-duplicate", "714"
		fixedObjects := []string{
			msServerXML(server),
			msServerDNSPropertiesXML(server, "true"),
			zoneXML(zoneRef),
			bindARecordXML(zoneRef),
		}
		withDuplicate := append(append([]string{}, fixedObjects...),
			zoneMSPrimaryServerXML(zoneRef, server),
			zoneMSPrimaryServerXML(zoneRef, server), // duplicate of the row above
		)
		withoutDuplicate := append(append([]string{}, fixedObjects...),
			zoneMSPrimaryServerXML(zoneRef, server),
		)

		dup, dupDiag := scan(t, withDuplicate...)
		plain, plainDiag := scan(t, withoutDuplicate...)
		if !dupDiag.Available || !plainDiag.Available {
			t.Fatalf("ledger unavailable: dup=%+v plain=%+v", dupDiag, plainDiag)
		}

		if dup.Evidence.DuplicateRelationshipRows != 1 {
			t.Errorf("with duplicate: DuplicateRelationshipRows = %d, want 1", dup.Evidence.DuplicateRelationshipRows)
		}
		if plain.Evidence.DuplicateRelationshipRows != 0 {
			t.Errorf("without duplicate: DuplicateRelationshipRows = %d, want 0", plain.Evidence.DuplicateRelationshipRows)
		}
		if dup.DDIObjects != plain.DDIObjects {
			t.Errorf("DDIObjects differ between duplicate and non-duplicate fixtures: dup=%+v plain=%+v", dup.DDIObjects, plain.DDIObjects)
		}
	})

	t.Run("an all-sentinel-native grid produces no review noise", func(t *testing.T) {
		// One ms_server object so the ledger is not Absent; every other
		// object's OWN reference is the sentinel — this is the guard
		// against review counts scaling with the size of a
		// Microsoft-free grid.
		const zoneRef, server = ".1.dns.zone_ref_placeholder.evidence-all-native", "716"
		ledger, diag := scan(t,
			msServerXML(server),
			zoneXML(zoneRef),        // no relationship row at all for this zone
			bindARecordXML(zoneRef), // ditto
			dhcpRangeXML("."),
			fixedAddressXML("192.0.2.20", "."),
			leaseXML("192.0.2.21", "active", "."),
		)
		if !diag.Available {
			t.Fatalf("ledger unavailable: %+v", diag)
		}
		if ledger.Evidence.RelationshipAnomalies != 0 {
			t.Errorf("Evidence.RelationshipAnomalies = %d, want 0", ledger.Evidence.RelationshipAnomalies)
		}
	})

	t.Run("a zone_ms_secondary_server row moves nothing but RelationshipRows (D-17)", func(t *testing.T) {
		const zoneRef, server = ".1.dns.zone_ref_placeholder.evidence-secondary", "717"
		fixedObjects := []string{
			msServerXML(server),
			msServerDNSPropertiesXML(server, "true"),
			hostObjectXML(zoneRef),
		}
		withSecondary := append(append([]string{}, fixedObjects...), zoneMSSecondaryServerXML(zoneRef, server))
		withoutSecondary := append([]string{}, fixedObjects...)

		with, withDiag := scan(t, withSecondary...)
		without, withoutDiag := scan(t, withoutSecondary...)
		if !withDiag.Available || !withoutDiag.Available {
			t.Fatalf("ledger unavailable: with=%+v without=%+v", withDiag, withoutDiag)
		}

		if with.DDIObjects != without.DDIObjects {
			t.Errorf("DDIObjects differ: with=%+v without=%+v", with.DDIObjects, without.DDIObjects)
		}
		if with.Evidence.RelationshipAnomalies != 0 || without.Evidence.RelationshipAnomalies != 0 {
			t.Errorf("RelationshipAnomalies: with=%d without=%d, want 0 for both", with.Evidence.RelationshipAnomalies, without.Evidence.RelationshipAnomalies)
		}
		if with.Evidence.RelationshipRows != 1 {
			t.Errorf("with secondary row: RelationshipRows = %d, want 1", with.Evidence.RelationshipRows)
		}
		if without.Evidence.RelationshipRows != 0 {
			t.Errorf("without secondary row: RelationshipRows = %d, want 0", without.Evidence.RelationshipRows)
		}
	})
}
