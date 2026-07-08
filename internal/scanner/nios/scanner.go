// Package nios implements the NIOS backup scanner for Phase 10.
// scanner.go implements a two-pass streaming XML parser:
//
//	Pass 1: build vnodeMap (vnode_id → hostname), find Grid Master, AND build
//	        memberResolver (zone → member, network → member) from ns_group + dhcp_member.
//	Pass 2: stream-count DDI objects with per-member attribution via resolver.
//
// Performance optimizations for large backups (100MB+ compressed, 2.5M+ objects):
//   - Auto-detects raw XML vs gzip+tar (supports pre-extracted onedb.xml from upload)
//   - Type-filtered parsing skips map allocation for irrelevant XML objects
//   - Reusable property buffer avoids per-object allocations
//   - Stream counting eliminates intermediate []parsedObject slice
//   - Buffered I/O (256KB) for file reads
package nios

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/calculator"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/scanner"
)

// Scanner is the NIOS provider implementation.
// It implements both scanner.Scanner and scanner.NiosResultScanner.
type Scanner struct {
	mu             sync.Mutex
	metrics        []NiosServerMetric
	gridFeatures   *NiosGridFeatures
	gridLicenses   *NiosGridLicenses
	migrationFlags *NiosMigrationFlags
	ipBreakdown    *NiosActiveIPBreakdown

	microsoftServers *NiosMicrosoftServers
}

// NiosActiveIPBreakdown exposes the grid-level Active-IP and per-family DDI counts
// produced by the last Scan(). It is the measurement surface for cmd/niosbench, which
// compares these values against the corporate DB-analyzer Summary_Report. Counting logic
// lives entirely in the scanner; this struct only reports what Scan() computed.
type NiosActiveIPBreakdown struct {
	FixedAddress        int            // len(fixedIPSet)
	ActiveLeases        int            // corporate-parity active leases (ActiveLeasesRaw + corporate +1)
	ActiveLeasesRaw     int            // true distinct active-lease IPs (len(activeLeaseIPSet))
	NetworkReservations int            // 2 × NetworkCount
	HostAddress         int            // corporate-parity host addresses (HostAddressRaw + corporate +1)
	HostAddressRaw      int            // true distinct host addresses (excl configure_for_dhcp)
	NetworkCount        int            // number of network objects
	UDDIActiveIPs       int            // FixedAddress + ActiveLeases + NetworkReservations
	ManagedActiveIPs    int            // UDDIActiveIPs + HostAddress
	FamilyCounts        map[string]int // NiosFamily → DDI count (weighted ×1)
}

// ActiveIPBreakdown returns the grid-level Active-IP / DDI breakdown from the last Scan().
// Returns nil if Scan() has not been called.
func (s *Scanner) ActiveIPBreakdown() *NiosActiveIPBreakdown {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ipBreakdown
}

// New returns a new NIOS Scanner.
func New() *Scanner { return &Scanner{} }

// GetNiosServerMetricsJSON returns JSON-encoded []NiosServerMetric after Scan() completes.
// Returns nil if Scan() has not been called or produced no metrics.
func (s *Scanner) GetNiosServerMetricsJSON() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.metrics) == 0 {
		return nil
	}
	data, err := json.Marshal(s.metrics)
	if err != nil {
		return nil
	}
	return data
}

// GetNiosGridFeaturesJSON returns JSON-encoded NiosGridFeatures after Scan() completes.
// Returns nil if Scan() has not been called or no features were detected.
func (s *Scanner) GetNiosGridFeaturesJSON() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gridFeatures == nil {
		return nil
	}
	data, err := json.Marshal(s.gridFeatures)
	if err != nil {
		return nil
	}
	return data
}

// GetNiosGridLicensesJSON returns JSON-encoded NiosGridLicenses after Scan() completes.
// Returns nil if Scan() has not been called or no grid licenses were found.
func (s *Scanner) GetNiosGridLicensesJSON() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gridLicenses == nil {
		return nil
	}
	data, err := json.Marshal(s.gridLicenses)
	if err != nil {
		return nil
	}
	return data
}

