package nios

import (
	"os"
	"sort"
	"strings"
	"testing"
)

// msSchemaProbeTypes are the four Microsoft-relationship object types this
// probe inspects for sweep A — the same set already allowlisted in pass1Types.
var msSchemaProbeTypes = map[string]struct{}{
	".com.infoblox.one.ms_server":                 {},
	".com.infoblox.dns.ms_server_dns_properties":  {},
	".com.infoblox.dns.ms_server_dhcp_properties": {},
	".com.infoblox.dns.zone_ms_primary_server":    {},
}

// probeStat tracks how often a PROPERTY name occurs on an object type and,
// only when isSafeProbeValue allows it, one sample VALUE to help identify
// the property's role without ever risking a hostname/FQDN/IP leak.
type probeStat struct {
	count  int
	sample string
}

// isSafeProbeValue reports whether v is safe to log verbatim from a real
// customer backup: either fully numeric (an internal OID) or a NIOS internal
// object reference. Anything else (hostnames, FQDNs, domains, IP addresses)
// must never be logged, since probe output may be pasted into 05-SCHEMA.md.
func isSafeProbeValue(v string) bool {
	if v == "" {
		return false
	}
	if strings.HasPrefix(v, ".com.infoblox") {
		return true
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func recordProbeStat(stats map[string]map[string]*probeStat, typ, name, value string) {
	byName := stats[typ]
	if byName == nil {
		byName = make(map[string]*probeStat)
		stats[typ] = byName
	}
	stat := byName[name]
	if stat == nil {
		stat = &probeStat{}
		byName[name] = stat
	}
	stat.count++
	if stat.sample == "" && isSafeProbeValue(value) {
		stat.sample = value
	}
}

// logProbeStats logs one sweep's results deterministically: sorted by object
// type then property name, so repeated runs against the same backup produce
// identical output.
func logProbeStats(t *testing.T, stats map[string]map[string]*probeStat) {
	types := make([]string, 0, len(stats))
	for typ := range stats {
		types = append(types, typ)
	}
	sort.Strings(types)
	for _, typ := range types {
		names := make([]string, 0, len(stats[typ]))
		for name := range stats[typ] {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			s := stats[typ][name]
			if s.sample != "" {
				t.Logf("  %s.%s: count=%d sample=%q", typ, name, s.count, s.sample)
			} else {
				t.Logf("  %s.%s: count=%d", typ, name, s.count)
			}
		}
	}
}

// TestMSSchemaProbe inspects a real NIOS Grid backup's onedb.xml to answer
// RESEARCH.md Assumptions A1 and A2: the exact PROPERTY keys carried by
// .com.infoblox.dns.zone_ms_primary_server, and whether any object type
// expresses a DHCP network/range to Microsoft-server relationship.
//
// Developer-invoked only: the backup path comes solely from the
// MS_SCHEMA_PROBE_BACKUP environment variable, never a committed file, so
// go test ./... stays green with no customer data present.
func TestMSSchemaProbe(t *testing.T) {
	backup := os.Getenv("MS_SCHEMA_PROBE_BACKUP")
	if backup == "" {
		t.Skip("MS_SCHEMA_PROBE_BACKUP not set — skipping onedb.xml Microsoft schema probe (developer-invoked only)")
	}

	// Sweep A: property names + counts on the four Microsoft object types,
	// plus every ms_oid value seen on ms_server (drives sweep B's join).
	sweepA := make(map[string]map[string]*probeStat)
	msOIDs := make(map[string]struct{})
	err := streamOnedbXMLFiltered(backup, msSchemaProbeTypes, func(props map[string]string) {
		typ := props["__type"]
		for name, value := range props {
			if name == "__type" {
				continue
			}
			recordProbeStat(sweepA, typ, name, value)
		}
		if typ == ".com.infoblox.one.ms_server" {
			if oid := props["ms_oid"]; oid != "" {
				msOIDs[oid] = struct{}{}
			}
		}
	})
	if err != nil {
		t.Fatalf("sweep A: %v", err)
	}
	t.Logf("=== Sweep A: PROPERTY names + counts on the four Microsoft object types ===")
	logProbeStats(t, sweepA)
	t.Logf("=== Sweep A: %d distinct ms_server ms_oid values observed ===", len(msOIDs))

	// Sweep B: every (__type, property name) pair anywhere in the backup whose
	// VALUE exactly equals one of sweep A's ms_oid values. This is the
	// mechanical answer to A2 — any object type referencing a Microsoft
	// server OID by value is a candidate relationship object, found without
	// knowing its name in advance. A nil filter visits every object type
	// (streamOnedbXMLFiltered treats nil, not an empty map, as "match
	// everything" — see the typeFilter != nil guards in scanner.go).
	sweepB := make(map[string]map[string]*probeStat)
	err = streamOnedbXMLFiltered(backup, nil, func(props map[string]string) {
		typ := props["__type"]
		for name, value := range props {
			if name == "__type" {
				continue
			}
			if _, ok := msOIDs[value]; ok {
				recordProbeStat(sweepB, typ, name, value)
			}
		}
	})
	if err != nil {
		t.Fatalf("sweep B: %v", err)
	}
	t.Logf("=== Sweep B: object types + properties referencing an ms_server ms_oid by value ===")
	logProbeStats(t, sweepB)
}
