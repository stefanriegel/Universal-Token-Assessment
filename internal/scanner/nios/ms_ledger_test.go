package nios

import (
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func conservingLedger() *MicrosoftAllocationLedger {
	return &MicrosoftAllocationLedger{
		DDIObjects:    MSCategoryPartition{Baseline: 10, Attributable: 6, Retained: 4},
		ActiveIPs:     MSCategoryPartition{Baseline: 20, Attributable: 15, Retained: 5},
		ManagedAssets: MSCategoryPartition{Baseline: 0, Attributable: 0, Retained: 0},
	}
}

// TestMSLedger_Conservation covers the three-category conservation gate.
func TestMSLedger_Conservation(t *testing.T) {
	t.Run("all categories conserve", func(t *testing.T) {
		if failing := checkMSConservation(conservingLedger()); len(failing) != 0 {
			t.Errorf("failing = %v, want empty", failing)
		}
	})

	t.Run("DDI Objects off by one", func(t *testing.T) {
		l := conservingLedger()
		l.DDIObjects.Retained++
		failing := checkMSConservation(l)
		if len(failing) != 1 || failing[0] != "DDI Objects" {
			t.Errorf("failing = %v, want [DDI Objects]", failing)
		}
	})

	t.Run("Active IPs off by one", func(t *testing.T) {
		l := conservingLedger()
		l.ActiveIPs.Retained++
		failing := checkMSConservation(l)
		if len(failing) != 1 || failing[0] != "Active IPs" {
			t.Errorf("failing = %v, want [Active IPs]", failing)
		}
	})

	t.Run("Managed Assets off by one", func(t *testing.T) {
		l := conservingLedger()
		l.ManagedAssets.Retained++
		failing := checkMSConservation(l)
		if len(failing) != 1 || failing[0] != "Managed Assets" {
			t.Errorf("failing = %v, want [Managed Assets]", failing)
		}
	})

	t.Run("all three inconsistent in fixed order", func(t *testing.T) {
		l := conservingLedger()
		l.DDIObjects.Retained++
		l.ActiveIPs.Retained++
		l.ManagedAssets.Retained++
		failing := checkMSConservation(l)
		want := []string{"DDI Objects", "Active IPs", "Managed Assets"}
		if len(failing) != len(want) {
			t.Fatalf("failing = %v, want %v", failing, want)
		}
		for i := range want {
			if failing[i] != want[i] {
				t.Errorf("failing[%d] = %q, want %q", i, failing[i], want[i])
			}
		}
	})

	t.Run("all-zero ledger conserves", func(t *testing.T) {
		l := &MicrosoftAllocationLedger{}
		if failing := checkMSConservation(l); len(failing) != 0 {
			t.Errorf("failing = %v, want empty", failing)
		}
	})

	t.Run("nil ledger does not panic", func(t *testing.T) {
		failing := checkMSConservation(nil)
		if len(failing) == 0 {
			t.Errorf("failing = %v, want a non-empty inconsistency report for nil ledger", failing)
		}
	})

	t.Run("build succeeds for a conserving state", func(t *testing.T) {
		st := newMSLedgerState()
		st.sawMSServer = true
		st.ddiObjects = MSCategoryPartition{Baseline: 10, Attributable: 6, Retained: 4}
		st.activeIPs = MSCategoryPartition{Baseline: 20, Attributable: 15, Retained: 5}

		ledger, diag := st.build()
		if ledger == nil {
			t.Fatal("build() returned nil ledger for a conserving state")
		}
		if !diag.Available {
			t.Errorf("diag.Available = false, want true")
		}
	})

	t.Run("build suppresses for a non-conserving state", func(t *testing.T) {
		st := newMSLedgerState()
		st.sawMSServer = true
		st.ddiObjects = MSCategoryPartition{Baseline: 10, Attributable: 6, Retained: 5} // off by one

		ledger, diag := st.build()
		if ledger != nil {
			t.Errorf("build() returned non-nil ledger for a non-conserving state: %+v", ledger)
		}
		if diag.Available {
			t.Errorf("diag.Available = true, want false")
		}
		if diag.Code != MSAllocationUnavailableCode {
			t.Errorf("diag.Code = %q, want %q", diag.Code, MSAllocationUnavailableCode)
		}
	})

	t.Run("build reports absent when no Microsoft server was seen", func(t *testing.T) {
		st := newMSLedgerState()

		ledger, diag := st.build()
		if ledger != nil {
			t.Errorf("build() returned non-nil ledger for an absent state: %+v", ledger)
		}
		if diag.Available {
			t.Errorf("diag.Available = true, want false")
		}
		if diag.Code != MSAllocationAbsentCode {
			t.Errorf("diag.Code = %q, want %q", diag.Code, MSAllocationAbsentCode)
		}
		if diag.Code == MSAllocationUnavailableCode {
			t.Errorf("absent code must differ from unavailable code")
		}
	})

	t.Run("unavailable message is fixed regardless of which category failed", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*msLedgerState)
		}{
			{"ddi", func(st *msLedgerState) {
				st.ddiObjects = MSCategoryPartition{Baseline: 1, Attributable: 0, Retained: 0}
			}},
			{"activeips", func(st *msLedgerState) { st.activeIPs = MSCategoryPartition{Baseline: 1, Attributable: 0, Retained: 0} }},
		}
		var messages []string
		for _, c := range cases {
			st := newMSLedgerState()
			st.sawMSServer = true
			c.mutate(st)
			_, diag := st.build()
			if diag.Available {
				t.Fatalf("%s: expected build() to suppress", c.name)
			}
			messages = append(messages, diag.Message)
		}
		for i := 1; i < len(messages); i++ {
			if messages[i] != messages[0] {
				t.Errorf("message differs across failure cases: %q vs %q", messages[i], messages[0])
			}
		}
	})

	t.Run("msConservationCheck seam overrides the gate for an otherwise-conserving state", func(t *testing.T) {
		restore := SetMSConservationCheckForTest(func(*MicrosoftAllocationLedger) []string {
			return []string{"DDI Objects"}
		})
		defer restore()

		st := newMSLedgerState()
		st.sawMSServer = true
		st.ddiObjects = MSCategoryPartition{Baseline: 10, Attributable: 6, Retained: 4}
		st.activeIPs = MSCategoryPartition{Baseline: 20, Attributable: 15, Retained: 5}

		ledger, diag := st.build()
		if ledger != nil {
			t.Errorf("build() = %+v, want nil despite the state conserving under the real gate", ledger)
		}
		if diag.Code != MSAllocationUnavailableCode {
			t.Errorf("diag.Code = %q, want %q", diag.Code, MSAllocationUnavailableCode)
		}
	})

	t.Run("msConservationCheck seam restores the real gate after cleanup", func(t *testing.T) {
		restore := SetMSConservationCheckForTest(func(*MicrosoftAllocationLedger) []string {
			return []string{"DDI Objects"}
		})
		restore()

		st := newMSLedgerState()
		st.sawMSServer = true
		st.ddiObjects = MSCategoryPartition{Baseline: 10, Attributable: 6, Retained: 4}
		st.activeIPs = MSCategoryPartition{Baseline: 20, Attributable: 15, Retained: 5}

		ledger, diag := st.build()
		if ledger == nil || !diag.Available {
			t.Errorf("after restore, build() = (%v, %+v), want a conserving ledger available again", ledger, diag)
		}
	})
}