// GetNiosMigrationFlagsJSON returns JSON-encoded NiosMigrationFlags after Scan() completes.
// Returns nil if Scan() has not been called or produced no migration flags.
func (s *Scanner) GetNiosMigrationFlagsJSON() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.migrationFlags == nil {
		return nil
	}
	data, err := json.Marshal(s.migrationFlags)
	if err != nil {
		return nil
	}
	return data
}

// GetNiosMicrosoftServersJSON returns JSON-encoded NiosMicrosoftServers after Scan().
// Returns nil if no Grid-managed Microsoft servers were found.
func (s *Scanner) GetNiosMicrosoftServersJSON() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.microsoftServers == nil || len(s.microsoftServers.Servers) == 0 {
		return nil
	}
	data, err := json.Marshal(s.microsoftServers)
	if err != nil {
		return nil
	}
	return data
}

// pass1Types contains __type values needed for the merged Pass 1:
// member discovery (virtual_node) + resolver construction (ns_group, dhcp_member, zone).
var pass1Types = map[string]struct{}{
	".com.infoblox.one.virtual_node":               {},
	".com.infoblox.one.physical_node":              {}, // hwtype/hwplatform + license → member linkage via physical_oid → virtual_node
	".com.infoblox.dns.ns_group_grid_primary":      {},
	".com.infoblox.dns.ns_group_forwarding_server": {}, // forward zones use forward_address, not grid_member
	".com.infoblox.dns.dhcp_member":                {},
	".com.infoblox.dns.zone":                       {},
	".com.infoblox.dns.bind_soa":                   {}, // SOA records for zone member fallback (mname property)
	".com.infoblox.dns.member_dns_properties":      {},
	".com.infoblox.dns.member_dhcp_properties":     {},
	".com.infoblox.one.product_license":            {}, // per-node license inventory
	".com.infoblox.one.license_grid_wide":          {}, // grid-wide license types
	".com.infoblox.one.vnode_time":                 {}, // NTP feature detection
	".com.infoblox.one.datacollector_cluster":      {}, // Data Connector feature detection
	".com.infoblox.one.ms_server":                  {}, // Microsoft-managed server
	".com.infoblox.dns.ms_server_dns_properties":   {}, // MS DNS managed flag
	".com.infoblox.dns.ms_server_dhcp_properties":  {}, // MS DHCP managed flag + host count
	".com.infoblox.dns.zone_ms_primary_server":     {}, // MS-managed primary DNS zones
}

// pass2Types contains __type values for Pass 2 (object counting).
// Built from XMLTypeToFamily keys at init time.
var pass2Types map[string]struct{}

func init() {
	pass2Types = make(map[string]struct{}, len(XMLTypeToFamily))
	for k := range XMLTypeToFamily {
		pass2Types[k] = struct{}{}
	}
}

