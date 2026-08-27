// Package nios — Microsoft allocation ledger.
// ms_ledger.go is the aggregate-only Microsoft attribution ledger: the
// per-service attribution conditions and the three-category conservation gate. It is deliberately separate from
// microsoft.go's informational NiosMicrosoftServers contract — that DTO stays a
// visibility breakout of per-server identifiers; this file never carries one.
package nios

import (
	"net/netip"
	"sort"
	"strings"
)

// MSServiceDNS and MSServiceDHCP identify which service a partition belongs to.
const (
	MSServiceDNS  = "DNS"
	MSServiceDHCP = "DHCP"
)

// msClassifyConditions is the set of independently-detected conditions that can
// make a token-bearing Microsoft resource ineligible for confident attribution.
type msClassifyConditions struct {
	MalformedValue        bool
	SentinelReference     bool
	UnmatchedReference    bool
	UnsupportedType       bool
	AmbiguousOwnership    bool
	ContainmentUnverified bool
	RelationshipAnomaly   bool
}

// MSCategoryPartition splits one original NIOS token category into the count
// still confidently attributable to Microsoft and the count retained in NIOS.
// The invariant is Baseline == Attributable + Retained (D-14); there is never a
// third token-bearing partition.
type MSCategoryPartition struct {
	Baseline     int `json:"baseline"`
	Attributable int `json:"attributable"`
	// Retained must be accumulated independently as resources are classified
	// (see msLedgerState) — never computed by subtracting the Attributable
	// count from the Baseline count. Deriving it that way would make
	// checkMSConservation unfailable and silently defeat D-16's suppression
	// path, since the invariant it checks would then hold by construction.
	Retained int `json:"retained"`
}

// MSServiceSplit is the per-service (DNS vs DHCP) breakdown of one side of the
// Microsoft-attributable DDI Objects count (D-01). It deliberately carries no
// Baseline field: the ledger's DDI baseline is set once, whole-grid, from
// result.gridDDI (scanner.go) — there is no independent per-service baseline
// source, and a Baseline field fillable only by adding Attributable+Retained
// would be a derive-by-addition mirror of the derive-by-subtraction pattern
// MSCategoryPartition.Retained's doc comment above already forbids. No string
// field may ever be added to this type: a free-text field is capable of
// carrying a backup-derived identifier.
type MSServiceSplit struct {
	Attributable int `json:"attributable"`
	Retained     int `json:"retained"`
}

// MSEvidenceCounts holds raw D-15 relationship metrics. These may exceed
// unique resource counts (a resource can have multiple relationship rows) and
// never participate in conservation — they are diagnostic evidence only.
type MSEvidenceCounts struct {
	// RelationshipRows is the raw count of explicit Microsoft relationship
	// rows observed, before dedup to unique resources.
	RelationshipRows int `json:"relationshipRows"`
	// DuplicateRelationshipRows is the count of relationship rows that named
	// a resource already claimed by an earlier valid relationship (D-04).
	DuplicateRelationshipRows int `json:"duplicateRelationshipRows"`
	// RelationshipAnomalies is the count of broken or anomalous relationship
	// rows found on an otherwise attributable resource (D-12) — evidence
	// only, never moves or duplicates the resource in the token ledger.
	RelationshipAnomalies int `json:"relationshipAnomalies"`
}

// MSServerCounts are per-server-role inputs Phase 6 needs for MSALLOC-04's
// one-asset-per-server union. These are NOT a Phase-5 token-bearing partition.
type MSServerCounts struct {
	DNSManaged    int `json:"dnsManaged"`
	DHCPManaged   int `json:"dhcpManaged"`
	EitherManaged int `json:"eitherManaged"`
}

// MicrosoftAllocationLedger is the aggregate-only Microsoft attribution
// snapshot for one scan, reconciled independently across three categories
// (D-13). The NIOS backup scanner emits no calculator.CategoryManagedAssets
// finding rows — grep of internal/scanner/nios confirms this — so the
// ManagedAssets partition is all-zero by construction in this phase and
// exists only to make D-13's three-way independent reconciliation literal.
// Phase 5 must not populate a non-zero Managed Assets attributable count:
// there is no NIOS baseline for it to move from.
type MicrosoftAllocationLedger struct {
	DDIObjects     MSCategoryPartition `json:"ddiObjects"`
	DDIObjectsDNS  MSServiceSplit      `json:"ddiObjectsDNS"`
	DDIObjectsDHCP MSServiceSplit      `json:"ddiObjectsDHCP"`
	ActiveIPs      MSCategoryPartition `json:"activeIPs"`
	ManagedAssets  MSCategoryPartition `json:"managedAssets"`
	Evidence       MSEvidenceCounts    `json:"evidence"`
	Servers        MSServerCounts      `json:"servers"`
}

// MSAllocationUnavailableCode and MSAllocationAbsentCode are the two distinct
// diagnostic codes msLedgerState.build can return alongside a nil ledger.
const (
	// MSAllocationUnavailableCode means a Microsoft server was seen but the
	// conservation gate failed, so the snapshot is suppressed (D-16).
	MSAllocationUnavailableCode = "MS_ALLOCATION_UNAVAILABLE"
	// MSAllocationAbsentCode means no Microsoft server was recorded at all —
	// the feature is simply absent from this backup, not a failure.
	MSAllocationAbsentCode = "MS_ALLOCATION_ABSENT"
)

// msAllocationUnavailableMessage is the single fixed diagnostic message for a
// conservation-gate failure. It is a constant, not built from any input value,
// so it cannot leak a count, a property name, an object type, a parser stage,
// a backup file structure, an XML tag, or a backup-derived identifier (D-16,
// UI-SPEC Copywriting Contract).
const msAllocationUnavailableMessage = "The Microsoft allocation snapshot is unavailable for this scan. " +
	"The baseline NIOS scan results remain valid and usable."

