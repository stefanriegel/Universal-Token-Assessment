// Package nios — Microsoft-managed DNS/DHCP server detection.
// microsoft.go collects the ms_server / ms_server_*_properties / zone_ms_primary_server
// objects during Pass 1 and joins them by ms_oid into an informational summary.
// This is a visibility breakout only — it does NOT affect token or sizing counts.
package nios

import (
	"sort"
	"strconv"
)

// NiosMicrosoftServer is one Microsoft (Windows) DNS/DHCP server managed by the NIOS Grid.
type NiosMicrosoftServer struct {
	FQDN        string `json:"fqdn"`        // ms_server.resolved_name
	Address     string `json:"address"`     // ms_server.address
	OS          string `json:"os"`          // ms_server.version_ex
	ADDomain    string `json:"adDomain"`    // ms_server.ad_domain
	DNSManaged  bool   `json:"dnsManaged"`  // ms_server_dns_properties.managed
	DHCPManaged bool   `json:"dhcpManaged"` // ms_server_dhcp_properties.managed
	DHCPHosts   int    `json:"dhcpHosts"`   // ms_server_dhcp_properties.total_hosts
	ReadOnly    bool   `json:"readOnly"`    // ms_server.read_only
}

// NiosMicrosoftServers is the grid-wide Microsoft-managed summary from a backup scan.
type NiosMicrosoftServers struct {
	Servers      []NiosMicrosoftServer `json:"servers"`
	ManagedZones int                   `json:"managedZones"` // count of zone_ms_primary_server
}

type msServerRaw struct {
	fqdn, address, os, adDomain string
	readOnly                    bool
}

type msDHCPRaw struct {
	managed bool
	hosts   int
}

// msCollector accumulates Microsoft-server objects during Pass 1 and joins
// ms_server ↔ dns/dhcp properties by ms_oid.
type msCollector struct {
	servers      map[string]msServerRaw // ms_oid → server
	dnsManaged   map[string]bool        // parent(ms_oid) → dns managed
	dhcp         map[string]msDHCPRaw   // parent(ms_oid) → dhcp props
	managedZones int
}

func newMSCollector() *msCollector {
	return &msCollector{
		servers:    make(map[string]msServerRaw),
		dnsManaged: make(map[string]bool),
		dhcp:       make(map[string]msDHCPRaw),
	}
}

// record consumes one of the four MS-related onedb.xml object types.
func (c *msCollector) record(props map[string]string) {
	switch props["__type"] {
	case ".com.infoblox.one.ms_server":
		oid := props["ms_oid"]
		if oid == "" {
			return
		}
		c.servers[oid] = msServerRaw{
			fqdn:     props["resolved_name"],
			address:  props["address"],
			os:       props["version_ex"],
			adDomain: props["ad_domain"],
			readOnly: props["read_only"] == "true",
		}
	case ".com.infoblox.dns.ms_server_dns_properties":
		if parent := props["parent"]; parent != "" {
			c.dnsManaged[parent] = props["managed"] == "true"
		}
	case ".com.infoblox.dns.ms_server_dhcp_properties":
		if parent := props["parent"]; parent != "" {
			hosts, _ := strconv.Atoi(props["total_hosts"])
			c.dhcp[parent] = msDHCPRaw{managed: props["managed"] == "true", hosts: hosts}
		}
	case ".com.infoblox.dns.zone_ms_primary_server":
		c.managedZones++
	}
}

// build joins collected data into NiosMicrosoftServers, sorted by FQDN.
// Returns nil when no ms_server objects were found (feature absent from backup).
func (c *msCollector) build() *NiosMicrosoftServers {
	if len(c.servers) == 0 {
		return nil
	}
	out := make([]NiosMicrosoftServer, 0, len(c.servers))
	for oid, s := range c.servers {
		dhcp := c.dhcp[oid]
		out = append(out, NiosMicrosoftServer{
			FQDN:        s.fqdn,
			Address:     s.address,
			OS:          s.os,
			ADDomain:    s.adDomain,
			DNSManaged:  c.dnsManaged[oid],
			DHCPManaged: dhcp.managed,
			DHCPHosts:   dhcp.hosts,
			ReadOnly:    s.readOnly,
		})
	}
	// Deterministic order: FQDN primary, Address as tiebreak (map iteration order
	// is random, so a total comparator is needed for byte-stable exports).
	sort.Slice(out, func(i, j int) bool {
		if out[i].FQDN != out[j].FQDN {
			return out[i].FQDN < out[j].FQDN
		}
		return out[i].Address < out[j].Address
	})
	return &NiosMicrosoftServers{Servers: out, ManagedZones: c.managedZones}
}