// Scan implements scanner.Scanner. It performs a two-pass streaming parse of the
// NIOS backup referenced by req.Credentials["backup_path"].
//
// The backup_path may point to either a raw onedb.xml file (pre-extracted during
// upload) or a gzip+tar archive (test fixtures). Format is auto-detected.
//
// Pass 1 (merged): builds vnode_id → hostname map, identifies Grid Master, AND
// collects ns_group/dhcp_member/zone data for the member resolver.
// Pass 2: stream-counts DDI objects with per-member attribution via resolver.
//
// The temp file at backup_path is deleted via defer after the scan completes.
func (s *Scanner) Scan(_ context.Context, req scanner.ScanRequest, publish func(scanner.Event)) ([]calculator.FindingRow, error) {
	backupPath := req.Credentials["backup_path"]
	if backupPath == "" {
		return nil, fmt.Errorf("nios: backup_path not set in credentials")
	}

	// Delete temp file when done — only if the path is inside os.TempDir()
	// so test fixtures in testdata/ are not accidentally deleted.
	if strings.HasPrefix(filepath.Clean(backupPath), filepath.Clean(os.TempDir())) {
		defer os.Remove(backupPath)
	}

	// Parse selected members filter.
	selectedSet := make(map[string]struct{})
	if sm := req.Credentials["selected_members"]; sm != "" {
		for _, h := range strings.Split(sm, ",") {
			h = strings.TrimSpace(h)
			if h != "" {
				selectedSet[h] = struct{}{}
			}
		}
	}

	// ---- Pass 1 (merged): members + resolver data in a single XML pass ----
	state := newPass1State()

	publish(scanner.Event{
		Type:     "resource_progress",
		Provider: "nios",
		Resource: "pass1_start",
		Count:    0,
	})

	if err := streamOnedbXMLFiltered(backupPath, pass1Types, state.record); err != nil {
		return nil, fmt.Errorf("nios: pass 1 failed: %w", err)
	}
	state.finalize()

	publish(scanner.Event{
		Type:     "resource_progress",
		Provider: "nios",
		Resource: "members",
		Count:    len(state.vnodeMap),
	})

	// ---- Build member resolver + per-member licenses from Pass 1 data ----
	resolver := state.buildResolver()
	memberLicenses := state.buildLicensesByMember()

	// Hoist commonly-used fields from state for the rest of Scan.
	vnodeMap := state.vnodeMap
	memberProps := state.memberProps
	gmHostname := state.gmHostname
	gridFeatures := state.gridFeatures
	gridLicenseTypes := state.gridLicenseTypes
	// ---- Pass 2: stream counting with per-object processing ----
	publish(scanner.Event{
		Type:     "resource_progress",
		Provider: "nios",
		Resource: "pass2_start",
		Count:    0,
	})

	result := newCountResult()
	objectCount := 0

	if err := streamOnedbXMLFiltered(backupPath, pass2Types, func(props map[string]string) {
		xmlType := props["__type"]
		family, ok := XMLTypeToFamily[xmlType]
		if !ok {
			return
		}
		objectCount++
		// DNAME feature detection: any bind_dname object means the feature is present.
		if xmlType == ".com.infoblox.dns.bind_dname" {
			gridFeatures.DNAMERecords = true
		}
		result.processObject(family, props, props["vnode_id"], vnodeMap, gmHostname, resolver)
	}); err != nil {
		return nil, fmt.Errorf("nios: pass 2 failed: %w", err)
	}

	publish(scanner.Event{
		Type:     "resource_progress",
		Provider: "nios",
		Resource: "objects",
		Count:    objectCount,
	})

	publish(scanner.Event{
		Type:     "resource_progress",
		Provider: "nios",
		Resource: "counting",
		Count:    result.gridDDI,
	})

	// ---- Build NiosMigrationFlags from pass 2 results ----
	if len(result.dhcpOptions) > 0 || len(result.hostRoutes) > 0 {
		dhcpOpts := result.dhcpOptions
		if dhcpOpts == nil {
			dhcpOpts = []DHCPOptionFlag{}
		}
		hostRoutes := result.hostRoutes
		if hostRoutes == nil {
			hostRoutes = []HostRouteFlag{}
		}
		s.mu.Lock()
		s.migrationFlags = &NiosMigrationFlags{
			DHCPOptions: dhcpOpts,
			HostRoutes:  hostRoutes,
		}
		s.mu.Unlock()
	}

	// ---- Build FindingRows ----
	var rows []calculator.FindingRow

	// Emit per-member DDI rows: each member gets rows for the DDI families attributed to it.
	for hostname, familyMap := range result.memberDDI {
		for family, count := range familyMap {
			if count == 0 {
				continue
			}
			displayName := familyDisplayName(family)
			rows = append(rows, calculator.FindingRow{
				Provider:         "nios",
				Source:           hostname,
				Category:         calculator.CategoryDDIObjects,
				Item:             displayName,
				Count:            count,
				TokensPerUnit:    calculator.NIOSTokensPerDDIObject,
				ManagementTokens: ceilDiv(count, calculator.NIOSTokensPerDDIObject),
			})
		}
	}

	// Emit rows for unresolved DDI objects (attributed to GM as fallback).
	for family, count := range result.unresolvedDDI {
		if count == 0 {
			continue
		}
		displayName := familyDisplayName(family) + " (grid-managed, unattributed)"
		rows = append(rows, calculator.FindingRow{
			Provider:         "nios",
			Source:           gmHostname,
			Category:         calculator.CategoryDDIObjects,
			Item:             displayName,
			Count:            count,
			TokensPerUnit:    calculator.NIOSTokensPerDDIObject,
			ManagementTokens: ceilDiv(count, calculator.NIOSTokensPerDDIObject),
		})
	}

	// Per-member Active IPs — one row per member with nonzero owned count.
	//
	// UDDI Active IPs = fixed + active-leases + 2*networks. Each fixed-address IP,
	// active-lease IP, and network-reservation pair is credited to exactly ONE
	// member (ownedActiveIPCount / ownedNetworkReservations, set in processObject —
	// first member encountered in file order wins), so Σ(per-member counts) equals
	// gridUDDIActiveIPs() exactly. This is architecturally different from
	// NiosServerMetrics.ActiveIPCount (buildMetrics), which deliberately keeps
	// DHCP-failover replicas on both peers for corporate-parity display — these two
	// counters are intentionally disjoint. Per-member attribution lets the frontend
	// Migration Planner's marginal-delta preview (computeMarginalDelta in
	// mock-data.ts) correctly move Active IP tokens when a single member toggles
	// between "staying" and "migrating".
	activeIPByMember := make(map[string]int, len(result.memberAccs)+1)
	for hostname, acc := range result.memberAccs {
		if n := acc.ownedActiveIPCount + acc.ownedNetworkReservations; n > 0 {
			activeIPByMember[hostname] += n
		}
	}
	// Unresolved network reservations and the corporate +1 active-lease off-by-one
	// mirror (see activeLeaseCount()) are attributed to the Grid Master, matching
	// the existing unresolvedDDI convention used for the DDI Objects category.
	gmExtra := result.unresolvedNetworkReservations
	if result.activeLeaseCount() > len(result.activeLeaseIPSet) {
		gmExtra++
	}
	if gmExtra > 0 {
		activeIPByMember[gmHostname] += gmExtra
	}
	for hostname, n := range activeIPByMember {
		if n == 0 {
			continue
		}
		rows = append(rows, calculator.FindingRow{
			Provider:         "nios",
			Source:           hostname,
			Category:         calculator.CategoryActiveIPs,
			Item:             "Active IPs",
			Count:            n,
			TokensPerUnit:    calculator.NIOSTokensPerActiveIP,
			ManagementTokens: ceilDiv(n, calculator.NIOSTokensPerActiveIP),
		})
	}

	// ---- Apply selectedMembers filter ----
	// If selectedMembers is non-empty, only emit rows for hostnames in the set.
	// GM is always included even if not in selectedMembers.
	if len(selectedSet) > 0 {
		filtered := rows[:0]
		for _, row := range rows {
			if row.Source == gmHostname {
				filtered = append(filtered, row)
				continue
			}
			if _, ok := selectedSet[row.Source]; ok {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}

	// ---- Build grid-level Active-IP / DDI breakdown (harness measurement surface) ----
	familyCounts := make(map[string]int, len(result.familyCounts))
	for fam, c := range result.familyCounts {
		familyCounts[fam] = c
	}
	breakdown := &NiosActiveIPBreakdown{
		FixedAddress:        len(result.fixedIPSet),
		ActiveLeases:        result.activeLeaseCount(), // corporate-parity (mirrors +1 off-by-one)
		ActiveLeasesRaw:     len(result.activeLeaseIPSet),
		NetworkReservations: 2 * result.networkCount(),
		HostAddress:         result.hostAddressCount(), // corporate-parity (mirrors +1 off-by-one)
		HostAddressRaw:      len(result.hostAddressIPSet),
		NetworkCount:        result.networkCount(),
		UDDIActiveIPs:       result.gridUDDIActiveIPs(),
		ManagedActiveIPs:    result.gridManagedIPs(),
		FamilyCounts:        familyCounts,
	}

	// ---- Build NiosServerMetrics ----
	s.mu.Lock()
	s.metrics = buildMetrics(vnodeMap, memberProps, result, gmHostname, memberLicenses)
	s.gridFeatures = &gridFeatures
	s.gridLicenses = &NiosGridLicenses{Types: gridLicenseTypes}
	s.ipBreakdown = breakdown
	s.microsoftServers = state.ms.build()
	s.mu.Unlock()

	return rows, nil
}

// buildMetrics constructs NiosServerMetric entries for each member in vnodeMap.
// ObjectCount uses member-attributed DDI from countResult.memberDDI.
// Unresolved DDI is NOT folded into any member's ObjectCount (it still flows into
// grid-total management-token math separately, keyed by gmHostname).
func buildMetrics(
	vnodeMap map[string]string,
	memberProps map[string]map[string]string,
	result countResult,
	gmHostname string,
	memberLicenses map[string]map[string]bool,
) []NiosServerMetric {
	metrics := make([]NiosServerMetric, 0, len(vnodeMap))
	for _, hostname := range vnodeMap {
		props := memberProps[hostname]
		role := extractServiceRole(props)
		runsDnsDhcp := hasDnsOrDhcp(props)

		objectCount := 0
		activeIPCount := 0
		managedIPCount := 0
		if acc, ok := result.memberAccs[hostname]; ok {
			objectCount = acc.ddiCount
			activeIPCount = len(acc.memberIPSet)
			// Managed IPs = UDDI IPs + host_address IPs, with dedup for overlap.
			overlap := 0
			for ip := range acc.hostAddressIPSet {
				if _, inMember := acc.memberIPSet[ip]; inMember {
					overlap++
				}
			}
			managedIPCount = len(acc.memberIPSet) + len(acc.hostAddressIPSet) - overlap
		}
		// NOTE: unresolved DDI is intentionally NOT added to the GM's objectCount here.
		// It still flows into management-token FindingRows keyed by gmHostname
		// (scanner.go, grid-total-preserving), but must never inflate the GM's own
		// sizing/ObjectCount display value (see 2026-07-08 mode-aware GM spec).

		// ServerObjectCount is sizing-only (NIOS-X form-factor tier): DDI ObjectCount
		// plus this member's own active leases and fixed addresses, counted per-member
		// and undeduped across failover peers. It must never feed ObjectCount or
		// management-token math.
		serverObjectCount := objectCount
		if acc, ok := result.memberAccs[hostname]; ok {
			serverObjectCount += len(acc.leaseIPSet) + len(acc.fixedIPSet)
		}

		// Extract hardware model and platform from physical_node data stored in Pass 1.
		model := ""
		platform := ""
		if props != nil {
			model = props["_hwtype"]
			if hp := props["_hwplatform"]; hp != "" {
				platform = classifyPlatform(hp)
			}
		}

		// DHCP stats from positional-matched memberProps.
		staticHosts := 0
		dynamicHosts := 0
		dhcpUtil := 0
		if props != nil {
			if v := props["_static_hosts"]; v != "" {
				staticHosts, _ = strconv.Atoi(v)
			}
			if v := props["_dynamic_hosts"]; v != "" {
				dynamicHosts, _ = strconv.Atoi(v)
			}
			if v := props["_dhcp_utilization"]; v != "" {
				dhcpUtil, _ = strconv.Atoi(v)
			}
		}

		licenses := memberLicenses[hostname] // may be nil, omitempty handles it

		metrics = append(metrics, NiosServerMetric{
			MemberID:          hostname,
			MemberName:        hostname,
			Role:              role,
			Model:             model,
			Platform:          platform,
			QPS:               0,
			LPS:               0,
			ObjectCount:       objectCount,
			ServerObjectCount: serverObjectCount,
			ActiveIPCount:     activeIPCount,
			ManagedIPCount:    managedIPCount,
			StaticHosts:       staticHosts,
			DynamicHosts:      dynamicHosts,
			DHCPUtilization:   dhcpUtil,
			RunsDnsDhcp:       runsDnsDhcp,
			Licenses:          licenses,
		})
	}
	return metrics
}

// classifyPlatform maps hwplatform values from physical_node objects to display labels.
// Real NIOS backup values include short codes like "VMW" and "HW" (from real-world backups),
// as well as full names like "VMware" and "Physical" (from host_platform field).
func classifyPlatform(hwplatform string) string {
	lower := strings.ToLower(strings.TrimSpace(hwplatform))
	switch {
	case lower == "" || lower == "physical" || lower == "bare-metal" || lower == "hw":
		return "Physical"
	case lower == "vmw" || lower == "vnios" || strings.Contains(lower, "vmware") || strings.Contains(lower, "vsphere"):
		return "VMware"
	case strings.Contains(lower, "aws") || strings.Contains(lower, "amazon"):
		return "AWS"
	case lower == "azr" || strings.Contains(lower, "azure") || strings.Contains(lower, "microsoft"):
		return "Azure"
	case strings.Contains(lower, "gcp") || strings.Contains(lower, "google"):
		return "GCP"
	case strings.Contains(lower, "kvm"):
		return "KVM"
	case strings.Contains(lower, "hyper-v") || strings.Contains(lower, "hyperv"):
		return "Hyper-V"
	default:
		return "Physical"
	}
}

// classifyPlatformFromModel infers platform type from the hardware model string
// (used by WAPI path where only hardware_type is available, not hwplatform).
// IB-V prefix = vNIOS (VMware), CP-V prefix = virtual Cloud Platform, IB- without V = Physical.
func classifyPlatformFromModel(model string) string {
	if model == "" {
		return ""
	}
	upper := strings.ToUpper(model)
	switch {
	case strings.HasPrefix(upper, "IB-V") || strings.HasPrefix(upper, "CP-V"):
		return "VMware"
	case strings.HasPrefix(upper, "IB-"):
		return "Physical"
	default:
		return ""
	}
}

// familyDisplayNames maps NiosFamily constants to human-readable display names
// for the FindingRow Item field.
var familyDisplayNames = map[string]string{
	NiosFamilyDNSZone:          "DNS Zones",
	NiosFamilyDNSRecordA:       "DNS A Records",
	NiosFamilyDNSRecordAAAA:    "DNS AAAA Records",
	NiosFamilyDNSRecordCNAME:   "DNS CNAME Records",
	NiosFamilyDNSRecordMX:      "DNS MX Records",
	NiosFamilyDNSRecordNS:      "DNS NS Records",
	NiosFamilyDNSRecordPTR:     "DNS PTR Records",
	NiosFamilyDNSRecordSOA:     "DNS SOA Records",
	NiosFamilyDNSRecordSRV:     "DNS SRV Records",
	NiosFamilyDNSRecordTXT:     "DNS TXT Records",
	NiosFamilyDNSRecordCAA:     "DNS CAA Records",
	NiosFamilyDNSRecordNAPTR:   "DNS NAPTR Records",
	NiosFamilyDNSRecordHTTPS:   "DNS HTTPS Records",
	NiosFamilyDNSRecordSVCB:    "DNS SVCB Records",
	NiosFamilyNetwork:          "DHCP Networks",
	NiosFamilyHostObject:       "Host Records",
	NiosFamilyHostAlias:        "Host Aliases",
	NiosFamilyFixedAddress:     "Fixed Addresses",
	NiosFamilyDHCPRange:        "DHCP Ranges",
	NiosFamilyExclusionRange:   "Exclusion Ranges",
	NiosFamilyNetworkContainer: "Network Containers",
	NiosFamilyNetworkView:      "Network Views",
	NiosFamilyDTCLBDN:          "DTC Load-Balanced Names",
	NiosFamilyDTCPool:          "DTC Pools",
	NiosFamilyDTCServer:        "DTC Servers",
	NiosFamilyDTCMonitor:       "DTC Monitors",
	NiosFamilyDTCTopology:      "DTC Topologies",
	NiosFamilyDiscoveryData:    "Discovered IPs",
	NiosFamilyDNSRecordAlias:   "DNS Alias Records",
	NiosFamilyDNAME:            "DNS DNAME Records",
}

// familyDisplayName returns the human-readable name for a NiosFamily constant.
// Falls back to "Other DDI Objects" for unmapped families.
func familyDisplayName(family string) string {
	if name, ok := familyDisplayNames[family]; ok {
		return name
	}
	return "Other DDI Objects"
}

// MergeQPSData merges peak QPS values from Splunk data into NiosServerMetric entries.
// For each metric, tries to match its MemberName against QPS map keys using:
//
//	(a) exact match on the full QPS key
//	(b) strip domain suffix from QPS key and match against member hostname prefix
//
// Returns a new slice (does not mutate input).
func MergeQPSData(metrics []NiosServerMetric, qpsData map[string]float64) []NiosServerMetric {
	if len(qpsData) == 0 {
		// Return a copy even if no QPS data, to avoid aliasing.
		result := make([]NiosServerMetric, len(metrics))
		copy(result, metrics)
		return result
	}

	// Build a short-hostname index: "fr-mop-22183-dns-1" → full QPS key and value.
	// This enables matching when member hostnames are short (no domain suffix).
	shortIndex := make(map[string]float64, len(qpsData))
	for key, val := range qpsData {
		// Extract the hostname prefix before the first dot.
		short := key
		if dotIdx := strings.IndexByte(key, '.'); dotIdx > 0 {
			short = key[:dotIdx]
		}
		// Use the max if multiple FQDNs share the same short hostname.
		if val > shortIndex[short] {
			shortIndex[short] = val
		}
	}

	result := make([]NiosServerMetric, len(metrics))
	for i, m := range metrics {
		result[i] = m

		// Strategy (a): exact match on full hostname.
		if peak, ok := qpsData[m.MemberName]; ok {
			result[i].QPS = int(math.Round(peak))
			continue
		}
		if peak, ok := qpsData[m.MemberID]; ok {
			result[i].QPS = int(math.Round(peak))
			continue
		}

		// Strategy (b): match by short hostname prefix.
		memberShort := m.MemberName
		if dotIdx := strings.IndexByte(m.MemberName, '.'); dotIdx > 0 {
			memberShort = m.MemberName[:dotIdx]
		}
		if peak, ok := shortIndex[memberShort]; ok {
			result[i].QPS = int(math.Round(peak))
		}
	}
	return result
}

// propPair holds a property name/value pair for the reusable buffer in
// parseObjectStreamFiltered. Avoids map allocation for filtered-out objects.
type propPair struct {
	name, value string
}

// streamOnedbXMLFiltered opens a backup file at path and streams XML objects,
// calling onObject only for objects whose __type is in typeFilter.
// If typeFilter is nil, all objects are passed through (no filtering).
//
// Auto-detects raw XML vs gzip+tar format by peeking at the first 2 bytes
// for gzip magic (0x1f 0x8b). Uses buffered I/O (256KB) for performance.
func streamOnedbXMLFiltered(path string, typeFilter map[string]struct{}, onObject func(props map[string]string)) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, 256<<10) // 256KB read buffer

	// Peek at first 2 bytes to detect gzip magic (0x1f 0x8b).
	magic, err := br.Peek(2)
	if err != nil {
		return fmt.Errorf("peek %s: %w", path, err)
	}

	if magic[0] == 0x1f && magic[1] == 0x8b {
		// Gzip+tar archive — decompress and find onedb.xml.
		gz, err := gzip.NewReader(br)
		if err != nil {
			return fmt.Errorf("gzip reader: %w", err)
		}
		defer gz.Close()

		tr := tar.NewReader(gz)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("tar: %w", err)
			}
			if filepath.Base(hdr.Name) != "onedb.xml" {
				continue
			}
			return parseObjectStreamXML(tr, typeFilter, onObject)
		}
		return fmt.Errorf("onedb.xml not found in archive")
	}

	// Raw XML file — use the fast byte-level parser (10-50x faster than encoding/xml
	// for NIOS onedb.xml format where each line is one OBJECT with inline PROPERTYs).
	return parseObjectStreamFast(br, typeFilter, onObject)
}