// TestMSLedger_AllocationStates proves MSAllocationDiagnostic's three states
// (absent, unavailable, present) are pairwise distinguishable from
// (ledger == nil, Available, Code) alone — the state contract documented on
// MSAllocationDiagnostic itself.
func TestMSLedger_AllocationStates(t *testing.T) {
	absentSt := newMSLedgerState()
	absentLedger, absentDiag := absentSt.build()

	unavailableSt := newMSLedgerState()
	unavailableSt.sawMSServer = true
	unavailableSt.ddiObjects = MSCategoryPartition{Baseline: 1, Attributable: 0, Retained: 0}
	unavailableLedger, unavailableDiag := unavailableSt.build()

	presentSt := newMSLedgerState()
	presentSt.sawMSServer = true
	presentSt.ddiObjects = MSCategoryPartition{Baseline: 10, Attributable: 6, Retained: 4}
	presentSt.activeIPs = MSCategoryPartition{Baseline: 20, Attributable: 15, Retained: 5}
	presentLedger, presentDiag := presentSt.build()

	cases := []struct {
		name   string
		ledger *MicrosoftAllocationLedger
		diag   MSAllocationDiagnostic
	}{
		{"absent", absentLedger, absentDiag},
		{"unavailable", unavailableLedger, unavailableDiag},
		{"present", presentLedger, presentDiag},
	}

	if cases[0].ledger != nil || cases[0].diag.Available || cases[0].diag.Code != MSAllocationAbsentCode {
		t.Errorf("absent state = (ledger=%v, diag=%+v), want (nil, Available=false, Code=%q)", cases[0].ledger, cases[0].diag, MSAllocationAbsentCode)
	}
	if cases[1].ledger != nil || cases[1].diag.Available || cases[1].diag.Code != MSAllocationUnavailableCode {
		t.Errorf("unavailable state = (ledger=%v, diag=%+v), want (nil, Available=false, Code=%q)", cases[1].ledger, cases[1].diag, MSAllocationUnavailableCode)
	}
	if cases[2].ledger == nil || !cases[2].diag.Available || cases[2].diag.Code != "" {
		t.Errorf("present state = (ledger=%v, diag=%+v), want (non-nil, Available=true, Code=\"\")", cases[2].ledger, cases[2].diag)
	}

	type signature struct {
		nilLedger bool
		available bool
		code      string
	}
	seen := make(map[signature]string, len(cases))
	for _, c := range cases {
		sig := signature{nilLedger: c.ledger == nil, available: c.diag.Available, code: c.diag.Code}
		if prior, dup := seen[sig]; dup {
			t.Errorf("state %q shares signature %+v with state %q — the three states are not pairwise distinguishable", c.name, sig, prior)
		}
		seen[sig] = c.name
	}
}