// MSAllocationDiagnostic reports whether a MicrosoftAllocationLedger is
// available for this scan, and if not, why. Exactly three states exist, each
// fully determined by (ledger == nil, Available, Code) alone — see
// TestMSLedger_AllocationStates:
//
//   - Present: ledger != nil, Available == true, Code == "". A Microsoft
//     server was seen and every category conserved (D-14).
//   - Absent: ledger == nil, Available == false, Code == MSAllocationAbsentCode.
//     No Microsoft server was recorded at all — the feature is simply absent
//     from this backup, not a failure.
//   - Unavailable: ledger == nil, Available == false, Code ==
//     MSAllocationUnavailableCode. A Microsoft server was seen but the
//     conservation gate failed (D-16), so the ledger is suppressed. The
//     baseline NIOS scan results are unaffected — Scan still returns them
//     with a nil error.
//
// No new field should be added to disambiguate a fourth state; the state is
// always fully determined by this triple.
type MSAllocationDiagnostic struct {
	Available bool   `json:"available"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
}

// msCategoryNames fixes the display-name order checkMSConservation reports
// failures in, so the diagnostic is deterministic.
var msCategoryNames = []string{"DDI Objects", "Active IPs", "Managed Assets"}

// msConservationCheck is the conservation-gate function build calls to
// decide suppression (D-16). Production code initializes it once, to
// checkMSConservation, and never reassigns it — every real classify* method
// in this file credits exactly one partition per baseline unit, by
// construction, so the gate cannot genuinely fail against a backup this
// package produced itself. SetMSConservationCheckForTest
// (ms_ledger_test.go, test-only) is the sole reassignment path, letting
// integration tests prove the D-16 suppression path fires end to end through
// Scan without needing a real onedb.xml fixture engineered to violate an
// invariant this package otherwise enforces by construction.
var msConservationCheck = checkMSConservation

// checkMSConservation returns the names of every category in l where
// Baseline != Attributable + Retained, in msCategoryNames order. A nil ledger
// returns a single-element slice naming the whole ledger as inconsistent
// rather than panicking.
func checkMSConservation(l *MicrosoftAllocationLedger) []string {
	if l == nil {
		return []string{"Microsoft Allocation Ledger"}
	}
	partitions := [3]MSCategoryPartition{l.DDIObjects, l.ActiveIPs, l.ManagedAssets}
	var failing []string
	for i, name := range msCategoryNames {
		p := partitions[i]
		if p.Baseline != p.Attributable+p.Retained {
			failing = append(failing, name)
		}
	}
	return failing
}

// msLedgerState accumulates Microsoft allocation data during Pass 1 and Pass
// 2, one instance per scan. build() is the only code path in this package
// that can construct a returnable *MicrosoftAllocationLedger, and it runs
// behind checkMSConservation, so no caller can bypass D-16.
type msLedgerState struct {
	ddiObjects MSCategoryPartition
	// ddiObjectsDNS and ddiObjectsDHCP are the per-service split of ddiObjects
	// (D-01). They are moved ONLY by creditDDIAttributable/creditDDIRetained,
	// alongside the combined ddiObjects field, so the two can never drift
	// apart.
	ddiObjectsDNS  MSServiceSplit
	ddiObjectsDHCP MSServiceSplit
	activeIPs      MSCategoryPartition
	managedAssets  MSCategoryPartition
	evidence       MSEvidenceCounts
	servers        MSServerCounts
	sawMSServer    bool

	// DNS attribution indexes, populated during Pass 1.
	msServerOIDs  map[string]struct{} // ms_oid → declared (recordMSServer)
	dnsManagedRaw map[string]string   // ms_oid (parent) → raw ms_server_dns_properties.managed value
	zoneMSServer  map[string]string   // zone ref (raw, unparsed — 05-SCHEMA.md A1) → ms_server OID, first-seen-wins
	// zoneSecondaryOnly records zones whose only relationship row is a
	// zone_ms_secondary_server (D-17 default: a secondary never claims).
	zoneSecondaryOnly map[string]struct{}
	// zoneAmbiguous holds every zone that saw two zone_ms_primary_server rows
	// naming different servers (D-09 ReasonAmbiguousOwnership).
	zoneAmbiguous map[string]struct{}
	// zoneAnomaly accumulates broken-row conditions (currently just a sentinel
	// ms_server reference) seen on a zone that may still resolve validly
	// through an earlier or later valid row (D-04/D-12: evidence only, never
	// moves an already-attributed resource).
	zoneAnomaly map[string]msClassifyConditions
	// zoneResolution is the precomputed per-zone verdict, built once by
	// buildMSDNSResolver() after Pass 1 completes and consulted read-only by
	// every classifyDNSObject call during Pass 2 (order-independence: a
	// zone_ms_primary_server row can appear before or after the ms_server row
	// it references).
	zoneResolution map[string]msZoneResolution

	// DHCP attribution indexes, populated during Pass 1 (plan 05-04, Task 1).
	// dhcpManagedRaw stores the RAW ms_server_dhcp_properties.managed value,
	// keyed by parent (ms_oid) — the DHCP-side twin of dnsManagedRaw above,
	// with the same deliberate avoidance of msCollector's `== "true"` silent
	// zero-default (microsoft.go).
	dhcpManagedRaw map[string]string
	// dhcpNetworkMSServer indexes a ms_dhcp_member relationship row by its
	// network reference (same composite "address/cidr/view" value the row's
	// own "network" property already carries — 05-SCHEMA.md), first-seen-wins.
	// .com.infoblox.dns.network carries no ms_server property of its own
	// (0/7306 occurrences), so this relationship index is the ONLY attribution
	// path for networks — never containment (D-08).
	dhcpNetworkMSServer map[string]string
	// dhcpNetworkAmbiguous holds every network that saw two ms_dhcp_member
	// rows naming different servers (D-09 ReasonAmbiguousOwnership).
	dhcpNetworkAmbiguous map[string]struct{}
	// dhcpNetworkAnomaly accumulates broken-row conditions (a sentinel
	// ms_server reference on the relationship row) seen on a network that may
	// still resolve validly through a different row (D-04/D-12).
	dhcpNetworkAnomaly map[string]msClassifyConditions
	// dhcpNetworkResolution is the precomputed per-network verdict, built once
	// by buildMSDHCPResolver() after Pass 1 completes, mirroring zoneResolution
	// for the same order-independence reason (a ms_dhcp_member row can appear
	// before or after the ms_server_dhcp_properties row it references).
	dhcpNetworkResolution map[string]msZoneResolution

	// containment is the longest-prefix-match resolver for exclusion_range
	// (the one DHCP family with no direct ms_server reference of its own,
	// 05-SCHEMA.md). Built once by buildMSContainmentResolver() after Pass 1
	// completes. D-06/D-08: containment is a narrow fallback scoped to this
	// family alone — networks/ranges/fixed/leases attribute via exact
	// reference and must never route through it.
	containment *msContainmentResolver

	// msFixedIPSeen and msActiveLeaseIPSeen are Task 3's tier-1 per-category
	// baseline-unit mirrors, and msActiveIPSet is the tier-2 cross-category
	// union — see the doc comment above classifyMSActiveIPUnit for the full
	// two-tier Active-IP accounting this implements (D-07).
	msFixedIPSeen       map[string]struct{}
	msActiveLeaseIPSeen map[string]struct{}
	msActiveIPSet       map[string]struct{}
}

// msZoneResolution is one zone's precomputed attribution verdict.
type msZoneResolution struct {
	attributable bool
	conditions   msClassifyConditions
}

func newMSLedgerState() *msLedgerState {
	return &msLedgerState{
		msServerOIDs:         make(map[string]struct{}),
		dnsManagedRaw:        make(map[string]string),
		zoneMSServer:         make(map[string]string),
		zoneSecondaryOnly:    make(map[string]struct{}),
		zoneAmbiguous:        make(map[string]struct{}),
		zoneAnomaly:          make(map[string]msClassifyConditions),
		dhcpManagedRaw:       make(map[string]string),
		dhcpNetworkMSServer:  make(map[string]string),
		dhcpNetworkAmbiguous: make(map[string]struct{}),
		dhcpNetworkAnomaly:   make(map[string]msClassifyConditions),
		msFixedIPSeen:        make(map[string]struct{}),
		msActiveLeaseIPSeen:  make(map[string]struct{}),
		msActiveIPSet:        make(map[string]struct{}),
	}
}

// creditDDIAttributable increments the combined ddiObjects.Attributable
// counter and the matching per-service split field for service. This is the
// ONLY place in this package that may move the combined DDI Attributable
// counter, so the combined total and the per-service split cannot drift
// apart by construction. service must be MSServiceDNS or MSServiceDHCP; any
// other value is a programming error and panics rather than silently
// dropping the credit, which would break the split identity the tests in
// ms_ledger_split_test.go assert.
func (l *msLedgerState) creditDDIAttributable(service string) {
	l.ddiObjects.Attributable++
	switch service {
	case MSServiceDNS:
		l.ddiObjectsDNS.Attributable++
	case MSServiceDHCP:
		l.ddiObjectsDHCP.Attributable++
	default:
		panic("nios: creditDDIAttributable: unknown service " + service)
	}
}

// creditDDIRetained increments the combined ddiObjects.Retained counter and
// the matching per-service split field for service. This is the ONLY place
// in this package that may move the combined DDI Retained counter — see
// creditDDIAttributable's doc comment for the full rationale, which applies
// identically here.
func (l *msLedgerState) creditDDIRetained(service string) {
	l.ddiObjects.Retained++
	switch service {
	case MSServiceDNS:
		l.ddiObjectsDNS.Retained++
	case MSServiceDHCP:
		l.ddiObjectsDHCP.Retained++
	default:
		panic("nios: creditDDIRetained: unknown service " + service)
	}
}

// msSentinelRef reports whether v is onedb.xml's unset-reference sentinel:
// empty, or the literal "." (verified 05-SCHEMA.md — 473,608/473,608
// occurrences on a Microsoft-unconfigured grid). Scope note: on a RELATIONSHIP
// object (e.g. zone_ms_primary_server.ms_server) a sentinel value means the row
// is broken (ReasonSentinelReference, task 2). On a plain resource object's own
// ms_server/ms_server_id property, a sentinel value simply asserts "not
// Microsoft-claimed" and is not an anomaly — that distinction is encoded at
// each call site, never inside this helper.
func msSentinelRef(v string) bool {
	return v == "" || v == "."
}

// recordMSServer indexes a declared Microsoft server by its ms_oid. This is
// what makes D-10's ReasonUnmatchedReference decidable later: a relationship
// row naming an OID absent from this set references a server the backup never
// declared.
func (l *msLedgerState) recordMSServer(props map[string]string) {
	oid := props["ms_oid"]
	if oid == "" {
		return
	}
	l.msServerOIDs[oid] = struct{}{}
	l.sawMSServer = true
}

// recordMSServerDNSFlag stores the RAW ms_server_dns_properties.managed value,
// keyed by parent (the ms_oid), for strict classification later. Deliberately
// does NOT reuse msCollector.dnsManaged: its `== "true"` comparison
// (microsoft.go:74) silently maps every malformed spelling to false, which is
// exactly the silent zero-default D-10's ReasonMalformedValue exists to catch.
func (l *msLedgerState) recordMSServerDNSFlag(props map[string]string) {
	parent := props["parent"]
	if parent == "" {
		return
	}
	l.dnsManagedRaw[parent] = props["managed"]
}

// recordZoneMSServer indexes a zone_ms_primary_server relationship row by its
// raw zone reference — the same key recordZone uses at pass1.go:216, byte
// identical, no view derivation (05-SCHEMA.md A1, D-03). D-15: every
// invocation increments RelationshipRows, including bail paths, because raw
// relationship rows are evidence regardless of whether they produce a usable
// claim. A sentinel ms_server value marks the zone as anomalous (D-12) without
// touching zoneMSServer — an earlier or later valid row on the same zone can
// still attribute it. First valid row wins; any repeat row for an
// already-claimed zone is a duplicate (D-04), and a duplicate naming a
// different server marks the zone ambiguous (D-09).
func (l *msLedgerState) recordZoneMSServer(props map[string]string) {
	l.evidence.RelationshipRows++
	zone := props["zone"]
	if zone == "" {
		return
	}
	server := props["ms_server"]
	if msSentinelRef(server) {
		c := l.zoneAnomaly[zone]
		c.SentinelReference = true
		l.zoneAnomaly[zone] = c
		return
	}
	existing, exists := l.zoneMSServer[zone]
	if !exists {
		l.zoneMSServer[zone] = server
		return
	}
	l.evidence.DuplicateRelationshipRows++
	if existing != server {
		l.zoneAmbiguous[zone] = struct{}{}
	}
}

// recordZoneMSSecondary records a zone_ms_secondary_server relationship row.
// Per the D-17 default, a secondary server is not authoritative and does not
// claim the zone: this only counts non-token-bearing evidence and marks the
// zone in zoneSecondaryOnly, never writing to zoneMSServer. The set exists so
// the default is one map write away from reversal, should that decision ever
// be revisited (see 05-03-PLAN.md flagged_assumptions S1 / D-17).
func (l *msLedgerState) recordZoneMSSecondary(props map[string]string) {
	l.evidence.RelationshipRows++
	zone := props["zone"]
	if zone == "" {
		return
	}
	l.zoneSecondaryOnly[zone] = struct{}{}
}

// recordMSServerDHCPFlag stores the RAW ms_server_dhcp_properties.managed
// value, keyed by parent (the ms_oid), mirroring recordMSServerDNSFlag above
// for the DHCP side.
func (l *msLedgerState) recordMSServerDHCPFlag(props map[string]string) {
	parent := props["parent"]
	if parent == "" {
		return
	}
	l.dhcpManagedRaw[parent] = props["managed"]
}

// recordDHCPNetworkMSServer indexes a ms_dhcp_member relationship row by its
// network reference. Mirrors recordZoneMSServer exactly (same first-valid-
// row-wins, same duplicate/ambiguity bookkeeping, same D-15 evidence
// counting on every invocation including bail paths) for the network/DHCP
// pairing instead of zone/DNS. .com.infoblox.dns.network has no ms_server
// property of its own, so this relationship row is the only claim a network
// can ever receive (D-08: never routed through containment).
func (l *msLedgerState) recordDHCPNetworkMSServer(props map[string]string) {
	l.evidence.RelationshipRows++
	network := props["network"]
	if network == "" {
		return
	}
	server := props["ms_server"]
	if msSentinelRef(server) {
		c := l.dhcpNetworkAnomaly[network]
		c.SentinelReference = true
		l.dhcpNetworkAnomaly[network] = c
		return
	}
	existing, exists := l.dhcpNetworkMSServer[network]
	if !exists {
		l.dhcpNetworkMSServer[network] = server
		return
	}
	l.evidence.DuplicateRelationshipRows++
	if existing != server {
		l.dhcpNetworkAmbiguous[network] = struct{}{}
	}
}

// buildMSDNSResolver precomputes the attribution verdict for every zone seen
// during Pass 1 (via recordZoneMSServer or recordZoneMSServer's anomaly path),
// storing it in zoneResolution. It must run exactly once, after Pass 1
// completes and before any classifyDNSObject call — the two-pass architecture
// means a zone_ms_primary_server row can appear in the backup before or after
// the ms_server / ms_server_dns_properties rows it references, so resolution
// cannot happen incrementally inside recordZoneMSServer itself. Never returns
// a hostname, never consults gmHostname, and never reaches the Tier-3
// Grid-Master fallback pattern at pass1.go:320-336.
func (l *msLedgerState) buildMSDNSResolver() {
	l.zoneResolution = make(map[string]msZoneResolution, len(l.zoneMSServer)+len(l.zoneAnomaly))
	seen := make(map[string]struct{}, len(l.zoneMSServer)+len(l.zoneAnomaly))
	for zone := range l.zoneMSServer {
		seen[zone] = struct{}{}
	}
	for zone := range l.zoneAnomaly {
		seen[zone] = struct{}{}
	}
	for zone := range seen {
		var c msClassifyConditions
		server, hasValidServer := l.zoneMSServer[zone]
		if !hasValidServer {
			// Every relationship row on this zone was a sentinel — no valid
			// claim was ever recorded.
			c.SentinelReference = true
			l.zoneResolution[zone] = msZoneResolution{conditions: c}
			continue
		}
		if _, ok := l.msServerOIDs[server]; !ok {
			c.UnmatchedReference = true
		} else if managed, ok := l.dnsManagedRaw[server]; !ok {
			c.UnmatchedReference = true
		} else if managed != "true" && managed != "false" {
			c.MalformedValue = true
		}
		if _, ambiguous := l.zoneAmbiguous[zone]; ambiguous {
			c.AmbiguousOwnership = true
		}
		attributable := !c.MalformedValue && !c.UnmatchedReference && !c.AmbiguousOwnership &&
			l.dnsManagedRaw[server] == "true"
		if _, hadAnomalyRow := l.zoneAnomaly[zone]; hadAnomalyRow && attributable {
			// A broken extra row coexists with a valid claim (D-04/D-12):
			// evidence only, never blocks or moves the resource.
			l.evidence.RelationshipAnomalies++
		}
		l.zoneResolution[zone] = msZoneResolution{attributable: attributable, conditions: c}
	}
}

// buildMSDHCPResolver precomputes the attribution verdict for every network
// seen during Pass 1, mirroring buildMSDNSResolver's zone resolution exactly
// with one deliberate addition: an exact managed=="false" value is its own
// RelationshipAnomaly condition here, not a silent zero-condition "not
// attributable." D-12's catch-all evidence bucket is extended here to gate
// attribution, since D-12 never blocked attribution on its own for the DNS
// side — a deliberate DHCP-side divergence, not a redefinition of the
// DNS-side code above.
func (l *msLedgerState) buildMSDHCPResolver() {
	l.dhcpNetworkResolution = make(map[string]msZoneResolution, len(l.dhcpNetworkMSServer)+len(l.dhcpNetworkAnomaly))
	seen := make(map[string]struct{}, len(l.dhcpNetworkMSServer)+len(l.dhcpNetworkAnomaly))
	for network := range l.dhcpNetworkMSServer {
		seen[network] = struct{}{}
	}
	for network := range l.dhcpNetworkAnomaly {
		seen[network] = struct{}{}
	}
	for network := range seen {
		var c msClassifyConditions
		server, hasValidServer := l.dhcpNetworkMSServer[network]
		if !hasValidServer {
			// Every relationship row on this network was a sentinel — no
			// valid claim was ever recorded.
			c.SentinelReference = true
			l.dhcpNetworkResolution[network] = msZoneResolution{conditions: c}
			continue
		}
		if _, ok := l.msServerOIDs[server]; !ok {
			c.UnmatchedReference = true
		} else if managed, ok := l.dhcpManagedRaw[server]; !ok {
			c.UnmatchedReference = true
		} else if managed == "false" {
			c.RelationshipAnomaly = true
		} else if managed != "true" {
			c.MalformedValue = true
		}
		if _, ambiguous := l.dhcpNetworkAmbiguous[network]; ambiguous {
			c.AmbiguousOwnership = true
		}
		attributable := !c.MalformedValue && !c.UnmatchedReference && !c.AmbiguousOwnership &&
			!c.RelationshipAnomaly && l.dhcpManagedRaw[server] == "true"
		if _, hadAnomalyRow := l.dhcpNetworkAnomaly[network]; hadAnomalyRow && attributable {
			l.evidence.RelationshipAnomalies++
		}
		l.dhcpNetworkResolution[network] = msZoneResolution{attributable: attributable, conditions: c}
	}
}

// dhcpNetworkResolutionKey rebuilds the composite "address/cidr/view" key a
// network object resolves under, mirroring counter.go's NiosFamilyNetwork
// lookupKey construction exactly (byte-for-byte, same three props, same "/"
// join) so a network object and its ms_dhcp_member relationship row stay
// joined on an identical key. A .com.infoblox.dns.ms_dhcp_member row's own
// "network" property is already in this composite form, so
// recordDHCPNetworkMSServer needs no equivalent helper.
func dhcpNetworkResolutionKey(props map[string]string) string {
	address := strings.TrimSpace(props["address"])
	cidr := strings.TrimSpace(props["cidr"])
	view := props["network_view"]
	if address == "" || cidr == "" || view == "" {
		return ""
	}
	return address + "/" + cidr + "/" + view
}

// classifyDHCPNetwork credits exactly one of Attributable or Retained in BOTH
// DDIObjects and ActiveIPs for a network object already counted in both
// baselines. Networks have no direct ms_server reference of their own, so
// this consults only the precomputed relationship verdict from
// buildMSDHCPResolver — never containment (D-08).
//
// The ActiveIPs credit is a network-reservation term distinct from Task 3's
// fixed_address/lease work: every network object unconditionally contributes
// 2 to ActiveIPs.Baseline via networkCount() (scanner.go's
// NetworkReservations = 2×network-object-count, covering the network and
// broadcast addresses NIOS reserves per network), regardless of whether its
// CIDR even parses. Nothing else in this ledger ever credits that baseline
// term, so a network object handled only by DDIObjects would leave
// ActiveIPs.Baseline permanently ahead of Attributable+Retained and fail
// checkMSConservation on any grid that has a network at all. The two
// reservation IPs follow the SAME verdict as the network's own DDI-object
// attribution — a Microsoft-managed network's reserved addresses move with
// it, not independently.
func (l *msLedgerState) classifyDHCPNetwork(props map[string]string) {
	res, ok := l.dhcpNetworkResolution[dhcpNetworkResolutionKey(props)]
	if ok && res.attributable {
		l.creditDDIAttributable(MSServiceDHCP)
		l.activeIPs.Attributable += 2
		return
	}
	l.creditDDIRetained(MSServiceDHCP)
	l.activeIPs.Retained += 2
	if ok {
	}
}

// resolveDirectMSRef classifies a resource's own direct ms_server-style
// reference (dhcp_range.ms_server, and by the same shape fixed_address.ms_server
// / lease.ms_server_id in Task 3). Unlike a relationship object's reference
// (ms_dhcp_member.ms_server, zone_ms_primary_server.ms_server), a sentinel
// value here is not a broken row — msSentinelRef's doc comment already
// establishes that a sentinel on a resource's own reference property simply
// asserts "not Microsoft-claimed" — so it credits Retained silently.
func (l *msLedgerState) resolveDirectMSRef(server string) (verified bool) {
	if msSentinelRef(server) {
		return false
	}
	if _, ok := l.msServerOIDs[server]; !ok {
		return false
	}
	managed, ok := l.dhcpManagedRaw[server]
	if !ok {
		return false
	}
	switch managed {
	case "true":
		return true
	case "false":
		// buildMSDHCPResolver treats an exact "false" as its own condition on
		// the DHCP side; either way the server is not confidently managed.
		return false
	default:
		return false
	}
}

// msNetworkOwner is one verified Microsoft-owned network boundary,
// precomputed for D-06 longest-prefix containment lookups.
type msNetworkOwner struct {
	prefix    netip.Prefix
	view      string
	serverOID string
	prefixLen int
}

// msContainmentResolver is the narrow D-06 fallback for a DHCP child that
// carries no direct ms_server reference of its own — in this phase's scope,
// NiosFamilyExclusionRange alone (05-SCHEMA.md: dhcp_range, fixed_address,
// and lease all carry their own reference; only exclusion_range does not).
// Built once on net/netip and deliberately kept separate from counter.go's
// net-based memberResolver/cidrEntry/resolveIPMember/buildCIDREntries so the
// two address representations never mix inside one resolution path
// (05-RESEARCH.md Alternatives Considered).
type msContainmentResolver struct {
	owners []msNetworkOwner
}

// buildMSContainmentResolver builds the containment resolver from
// dhcpNetworkResolution — the same per-network verdict classifyDHCPNetwork
// consults — filtered to attributable networks (D-08: only a verified
// Microsoft-owned network is ever a containment boundary). Reusing that
// precomputed verdict, rather than re-deriving managed-flag/msServerOIDs/
// ambiguity checks here, means a network can never be a containment
// boundary under looser rules than it is a DDI-object attribution itself.
// A key that fails to parse as "address/cidr/view" is skipped and counted as
// a relationship anomaly rather than silently dropped. Must run after
// buildMSDHCPResolver has populated dhcpNetworkResolution.
//
// The resulting slice is sorted descending by prefix length with a
// deterministic tie-break (prefix string, then view) so repeated builds from
// the same input are byte-identical — resolveMSChild never relies on this
// order to resolve an actual tie, it detects those explicitly.
func (l *msLedgerState) buildMSContainmentResolver() {
	owners := make([]msNetworkOwner, 0, len(l.dhcpNetworkResolution))
	for key, res := range l.dhcpNetworkResolution {
		if !res.attributable {
			continue
		}
		parts := strings.SplitN(key, "/", 3)
		if len(parts) != 3 {
			l.evidence.RelationshipAnomalies++
			continue
		}
		prefix, err := netip.ParsePrefix(parts[0] + "/" + parts[1])
		if err != nil {
			l.evidence.RelationshipAnomalies++
			continue
		}
		owners = append(owners, msNetworkOwner{
			prefix:    prefix,
			view:      parts[2],
			serverOID: l.dhcpNetworkMSServer[key],
			prefixLen: prefix.Bits(),
		})
	}
	sort.Slice(owners, func(i, j int) bool {
		if owners[i].prefixLen != owners[j].prefixLen {
			return owners[i].prefixLen > owners[j].prefixLen
		}
		if owners[i].prefix.String() != owners[j].prefix.String() {
			return owners[i].prefix.String() < owners[j].prefix.String()
		}
		return owners[i].view < owners[j].view
	})
	l.containment = &msContainmentResolver{owners: owners}
}

// resolveMSChild resolves one child address against the verified
// Microsoft-owned network boundaries in r, in four ordered tiers:
//
//  1. Is any owner of the same address family as addr present at all? If
//     not, addr can be neither verified nor refuted — an IPv6 address
//     against an IPv4-only resolver (or the 4-in-6-mapped form of an address
//     that would otherwise fall inside an IPv4 network) is unverifiable,
//     which is not the same as "no boundary".
//  2. Among same-family owners, does any geographically contain addr,
//     regardless of view? If none do, there was no Microsoft boundary to be
//     unverifiable within (D-06) — silent retain.
//  3. Among the containing owners, does the child's own view match any of
//     them? A missing child view never matches any view. If none match,
//     a boundary exists but cannot be verified across views.
//  4. Among the view-matched containing owners, the strict-longest prefix
//     wins; a tie at the longest length is ambiguous and never resolved by
//     slice order.
//
// The family comparison deliberately does NOT call Unmap() on addr first:
// Unmap would convert a 4-in-6-mapped address into pure IPv4 form and make
// it wrongly match an IPv4-only resolver, defeating tier 1's guard. Is4()
// is already false for both a genuine IPv6 address and a 4-in-6-mapped one,
// which is exactly the guard tier 1 needs.
func (r *msContainmentResolver) resolveMSChild(addr, view string) (serverOID string) {
	parsed, err := netip.ParseAddr(addr)
	if err != nil {
		return ""
	}
	if r == nil || len(r.owners) == 0 {
		return ""
	}
	is4 := parsed.Is4()

	sameFamily := false
	for _, owner := range r.owners {
		if owner.prefix.Addr().Is4() == is4 {
			sameFamily = true
			break
		}
	}
	if !sameFamily {
		return ""
	}

	var containing []msNetworkOwner
	for _, owner := range r.owners {
		if owner.prefix.Addr().Is4() == is4 && owner.prefix.Contains(parsed) {
			containing = append(containing, owner)
		}
	}
	if len(containing) == 0 {
		return ""
	}

	var viewMatched []msNetworkOwner
	if view != "" {
		for _, owner := range containing {
			if owner.view == view {
				viewMatched = append(viewMatched, owner)
			}
		}
	}
	if len(viewMatched) == 0 {
		return ""
	}

	bestLen := -1
	var best []msNetworkOwner
	for _, owner := range viewMatched {
		switch {
		case owner.prefixLen > bestLen:
			bestLen = owner.prefixLen
			best = []msNetworkOwner{owner}
		case owner.prefixLen == bestLen:
			best = append(best, owner)
		}
	}
	if len(best) > 1 {
		return ""
	}
	return best[0].serverOID
}

// dhcpParentNetworkKey extracts the "address/cidr/view" segment from a
// $-delimited parent reference (".com.infoblox.dns.network$ADDRESS/CIDR/VIEW"),
// mirroring the split counter.go's NiosFamilyDHCPOption case already performs
// on the identical reference shape (counter.go:526). Returns "" if parent
// carries no "$".
func dhcpParentNetworkKey(parent string) string {
	idx := strings.Index(parent, "$")
	if idx < 0 {
		return ""
	}
	return parent[idx+1:]
}

// firstNonEmpty returns the first argument that is non-empty after trimming,
// or "" if all are empty. Used where a property name is uncertain across
// schema variants — 05-SCHEMA.md never censused exclusion_range's own shape.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// classifyDHCPExclusionRange resolves an exclusion_range — the one DHCP
// family in this phase's scope with no direct ms_server reference of its own
// (05-SCHEMA.md). Two steps, in order:
//
//  1. An exact match on the object's own parent-network reference against
//     dhcpNetworkResolution — the same verified relationship
//     classifyDHCPNetwork consults. This is D-05's exact-reference path, not
//     containment, and short-circuits step 2 entirely on a hit.
//  2. D-06 longest-prefix containment via resolveMSChild.
//
// 05-SCHEMA.md never censused exclusion_range's own property shape, so the
// address is read from the first non-empty of three plausible property names
// (start_address, start_addr, address) rather than betting on one — these
// are the only three keys this code reads; a later probe run should confirm
// or correct them against real data.
func (l *msLedgerState) classifyDHCPExclusionRange(props map[string]string) {
	if key := dhcpParentNetworkKey(props["parent"]); key != "" {
		if res, ok := l.dhcpNetworkResolution[key]; ok && res.attributable {
			l.creditDDIAttributable(MSServiceDHCP)
			return
		}
	}

	addr := firstNonEmpty(props["start_address"], props["start_addr"], props["address"])
	if addr == "" {
		l.creditDDIRetained(MSServiceDHCP)
		return
	}
	// resolveMSChild only ever names an owner when containment resolved
	// cleanly, so a non-empty result is itself the verified verdict.
	if server := l.containment.resolveMSChild(addr, props["network_view"]); server != "" {
		l.creditDDIAttributable(MSServiceDHCP)
		return
	}
	l.creditDDIRetained(MSServiceDHCP)
}

// classifyDHCPObject credits exactly one of Attributable or Retained for a
// DHCP-family DDI object already counted in the baseline (gridDDI), or routes
// a fixed_address/lease object into Task 3's Active-IP-only accounting
// (D-08: neither family is a DDI object, so neither ever touches
// DDIObjects). Mirrors classifyDNSObject's contract (never returns without
// crediting) for the three DDI-bearing families:
//   - NiosFamilyNetwork: relationship-only (classifyDHCPNetwork).
//   - NiosFamilyDHCPRange: direct dhcp_range.ms_server reference
//     (resolveDirectMSRef).
//   - NiosFamilyExclusionRange: parent exact-match, then D-06 containment
//     (classifyDHCPExclusionRange).
//   - NiosFamilyFixedAddress / NiosFamilyLease: Active-IP-only, direct
//     reference (classifyDHCPFixedAddress / classifyDHCPLease).
//
// msDHCPClassifiedFamilies is the set of DDI families counter.go routes to
// classifyDHCPObject; everything outside it goes to classifyDNSObject.
//
// network_container and network_view are IPAM objects, not DNS ones: they used
// to fall through the routing else-branch into the DNS classifier, which on a
// real grid attributed five figures of IPAM to the wrong service. The ledger
// has exactly two services, and these sit on the same side of the grid as
// network (already routed here at counter.go:520). Neither can ever be
// Microsoft-attributable, which is what classifyDHCPObject's default branch
// records.
var msDHCPClassifiedFamilies = map[string]struct{}{
	NiosFamilyDHCPRange:        {},
	NiosFamilyExclusionRange:   {},
	NiosFamilyNetworkContainer: {},
	NiosFamilyNetworkView:      {},
}

func (l *msLedgerState) classifyDHCPObject(family string, props map[string]string) {
	switch family {
	case NiosFamilyNetwork:
		l.classifyDHCPNetwork(props)
	case NiosFamilyDHCPRange:
		verified := l.resolveDirectMSRef(props["ms_server"])
		if verified {
			l.creditDDIAttributable(MSServiceDHCP)
			return
		}
		l.creditDDIRetained(MSServiceDHCP)
	case NiosFamilyExclusionRange:
		l.classifyDHCPExclusionRange(props)
	case NiosFamilyFixedAddress:
		l.classifyDHCPFixedAddress(props)
	case NiosFamilyLease:
		l.classifyDHCPLease(props)
	default:
		// An IPAM family (network_container, network_view): a DDI object no
		// Microsoft switch can move, retained under the same reason code the
		// DNS side files for its own unmovable families. Crediting exactly one
		// bucket here is what keeps the conservation gate satisfied — falling
		// out of this switch uncredited would suppress the whole ledger.
		l.creditDDIRetained(MSServiceDHCP)
	}
}

// msNormalizeAddress parses ip and returns its canonical form for use as a
// set key in either of Task 3's two tiers. Unmap collapses an
// IPv4-mapped-IPv6 spelling ("::ffff:192.0.2.5") to plain IPv4 ("192.0.2.5") so
// both tiers key consistently regardless of which form the backup used. An
// empty or unparseable value returns ok=false; the caller decides what that
// means for its own family — an empty ip_address is not a baseline unit at
// all, while a non-empty unparseable one is a malformed baseline unit (see
// classifyMSActiveIPUnit).
func msNormalizeAddress(ip string) (string, bool) {
	parsed, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return "", false
	}
	return parsed.Unmap().String(), true
}

// creditMSActiveIP offers ip (already normalized) to Task 3's tier-2
// cross-category union, applying the same first-seen-wins
// map[string]struct{} idiom counter.go's activeLeaseIPSet uses
// (counter.go:394-395). Reports whether the insertion was new —
// classifyMSActiveIPUnit uses that to distinguish a fresh union member
// (Attributable is read from the union's cardinality once, after Pass 2, so
// a new member needs no separate credit here) from the cross-category
// union-surplus case (credits Retained).
func (l *msLedgerState) creditMSActiveIP(ip string) bool {
	if _, exists := l.msActiveIPSet[ip]; exists {
		return false
	}
	l.msActiveIPSet[ip] = struct{}{}
	return true
}

// classifyMSActiveIPUnit is Task 3's shared per-object accounting step for
// one fixed_address or active lease object (D-07), gated by categorySeen
// (the caller's own tier-1 set — msFixedIPSeen or msActiveLeaseIPSeen) and
// rawServer (the object's own direct reference — ms_server for a fixed
// reservation, ms_server_id for a lease). The caller has already applied any
// family-specific gate: an empty ip_address, or a non-active lease, is not a
// baseline unit at all and must never reach this method (see
// classifyDHCPFixedAddress / classifyDHCPLease).
//
// Four steps, in order:
//  1. Normalize the address. An unparseable-but-non-empty value IS a
//     baseline unit (counter.go's own sets key on the raw string with no
//     validation) but this method cannot dedup, resolve, or union it as an
//     address, so it never enters categorySeen or the tier-2 union (D-10) —
//     it always credits Retained with ReasonMalformedValue. A byte-identical
//     repeat of the same malformed string is a known, accepted gap against
//     counter.go's raw-string dedup: it would need a corrupted backup with
//     two identical unparseable ip_address values to surface.
//  2. Already in categorySeen? The baseline already collapsed this object to
//     zero extra units (the common case: a DHCP-failover lease replica) — a
//     same-category duplicate credits NEITHER partition and stops here.
//  3. Otherwise this object IS a baseline unit: insert into categorySeen,
//     then resolve rawServer exactly like Task 1's dhcp_range path
//     (resolveDirectMSRef).
//  4. A verified reference offers the address to the tier-2 union. A new
//     union member needs no explicit credit here (Attributable is read from
//     the union's cardinality once, after Pass 2 completes, in scanner.go);
//     an address already in the union is the CROSS-category union-surplus
//     case (D-07) and credits Retained instead. An unverified reference
//     credits Retained.
func (l *msLedgerState) classifyMSActiveIPUnit(categorySeen map[string]struct{}, family, rawIP, rawServer string) {
	ip, ok := msNormalizeAddress(rawIP)
	if !ok {
		l.activeIPs.Retained++
		return
	}
	if _, dup := categorySeen[ip]; dup {
		return
	}
	categorySeen[ip] = struct{}{}

	verified := l.resolveDirectMSRef(rawServer)
	if verified {
		if !l.creditMSActiveIP(ip) {
			l.activeIPs.Retained++ // cross-category union-surplus (D-07)
		}
		return
	}
	l.activeIPs.Retained++
}

// classifyDHCPFixedAddress applies Task 3's Active-IP-only accounting to one
// fixed_address object. Per D-08 a fixed reservation contributes through
// Active IPs only — this method never touches DDIObjects. An empty
// ip_address is not a baseline unit at all (mirrors counter.go:411's own
// `if ip != ""` gate) and is skipped before any set or partition is touched.
func (l *msLedgerState) classifyDHCPFixedAddress(props map[string]string) {
	if strings.TrimSpace(props["ip_address"]) == "" {
		return
	}
	l.classifyMSActiveIPUnit(l.msFixedIPSeen, NiosFamilyFixedAddress, props["ip_address"], props["ms_server"])
}

// classifyDHCPLease applies Task 3's Active-IP-only accounting to one lease
// object, gated by the same binding_state=="active" guard counter.go's own
// baseline case uses (counter.go:389) — a non-active lease, or one with an
// empty ip_address, is not a baseline unit at all. Reads ms_server_id, not
// ms_server (05-SCHEMA.md: differing property name on this family). The
// failover replica on a lease's second peer carries the identical address
// and binding_state and is collapsed by msActiveLeaseIPSeen — vnode_id is
// never compared, and .com.infoblox.dns.dhcp_failover_association is
// deliberately never indexed: the union already collapses the replica by
// address alone, so a failover-association join would add a Pass 1 index and
// a join for zero behavioural difference. That was considered and declined —
// this comment is the record so it is not re-litigated.
func (l *msLedgerState) classifyDHCPLease(props map[string]string) {
	if props["binding_state"] != "active" || strings.TrimSpace(props["ip_address"]) == "" {
		return
	}
	l.classifyMSActiveIPUnit(l.msActiveLeaseIPSeen, NiosFamilyLease, props["ip_address"], props["ms_server_id"])
}

// msDNSRecordExplicitList is the D-02 allowlist of fifteen typed DNS record
// families eligible for Microsoft DNS attribution. NiosFamilyDNSZone is
// credited by the relationship path — classifyDNSObject handles it before
// ever consulting this set, never through it. NiosFamilyHostObject and
// NiosFamilyHostAlias are NIOS-specific constructs, not typed DNS records:
// both remain DDIFamilies members and are retained under
// ReasonUnsupportedType. The D-02 clarification ratified 2026-08-13 locks
// this list at exactly 15 entries — do not widen it to admit any of the
// three excluded families.
var msDNSRecordExplicitList = []string{
	NiosFamilyDNSRecordA,
	NiosFamilyDNSRecordAAAA,
	NiosFamilyDNSRecordCNAME,
	NiosFamilyDNSRecordMX,
	NiosFamilyDNSRecordNS,
	NiosFamilyDNSRecordPTR,
	NiosFamilyDNSRecordSOA,
	NiosFamilyDNSRecordSRV,
	NiosFamilyDNSRecordTXT,
	NiosFamilyDNSRecordCAA,
	NiosFamilyDNSRecordNAPTR,
	NiosFamilyDNSRecordHTTPS,
	NiosFamilyDNSRecordSVCB,
	NiosFamilyDNSRecordAlias,
	NiosFamilyDNAME,
}

// msDNSRecordFamilies is msDNSRecordExplicitList filtered to DDIFamilies
// membership, computed once at package init. Iterating DDIFamilies — rather
// than trusting the explicit list outright — means a family later removed
// from DDIFamilies cannot linger here, and a family later added to
// DDIFamilies does not silently become Microsoft-attributable without a
// corresponding edit to msDNSRecordExplicitList above.
var msDNSRecordFamilies = func() map[string]struct{} {
	explicit := make(map[string]struct{}, len(msDNSRecordExplicitList))
	for _, f := range msDNSRecordExplicitList {
		explicit[f] = struct{}{}
	}
	out := make(map[string]struct{}, len(explicit))
	for family := range DDIFamilies {
		if _, ok := explicit[family]; ok {
			out[family] = struct{}{}
		}
	}
	return out
}()

// msZoneOwnRef derives a zone object's own reference key — the key
// zone_ms_primary_server rows and child records are indexed under — from the
// parent reference the object itself carries plus its fqdn.
//
// A reverse-zone object's "zone" property points at its parent zone, while the
// relationship row claiming it carries the child's full reversed key. Looking
// a zone up under its parent key therefore never finds that zone's own claim
// and can silently hand it its parent's verdict.
//
// The key format is the view prefix followed by the fqdn's labels in reverse,
// so only the view is taken from parentRef; the rest is rebuilt from fqdn.
// Verified against a production backup: all 99 relationship keys resolve.
// Returns "" when either input is missing or parentRef is not a reference at
// all — an unresolvable key is not attributable, which is the same answer the
// caller already gives an unclaimed zone.
func msZoneOwnRef(parentRef, fqdn string) string {
	fqdn = strings.Trim(strings.TrimSpace(fqdn), ".")
	if fqdn == "" || !strings.HasPrefix(parentRef, ".") {
		return ""
	}
	view := parentRef
	if idx := strings.Index(parentRef[1:], "."); idx >= 0 {
		view = parentRef[:idx+1]
	}
	var b strings.Builder
	b.WriteString(view)
	labels := strings.Split(fqdn, ".")
	for i := len(labels) - 1; i >= 0; i-- {
		b.WriteByte('.')
		b.WriteString(labels[i])
	}
	return b.String()
}

// classifyDNSObject credits exactly one of Attributable or Retained for a DDI
// object that has already been counted in the baseline (gridDDI). It must
// never return without crediting: every family it is handed already
// incremented gridDDI, so a silent skip would break checkMSConservation's
// invariant.
//
// Family eligibility is evaluated FIRST: a family absent from
// msDNSRecordFamilies (a NIOS-specific construct such as a host object or
// host alias, or any other DDIFamilies member outside the typed-DNS-record
// allowlist) is retained under ReasonUnsupportedType regardless of its zone's
// attribution state — that answer never depends on the zone at all.
// NiosFamilyDNSZone is handled before the eligibility check, since a zone is
// credited by the relationship path, not by record-type membership.
//
// Only an eligible record then consults its own zone: an attributed zone
// credits Attributable, and a non-attributed zone credits Retained — the
// record's non-attribution is explained by its zone, not by the record
// (D-02).
func (l *msLedgerState) classifyDNSObject(family string, props map[string]string) {
	res, ok := l.zoneResolution[props["zone"]]

	if family == NiosFamilyDNSZone {
		// A zone object's own "zone" property is its PARENT's reference, not
		// its own, so the lookup above answered a question about the wrong
		// zone; re-resolve on the derived own-reference instead.
		res, ok = l.zoneResolution[msZoneOwnRef(props["zone"], props["fqdn"])]
		if ok && res.attributable {
			l.creditDDIAttributable(MSServiceDNS)
			return
		}
		l.creditDDIRetained(MSServiceDNS)
		if ok {
		}
		return
	}

	if _, eligible := msDNSRecordFamilies[family]; !eligible {
		l.creditDDIRetained(MSServiceDNS)
		return
	}

	if ok && res.attributable {
		l.creditDDIAttributable(MSServiceDNS)
		return
	}
	l.creditDDIRetained(MSServiceDNS)
}

// build assembles the ledger from the accumulated partitions, runs the
// conservation gate, and returns either a conserving ledger with an
// Available diagnostic, or a nil ledger with a suppression diagnostic.
// Order: absent (no Microsoft server at all) takes priority over unavailable
// (a server was seen but the gate failed).
func (l *msLedgerState) build() (*MicrosoftAllocationLedger, MSAllocationDiagnostic) {
	if !l.sawMSServer {
		return nil, MSAllocationDiagnostic{Available: false, Code: MSAllocationAbsentCode}
	}

	ledger := &MicrosoftAllocationLedger{
		DDIObjects:     l.ddiObjects,
		DDIObjectsDNS:  l.ddiObjectsDNS,
		DDIObjectsDHCP: l.ddiObjectsDHCP,
		ActiveIPs:      l.activeIPs,
		ManagedAssets:  l.managedAssets,
		Evidence:       l.evidence,
		Servers:        l.servers,
	}

	if failing := msConservationCheck(ledger); len(failing) > 0 {
		return nil, MSAllocationDiagnostic{
			Available: false,
			Code:      MSAllocationUnavailableCode,
			Message:   msAllocationUnavailableMessage,
		}
	}

	if failing := msLedgerInvariants(ledger); len(failing) > 0 {
		return nil, MSAllocationDiagnostic{
			Available: false,
			Code:      MSAllocationUnavailableCode,
			Message:   msAllocationUnavailableMessage,
		}
	}

	return ledger, MSAllocationDiagnostic{Available: true}
}

// msLedgerInvariants returns the names of every violated structural
// invariant in l, checked independently of checkMSConservation's arithmetic.
// A structurally malformed ledger — any non-zero ManagedAssets value (Phase 5
// carries no managed-asset attribution at all) — is as untrustworthy as one
// that fails to conserve, so build suppresses on either gate identically
// (D-14). A nil ledger is reported as failing rather than
// panicking, mirroring checkMSConservation's own nil handling.
func msLedgerInvariants(l *MicrosoftAllocationLedger) []string {
	if l == nil {
		return []string{"nil ledger"}
	}

	var failing []string

	if l.ManagedAssets != (MSCategoryPartition{}) {
		failing = append(failing, "ManagedAssets is non-zero — Phase 5 carries no managed-asset attribution")
	}

	return failing
}