// parseObjectStreamXML is the encoding/xml based parser. Used for gzip+tar archives
// where the XML may be multi-line (e.g. test fixtures). Handles arbitrary XML formatting.
func parseObjectStreamXML(r io.Reader, typeFilter map[string]struct{}, onObject func(props map[string]string)) error {
	decoder := xml.NewDecoder(r)
	propBuf := make([]propPair, 0, 32)
	inObject := false
	skip := false

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("XML parse error: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "OBJECT":
				inObject = true
				skip = false
				propBuf = propBuf[:0]
			case "PROPERTY":
				if !inObject || skip {
					continue
				}
				var name, value string
				for _, attr := range t.Attr {
					switch attr.Name.Local {
					case "NAME":
						name = attr.Value
					case "VALUE":
						value = attr.Value
					}
				}
				if name == "__type" && typeFilter != nil {
					if _, ok := typeFilter[value]; !ok {
						skip = true
						continue
					}
				}
				if name != "" {
					propBuf = append(propBuf, propPair{name, value})
				}
			}

		case xml.EndElement:
			if t.Name.Local == "OBJECT" && inObject {
				inObject = false
				if !skip && len(propBuf) > 0 {
					props := make(map[string]string, len(propBuf))
					for _, p := range propBuf {
						props[p.name] = p.value
					}
					onObject(props)
				}
			}
		}
	}
	return nil
}