// TestMSLedger_DiagnosticPrivacy asserts the unavailable diagnostic message
// cannot leak a count, a property name, an object type, or an identifier.
func TestMSLedger_DiagnosticPrivacy(t *testing.T) {
	msg := msAllocationUnavailableMessage

	if strings.ContainsAny(msg, "0123456789") {
		t.Errorf("message contains a digit: %q", msg)
	}
	if strings.Contains(msg, "/") {
		t.Errorf("message contains a slash: %q", msg)
	}
	// A dotted onedb.xml __type marker (e.g. ".com.infoblox.one.virtual_node")
	// shows up as a "." immediately followed by a non-space character. An
	// ordinary sentence-ending period is always followed by a space or the
	// end of the string, so this catches the leak class without hardcoding
	// any literal type name from this plan's prose.
	for i := 0; i < len(msg); i++ {
		if msg[i] == '.' && i+1 < len(msg) && msg[i+1] != ' ' {
			t.Errorf("message contains a dotted type-marker-like sequence at index %d: %q", i, msg)
		}
	}
}

// TestMSLedger_DNS covers zone identity, duplicate collapse, ambiguity, and
// the DNS-side reason codes at the msLedgerState level (task 2), constructing
// state directly rather than going through Scan.
func TestMSLedger_DNS(t *testing.T) {
	t.Run("duplicate rows with the same server collapse to one attributed zone", func(t *testing.T) {
		l := newMSLedgerState()
		l.recordMSServer(map[string]string{"ms_oid": "101"})
		l.recordMSServerDNSFlag(map[string]string{"parent": "101", "managed": "true"})
		l.recordZoneMSServer(map[string]string{"zone": ".1.zone.a", "ms_server": "101"})
		l.recordZoneMSServer(map[string]string{"zone": ".1.zone.a", "ms_server": "101"})
		l.buildMSDNSResolver()
		l.classifyDNSObject(NiosFamilyDNSZone, msZoneObjProps(".1.zone.a"))

		if l.ddiObjects.Attributable != 1 {
			t.Errorf("Attributable = %d, want 1", l.ddiObjects.Attributable)
		}
		if l.evidence.RelationshipRows != 2 {
			t.Errorf("RelationshipRows = %d, want 2", l.evidence.RelationshipRows)
		}
		if l.evidence.DuplicateRelationshipRows < 1 {
			t.Errorf("DuplicateRelationshipRows = %d, want >= 1", l.evidence.DuplicateRelationshipRows)
		}
	})

	t.Run("zone references differing only in view segment are distinct", func(t *testing.T) {
		l := newMSLedgerState()
		l.recordMSServer(map[string]string{"ms_oid": "101"})
		l.recordMSServerDNSFlag(map[string]string{"parent": "101", "managed": "true"})
		l.recordZoneMSServer(map[string]string{"zone": ".1.zone.a", "ms_server": "101"})
		l.recordZoneMSServer(map[string]string{"zone": ".2.zone.a", "ms_server": "101"})
		l.buildMSDNSResolver()
		l.classifyDNSObject(NiosFamilyDNSZone, msZoneObjProps(".1.zone.a"))
		l.classifyDNSObject(NiosFamilyDNSZone, msZoneObjProps(".2.zone.a"))

		if l.ddiObjects.Attributable != 2 {
			t.Errorf("Attributable = %d, want 2", l.ddiObjects.Attributable)
		}
	})

	t.Run("valid row plus sentinel row on the same zone still attributes once", func(t *testing.T) {
		l := newMSLedgerState()
		l.recordMSServer(map[string]string{"ms_oid": "101"})
		l.recordMSServerDNSFlag(map[string]string{"parent": "101", "managed": "true"})
		l.recordZoneMSServer(map[string]string{"zone": ".1.zone.a", "ms_server": "101"})
		l.recordZoneMSServer(map[string]string{"zone": ".1.zone.a", "ms_server": "."})
		l.buildMSDNSResolver()
		l.classifyDNSObject(NiosFamilyDNSZone, msZoneObjProps(".1.zone.a"))

		if l.ddiObjects.Attributable != 1 {
			t.Errorf("Attributable = %d, want 1", l.ddiObjects.Attributable)
		}
		if l.evidence.RelationshipAnomalies < 1 {
			t.Errorf("RelationshipAnomalies = %d, want >= 1", l.evidence.RelationshipAnomalies)
		}
	})

	t.Run("sentinel-only zone is retained with ReasonSentinelReference", func(t *testing.T) {
		for _, sentinel := range []string{".", ""} {
			l := newMSLedgerState()
			l.recordMSServer(map[string]string{"ms_oid": "101"})
			l.recordMSServerDNSFlag(map[string]string{"parent": "101", "managed": "true"})
			l.recordZoneMSServer(map[string]string{"zone": ".1.zone.a", "ms_server": sentinel})
			l.buildMSDNSResolver()
			l.classifyDNSObject(NiosFamilyDNSZone, msZoneObjProps(".1.zone.a"))

			if l.ddiObjects.Attributable != 0 {
				t.Errorf("sentinel %q: Attributable = %d, want 0", sentinel, l.ddiObjects.Attributable)
			}
		}
	})

	t.Run("unmatched server OID is retained with ReasonUnmatchedReference", func(t *testing.T) {
		l := newMSLedgerState()
		// "999" is never declared via recordMSServer.
		l.recordZoneMSServer(map[string]string{"zone": ".1.zone.a", "ms_server": "999"})
		l.buildMSDNSResolver()
		l.classifyDNSObject(NiosFamilyDNSZone, msZoneObjProps(".1.zone.a"))

	})

	t.Run("malformed managed value is retained with ReasonMalformedValue", func(t *testing.T) {
		l := newMSLedgerState()
		l.recordMSServer(map[string]string{"ms_oid": "101"})
		l.recordMSServerDNSFlag(map[string]string{"parent": "101", "managed": "sometimes"})
		l.recordZoneMSServer(map[string]string{"zone": ".1.zone.a", "ms_server": "101"})
		l.buildMSDNSResolver()
		l.classifyDNSObject(NiosFamilyDNSZone, msZoneObjProps(".1.zone.a"))

	})

	t.Run("server with no ms_server_dns_properties object is ReasonUnmatchedReference", func(t *testing.T) {
		l := newMSLedgerState()
		l.recordMSServer(map[string]string{"ms_oid": "101"})
		// No recordMSServerDNSFlag call — the flag object is entirely absent.
		l.recordZoneMSServer(map[string]string{"zone": ".1.zone.a", "ms_server": "101"})
		l.buildMSDNSResolver()
		l.classifyDNSObject(NiosFamilyDNSZone, msZoneObjProps(".1.zone.a"))

	})

	t.Run("two different DNS-managed servers is ReasonAmbiguousOwnership", func(t *testing.T) {
		l := newMSLedgerState()
		l.recordMSServer(map[string]string{"ms_oid": "101"})
		l.recordMSServer(map[string]string{"ms_oid": "102"})
		l.recordMSServerDNSFlag(map[string]string{"parent": "101", "managed": "true"})
		l.recordMSServerDNSFlag(map[string]string{"parent": "102", "managed": "true"})
		l.recordZoneMSServer(map[string]string{"zone": ".1.zone.a", "ms_server": "101"})
		l.recordZoneMSServer(map[string]string{"zone": ".1.zone.a", "ms_server": "102"})
		l.buildMSDNSResolver()
		l.classifyDNSObject(NiosFamilyDNSZone, msZoneObjProps(".1.zone.a"))

		if l.ddiObjects.Attributable != 0 {
			t.Errorf("Attributable = %d, want 0", l.ddiObjects.Attributable)
		}
	})

	t.Run("secondary-only zone is retained with no review entry (D-17)", func(t *testing.T) {
		l := newMSLedgerState()
		l.recordMSServer(map[string]string{"ms_oid": "101"})
		l.recordMSServerDNSFlag(map[string]string{"parent": "101", "managed": "true"})
		l.recordZoneMSSecondary(map[string]string{"zone": ".1.zone.a", "ms_server": "101"})
		l.buildMSDNSResolver()
		l.classifyDNSObject(NiosFamilyDNSZone, msZoneObjProps(".1.zone.a"))

		if l.ddiObjects.Attributable != 0 {
			t.Errorf("Attributable = %d, want 0", l.ddiObjects.Attributable)
		}
		if l.evidence.RelationshipAnomalies != 0 {
			t.Errorf("RelationshipAnomalies = %d, want 0", l.evidence.RelationshipAnomalies)
		}
	})

	t.Run("a retained zone with no relationship row at all gets no review entry", func(t *testing.T) {
		l := newMSLedgerState()
		l.buildMSDNSResolver()
		l.classifyDNSObject(NiosFamilyDNSZone, msZoneObjProps(".1.zone.never-referenced"))

		if l.ddiObjects.Attributable != 0 {
			t.Errorf("Attributable = %d, want 0", l.ddiObjects.Attributable)
		}
	})

	t.Run("msSentinelRef matches exactly the verified sentinel spellings", func(t *testing.T) {
		cases := map[string]bool{"": true, ".": true, "101": false, "true": false}
		for v, want := range cases {
			if got := msSentinelRef(v); got != want {
				t.Errorf("msSentinelRef(%q) = %v, want %v", v, got, want)
			}
		}
	})

	t.Run("msDNSRecordFamilies is the D-02 allowlist: 15 members, all in DDIFamilies, excluding the three special-cased families", func(t *testing.T) {
		if len(msDNSRecordFamilies) != 15 {
			t.Errorf("len(msDNSRecordFamilies) = %d, want 15", len(msDNSRecordFamilies))
		}
		for family := range msDNSRecordFamilies {
			if _, ok := DDIFamilies[family]; !ok {
				t.Errorf("msDNSRecordFamilies contains %q, which is not in DDIFamilies", family)
			}
		}
		for _, excluded := range []string{NiosFamilyDNSZone, NiosFamilyHostObject, NiosFamilyHostAlias} {
			if _, ok := msDNSRecordFamilies[excluded]; ok {
				t.Errorf("msDNSRecordFamilies must not contain %q", excluded)
			}
		}
	})
}

