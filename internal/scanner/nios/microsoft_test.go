package nios

import "testing"

func TestMSCollectorBuild(t *testing.T) {
	c := newMSCollector()
	c.record(map[string]string{
		"__type": ".com.infoblox.one.ms_server", "ms_oid": "8",
		"resolved_name": "dc01.contoso.local", "address": "10.0.0.11",
		"version_ex": "Windows Server 2019", "ad_domain": "contoso.local", "read_only": "true",
	})
	c.record(map[string]string{
		"__type": ".com.infoblox.one.ms_server", "ms_oid": "10",
		"resolved_name": "dc02.contoso.local", "address": "10.0.0.12",
		"version_ex": "Windows Server 2012 R2", "ad_domain": "contoso.local", "read_only": "true",
	})
	c.record(map[string]string{"__type": ".com.infoblox.dns.ms_server_dns_properties", "parent": "8", "managed": "true"})
	c.record(map[string]string{"__type": ".com.infoblox.dns.ms_server_dns_properties", "parent": "10", "managed": "false"})
	c.record(map[string]string{"__type": ".com.infoblox.dns.ms_server_dhcp_properties", "parent": "8", "managed": "false", "total_hosts": "0"})
	c.record(map[string]string{"__type": ".com.infoblox.dns.zone_ms_primary_server"})
	c.record(map[string]string{"__type": ".com.infoblox.dns.zone_ms_primary_server"})

	got := c.build()
	if got == nil {
		t.Fatal("build() = nil, want servers")
	}
	if len(got.Servers) != 2 {
		t.Fatalf("Servers = %d, want 2", len(got.Servers))
	}
	if got.ManagedZones != 2 {
		t.Errorf("ManagedZones = %d, want 2", got.ManagedZones)
	}
	// Sorted by FQDN → dc01 first, DNS-managed.
	if got.Servers[0].FQDN != "dc01.contoso.local" || !got.Servers[0].DNSManaged {
		t.Errorf("server[0] = %+v, want dc01 DNS-managed", got.Servers[0])
	}
	if got.Servers[1].DNSManaged {
		t.Error("server[1].DNSManaged = true, want false")
	}
}

func TestMSCollectorBuildEmpty(t *testing.T) {
	if got := newMSCollector().build(); got != nil {
		t.Errorf("build() with no servers = %+v, want nil", got)
	}
}

func TestPass1RecordsMicrosoftServers(t *testing.T) {
	st := newPass1State()
	st.record(map[string]string{
		"__type": ".com.infoblox.one.ms_server", "ms_oid": "8",
		"resolved_name": "dc01.contoso.local", "address": "10.0.0.11",
	})
	st.record(map[string]string{"__type": ".com.infoblox.dns.ms_server_dns_properties", "parent": "8", "managed": "true"})

	got := st.ms.build()
	if got == nil || len(got.Servers) != 1 || !got.Servers[0].DNSManaged {
		t.Fatalf("st.ms.build() = %+v, want 1 DNS-managed server", got)
	}
}

func TestScannerMicrosoftServersJSON(t *testing.T) {
	s := New()
	if s.GetNiosMicrosoftServersJSON() != nil {
		t.Error("expected nil before scan / with no MS servers")
	}
	s.microsoftServers = &NiosMicrosoftServers{
		Servers:      []NiosMicrosoftServer{{FQDN: "dc01.contoso.local", DNSManaged: true}},
		ManagedZones: 3,
	}
	if s.GetNiosMicrosoftServersJSON() == nil {
		t.Error("expected JSON when MS servers present")
	}
}