var (
	nameTag    = []byte(`NAME="`)
	valTag     = []byte(`VALUE="`)
	objectOpen = []byte("<OBJECT>")
	objectEnd  = []byte("</OBJECT>")
)

// parseObjectStreamFast is a high-performance parser for NIOS onedb.xml files.
// Instead of using encoding/xml (which is slow for multi-million object files),
// it scans for OBJECT/PROPERTY byte patterns directly.
//
// Handles both single-line format (real NIOS backups: all PROPERTYs on one line)
// and multi-line format (test fixtures: one PROPERTY per line).
//
// ~10-50x faster than encoding/xml for typical NIOS backups (2GB, 2.5M objects).
func parseObjectStreamFast(r io.Reader, typeFilter map[string]struct{}, onObject func(props map[string]string)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4<<20), 4<<20) // 4MB max line

	propBuf := make([]propPair, 0, 32)
	inObject := false
	skip := false

	for sc.Scan() {
		line := sc.Bytes()

		// Check for OBJECT start.
		if bytes.Contains(line, objectOpen) {
			inObject = true
			skip = false
			propBuf = propBuf[:0]
		}

		// Extract PROPERTY elements from this line.
		if inObject && !skip {
			extractProperties(line, &propBuf, typeFilter, &skip)
		}

		// Check for OBJECT end.
		if bytes.Contains(line, objectEnd) && inObject {
			inObject = false
			if !skip && len(propBuf) > 0 {
				props := make(map[string]string, len(propBuf))
				for _, p := range propBuf {
					props[p.name] = p.value
				}
				onObject(props)
			}
		}
	}
	return sc.Err()
}