// mustPrefix parses s as a netip.Prefix or fails the test.
func mustPrefix(t *testing.T, s string) netip.Prefix {
	t.Helper()
	p, err := netip.ParsePrefix(s)
	if err != nil {
		t.Fatalf("ParsePrefix(%q): %v", s, err)
	}
	return p
}

// TestMSLedger_DHCPContainment covers the D-06 longest-prefix containment
// resolver at unit level (task 3 of 05-04), directly constructing
// msContainmentResolver instances per the plan's <action> guidance.
// Integration-level coverage through Scan lives in
// TestMSLedger_DHCPContainmentIntegration (ms_ledger_fixtures_test.go).
func TestMSLedger_DHCPContainment(t *testing.T) {
	t.Run("malformed address resolves no owner", func(t *testing.T) {
		r := &msContainmentResolver{}
		if server := r.resolveMSChild("not-an-ip", "v1"); server != "" {
			t.Errorf("server = %q, want no owner", server)
		}
	})

	t.Run("empty resolver has no boundary at all, silent retain", func(t *testing.T) {
		r := &msContainmentResolver{}
		server := r.resolveMSChild("192.0.2.5", "v1")
		if server != "" {
			t.Errorf("server = %q, want no owner", server)
		}
	})

	t.Run("IPv6 child against an IPv4-only resolver resolves no owner", func(t *testing.T) {
		r := &msContainmentResolver{owners: []msNetworkOwner{
			{prefix: mustPrefix(t, "192.0.2.0/24"), view: "v1", serverOID: "srv1", prefixLen: 24},
		}}
		if server := r.resolveMSChild("2001:db8::1", "v1"); server != "" {
			t.Errorf("server = %q, want no owner", server)
		}
	})

	t.Run("4-in-6-mapped form does not match an IPv4 prefix it would otherwise fall inside", func(t *testing.T) {
		r := &msContainmentResolver{owners: []msNetworkOwner{
			{prefix: mustPrefix(t, "192.0.2.0/24"), view: "v1", serverOID: "srv1", prefixLen: 24},
		}}
		if server := r.resolveMSChild("::ffff:192.0.2.5", "v1"); server != "" {
			t.Errorf("server = %q, want no owner", server)
		}
	})

	t.Run("missing child view with a containing network resolves no owner", func(t *testing.T) {
		r := &msContainmentResolver{owners: []msNetworkOwner{
			{prefix: mustPrefix(t, "192.0.2.0/24"), view: "v1", serverOID: "srv1", prefixLen: 24},
		}}
		if server := r.resolveMSChild("192.0.2.5", ""); server != "" {
			t.Errorf("server = %q, want no owner", server)
		}
	})

	t.Run("containing network in a different view resolves no owner", func(t *testing.T) {
		r := &msContainmentResolver{owners: []msNetworkOwner{
			{prefix: mustPrefix(t, "192.0.2.0/24"), view: "v1", serverOID: "srv1", prefixLen: 24},
		}}
		if server := r.resolveMSChild("192.0.2.5", "v2"); server != "" {
			t.Errorf("server = %q, want no owner", server)
		}
	})

	t.Run("no containing network at all is a silent retain", func(t *testing.T) {
		r := &msContainmentResolver{owners: []msNetworkOwner{
			{prefix: mustPrefix(t, "192.0.2.0/24"), view: "v1", serverOID: "srv1", prefixLen: 24},
		}}
		server := r.resolveMSChild("198.51.100.5", "v1")
		if server != "" {
			t.Errorf("server = %q, want no owner", server)
		}
	})

	t.Run("different prefix length resolves to the longer prefix", func(t *testing.T) {
		r := &msContainmentResolver{owners: []msNetworkOwner{
			{prefix: mustPrefix(t, "192.0.2.0/16"), view: "v1", serverOID: "srv-wide", prefixLen: 16},
			{prefix: mustPrefix(t, "192.0.2.0/24"), view: "v1", serverOID: "srv-narrow", prefixLen: 24},
		}}
		server := r.resolveMSChild("192.0.2.5", "v1")
		if server != "srv-narrow" {
			t.Errorf("server = %q, want srv-narrow", server)
		}
	})

	t.Run("equal prefix length in the same view resolves no owner, never by slice order", func(t *testing.T) {
		a := msNetworkOwner{prefix: mustPrefix(t, "192.0.2.0/24"), view: "v1", serverOID: "srv-a", prefixLen: 24}
		b := msNetworkOwner{prefix: mustPrefix(t, "192.0.2.0/24"), view: "v1", serverOID: "srv-b", prefixLen: 24}
		for _, owners := range [][]msNetworkOwner{{a, b}, {b, a}} {
			r := &msContainmentResolver{owners: owners}
			if server := r.resolveMSChild("192.0.2.5", "v1"); server != "" {
				t.Errorf("server = %q, want no owner regardless of slice order", server)
			}
		}
	})

	t.Run("resolving the same child twice returns identical results", func(t *testing.T) {
		r := &msContainmentResolver{owners: []msNetworkOwner{
			{prefix: mustPrefix(t, "192.0.2.0/24"), view: "v1", serverOID: "srv1", prefixLen: 24},
		}}
		s1 := r.resolveMSChild("192.0.2.5", "v1")
		s2 := r.resolveMSChild("192.0.2.5", "v1")
		if s1 != s2 {
			t.Errorf("got %q then %q, want identical", s1, s2)
		}
	})

	t.Run("building the resolver twice from the same input produces identical slice order", func(t *testing.T) {
		build := func() []msNetworkOwner {
			l := newMSLedgerState()
			l.dhcpNetworkResolution = map[string]msZoneResolution{
				"192.0.2.0/24/v1":    {attributable: true},
				"198.51.100.0/24/v1": {attributable: true},
				"192.0.2.0/16/v1":    {attributable: true},
			}
			l.dhcpNetworkMSServer = map[string]string{
				"192.0.2.0/24/v1":    "srv-a",
				"198.51.100.0/24/v1": "srv-b",
				"192.0.2.0/16/v1":    "srv-c",
			}
			l.buildMSContainmentResolver()
			return l.containment.owners
		}
		a := build()
		b := build()
		if !reflect.DeepEqual(a, b) {
			t.Errorf("owners differ across builds:\n%+v\n%+v", a, b)
		}
	})

	t.Run("a malformed network key is skipped and counted as a relationship anomaly", func(t *testing.T) {
		l := newMSLedgerState()
		l.dhcpNetworkResolution = map[string]msZoneResolution{"not-a-valid-key": {attributable: true}}
		l.dhcpNetworkMSServer = map[string]string{"not-a-valid-key": "srv1"}
		l.buildMSContainmentResolver()
		if len(l.containment.owners) != 0 {
			t.Errorf("owners = %+v, want empty", l.containment.owners)
		}
		if l.evidence.RelationshipAnomalies != 1 {
			t.Errorf("RelationshipAnomalies = %d, want 1", l.evidence.RelationshipAnomalies)
		}
	})

	t.Run("a parent exact match short-circuits an ambiguous containment set", func(t *testing.T) {
		// Two owners at the identical prefix length and view, both containing
		// the same test address, is unreachable through real distinct network
		// objects (equal-length CIDR blocks partition the address space and
		// cannot overlap) — this is exactly why it is constructed directly
		// here rather than through Scan (see acceptance criteria).
		const parentKey = "192.0.2.0/24/v1"
		l := newMSLedgerState()
		l.dhcpNetworkResolution = map[string]msZoneResolution{parentKey: {attributable: true}}
		l.containment = &msContainmentResolver{owners: []msNetworkOwner{
			{prefix: mustPrefix(t, "192.0.2.0/24"), view: "v1", serverOID: "srv-a", prefixLen: 24},
			{prefix: mustPrefix(t, "192.0.2.0/24"), view: "v1", serverOID: "srv-b", prefixLen: 24},
		}}

		l.classifyDHCPExclusionRange(map[string]string{
			"parent":        ".com.infoblox.dns.network$" + parentKey,
			"start_address": "192.0.2.5",
			"network_view":  "v1",
		})

		if l.ddiObjects.Attributable != 1 {
			t.Errorf("Attributable = %d, want 1", l.ddiObjects.Attributable)
		}
		if l.ddiObjects.Retained != 0 {
			t.Errorf("Retained = %d, want 0 — the parent match must short-circuit before containment's ambiguity fires", l.ddiObjects.Retained)
		}
	})
}

// MicrosoftLedgerForTest exposes the Microsoft allocation ledger for Phase 5
// fixture-level test assertions in the external nios_test package. Test-only
// surface (this file is excluded from production builds) — Phase 7 will
// decide whether and how this is exposed as a real API.
func (s *Scanner) MicrosoftLedgerForTest() (*MicrosoftAllocationLedger, MSAllocationDiagnostic) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.microsoftLedger, s.microsoftDiag
}

// SetMSConservationCheckForTest overrides the conservation-gate function
// msLedgerState.build calls (D-16), letting the external nios_test package
// prove the suppression path fires end to end through Scan on an otherwise
// legitimate fixture — one this package's own accounting invariants make
// impossible to construct as genuinely inconsistent. The returned restore
// function MUST be invoked (via t.Cleanup or defer) by every caller;
// forgetting to restore leaks the override into every later test in the
// same process. Test-only surface (this file is excluded from production
// builds).
func SetMSConservationCheckForTest(fn func(*MicrosoftAllocationLedger) []string) (restore func()) {
	prev := msConservationCheck
	msConservationCheck = fn
	return func() { msConservationCheck = prev }
}

// wellFormedLedger returns a MicrosoftAllocationLedger that trips none of
// msLedgerInvariants' checks, so every subtest in TestMSLedger_Invariants can
// start from a known-good baseline and mutate exactly one field.
func wellFormedLedger() *MicrosoftAllocationLedger {
	return &MicrosoftAllocationLedger{
		DDIObjects: MSCategoryPartition{Baseline: 10, Attributable: 8, Retained: 2},
		ActiveIPs:  MSCategoryPartition{Baseline: 5, Attributable: 4, Retained: 1},
	}
}