// extractProperties scans a line for PROPERTY NAME="..." VALUE="..." patterns
// and appends them to propBuf. Sets *skip=true if __type is not in typeFilter.
func extractProperties(line []byte, propBuf *[]propPair, typeFilter map[string]struct{}, skip *bool) {
	pos := 0
	for {
		idx := bytes.Index(line[pos:], nameTag)
		if idx < 0 {
			break
		}
		nameStart := pos + idx + len(nameTag)

		nameEnd := bytes.IndexByte(line[nameStart:], '"')
		if nameEnd < 0 {
			break
		}
		name := string(line[nameStart : nameStart+nameEnd])
		pos = nameStart + nameEnd + 1

		valIdx := bytes.Index(line[pos:], valTag)
		if valIdx < 0 {
			break
		}
		valStart := pos + valIdx + len(valTag)

		valEnd := bytes.IndexByte(line[valStart:], '"')
		if valEnd < 0 {
			break
		}
		value := string(line[valStart : valStart+valEnd])
		pos = valStart + valEnd + 1

		// Decode XML entities if present.
		if strings.ContainsRune(value, '&') {
			value = decodeXMLEntities(value)
		}

		if name == "__type" && typeFilter != nil {
			if _, ok := typeFilter[value]; !ok {
				*skip = true
				return
			}
		}

		if name != "" {
			*propBuf = append(*propBuf, propPair{name, value})
		}
	}
}

// decodeXMLEntities replaces the 5 standard XML entities with their characters.
func decodeXMLEntities(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&apos;", "'")
	return s
}