// TestMSLedger_Invariants drives msLedgerInvariants directly against
// hand-built ledgers (05-05-PLAN.md Task 3), rather than through build, since
// it is a pure function of the assembled ledger and every malformation case
// here is impossible to reach through the real classification pipeline
// (mirrors the msConservationCheck seam's own rationale: the invariant gate
// exists for a ledger this package cannot genuinely produce itself).
func TestMSLedger_Invariants(t *testing.T) {
	cases := []struct {
		name    string
		ledger  *MicrosoftAllocationLedger
		wantErr bool
	}{
		{
			name:   "well-formed ledger passes",
			ledger: wellFormedLedger(),
		},
		{
			name:    "nil ledger fails",
			wantErr: true,
		},
		{
			name: "non-zero ManagedAssets fails",
			ledger: func() *MicrosoftAllocationLedger {
				l := wellFormedLedger()
				l.ManagedAssets = MSCategoryPartition{Baseline: 1, Attributable: 1}
				return l
			}(),
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			failing := msLedgerInvariants(c.ledger)
			if c.wantErr && len(failing) == 0 {
				t.Errorf("msLedgerInvariants(%+v) = empty, want at least one violation", c.ledger)
			}
			if !c.wantErr && len(failing) != 0 {
				t.Errorf("msLedgerInvariants(%+v) = %v, want none", c.ledger, failing)
			}
		})
	}
}

// TestMSLedger_InvariantsSuppressBuild proves build() suppresses identically
// to the conservation gate when msLedgerInvariants finds a violation — the
// second half of Task 3's acceptance criterion. Each subtest drives a real
// violation from msLedgerState's own fields (no second seam needed): an
// empty-Service review entry is produced by calling the review accumulator
// directly, exactly as classifyDHCPObject or classifyDNSObject would if
// ever called with an empty family/service — a case the real classify*
// methods never exercise, but msLedgerInvariants exists precisely because
// build must not trust that they never will.
func TestMSLedger_InvariantsSuppressBuild(t *testing.T) {
	t.Run("control: an otherwise-valid state still builds", func(t *testing.T) {
		st := newMSLedgerState()
		st.sawMSServer = true
		st.ddiObjects = MSCategoryPartition{Baseline: 10, Attributable: 8, Retained: 2}

		ledger, diag := st.build()
		if ledger == nil || !diag.Available {
			t.Fatalf("build() = (%v, %+v), want a passing ledger", ledger, diag)
		}
	})

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

// msZoneObjProps builds the props of the zone object for the zone whose own
// reference is ownRef.
func msZoneObjProps(ownRef string) map[string]string {
	parent, fqdn := msZoneParentAndFQDN(ownRef)
	return map[string]string{"zone": parent, "fqdn": fqdn}
}
