package nios_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/scanner"
	niosscanner "github.com/stefanriegel/Universal-Token-Assessment/internal/scanner/nios"
)

// writeBackup packs an onedb.xml body into a gzip+tar backup and returns its path.
func writeBackup(t *testing.T, body string) string {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	b := []byte(body)
	if err := tw.WriteHeader(&tar.Header{Name: "onedb.xml", Mode: 0600, Size: int64(len(b))}); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write(b); err != nil {
		t.Fatalf("tar write: %v", err)
	}
	tw.Close()
	gw.Close()
	path := filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	return path
}

func scanBreakdown(t *testing.T, body string) *niosscanner.NiosActiveIPBreakdown {
	t.Helper()
	s := niosscanner.New()
	if _, err := s.Scan(context.Background(), scanner.ScanRequest{
		Provider:    "nios",
		Credentials: map[string]string{"backup_path": writeBackup(t, body)},
	}, func(scanner.Event) {}); err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	bd := s.ActiveIPBreakdown()
	if bd == nil {
		t.Fatal("nil breakdown")
	}
	return bd
}

const gmHeader = `<?xml version="1.0" encoding="UTF-8"?>
<DATABASE NAME="onedb" VERSION="9.0.6-test">
<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.one.virtual_node"/>
<PROPERTY NAME="virtual_oid" VALUE="101"/>
<PROPERTY NAME="host_name" VALUE="gm.test.local"/>
<PROPERTY NAME="is_grid_master" VALUE="true"/>
</OBJECT>`

// TestNIOS_NetworkReservations asserts Network Reservations = 2 × network count and
// that they flow into the UDDI Active-IP total (flat ×2, including /31 and /32).
func TestNIOS_NetworkReservations(t *testing.T) {
	body := gmHeader + `
<OBJECT><PROPERTY NAME="__type" VALUE=".com.infoblox.dns.network"/><PROPERTY NAME="address" VALUE="10.0.0.0"/><PROPERTY NAME="cidr" VALUE="24"/><PROPERTY NAME="network_view" VALUE="0"/></OBJECT>
<OBJECT><PROPERTY NAME="__type" VALUE=".com.infoblox.dns.network"/><PROPERTY NAME="address" VALUE="10.0.1.0"/><PROPERTY NAME="cidr" VALUE="31"/><PROPERTY NAME="network_view" VALUE="0"/></OBJECT>
<OBJECT><PROPERTY NAME="__type" VALUE=".com.infoblox.dns.network"/><PROPERTY NAME="address" VALUE="10.0.2.1"/><PROPERTY NAME="cidr" VALUE="32"/><PROPERTY NAME="network_view" VALUE="0"/></OBJECT>
</DATABASE>`
	bd := scanBreakdown(t, body)
	if bd.NetworkCount != 3 {
		t.Fatalf("NetworkCount = %d, want 3", bd.NetworkCount)
	}
	if bd.NetworkReservations != 6 {
		t.Errorf("NetworkReservations = %d, want 6 (2×3, flat including /31 and /32)", bd.NetworkReservations)
	}
	// No leases → no corporate +1 mirror. UDDI = 0 fixed + 0 leases + 6 reservations.
	if bd.UDDIActiveIPs != 6 {
		t.Errorf("UDDIActiveIPs = %d, want 6 (0 fixed + 0 leases + 6 reservations)", bd.UDDIActiveIPs)
	}
}

// TestNIOS_HostAddressConfigureForDHCPExcluded asserts host addresses with
// configure_for_dhcp="true" (DHCP fixed reservations, already counted as fixed) are
// excluded from the true host-address set. Two cfd=false entries are kept.
func TestNIOS_HostAddressConfigureForDHCPExcluded(t *testing.T) {
	body := gmHeader + `
<OBJECT><PROPERTY NAME="__type" VALUE=".com.infoblox.dns.host_address"/><PROPERTY NAME="address" VALUE="10.0.0.10"/><PROPERTY NAME="configure_for_dhcp" VALUE="false"/></OBJECT>
<OBJECT><PROPERTY NAME="__type" VALUE=".com.infoblox.dns.host_address"/><PROPERTY NAME="address" VALUE="10.0.0.11"/><PROPERTY NAME="configure_for_dhcp" VALUE="false"/></OBJECT>
<OBJECT><PROPERTY NAME="__type" VALUE=".com.infoblox.dns.host_address"/><PROPERTY NAME="address" VALUE="10.0.0.12"/><PROPERTY NAME="configure_for_dhcp" VALUE="true"/></OBJECT>
</DATABASE>`
	bd := scanBreakdown(t, body)
	// True distinct host addresses = 2 (10.0.0.12 excluded as a DHCP reservation).
	if bd.HostAddressRaw != 2 {
		t.Errorf("HostAddressRaw = %d, want 2 (configure_for_dhcp=true entry excluded)", bd.HostAddressRaw)
	}
	// Corporate-parity value adds the DB-analyzer's +1 because an entry was excluded.
	if bd.HostAddress != 3 {
		t.Errorf("HostAddress = %d, want 3 (raw 2 + corporate +1 mirror)", bd.HostAddress)
	}
}

// TestNIOS_CorporateOffByOneMirror asserts the scanner mirrors the corporate DB-analyzer's
// +1 active-lease off-by-one: raw distinct active leases plus 1 whenever any exist.
func TestNIOS_CorporateOffByOneMirror(t *testing.T) {
	body := gmHeader + `
<OBJECT><PROPERTY NAME="__type" VALUE=".com.infoblox.dns.lease"/><PROPERTY NAME="vnode_id" VALUE="101"/><PROPERTY NAME="binding_state" VALUE="active"/><PROPERTY NAME="ip_address" VALUE="10.0.0.1"/></OBJECT>
<OBJECT><PROPERTY NAME="__type" VALUE=".com.infoblox.dns.lease"/><PROPERTY NAME="vnode_id" VALUE="101"/><PROPERTY NAME="binding_state" VALUE="active"/><PROPERTY NAME="ip_address" VALUE="10.0.0.2"/></OBJECT>
</DATABASE>`
	bd := scanBreakdown(t, body)
	if bd.ActiveLeasesRaw != 2 {
		t.Errorf("ActiveLeasesRaw = %d, want 2 (true distinct active leases)", bd.ActiveLeasesRaw)
	}
	if bd.ActiveLeases != 3 {
		t.Errorf("ActiveLeases = %d, want 3 (raw 2 + corporate +1 mirror)", bd.ActiveLeases)
	}
}

// TestNIOS_FixedAndLeaseCountedIndependently asserts that an IP which is both a fixed
// address and an active lease is counted in BOTH categories (no cross-dedup), matching
// the corporate DB-analyzer's independent per-type rows.
func TestNIOS_FixedAndLeaseCountedIndependently(t *testing.T) {
	body := gmHeader + `
<OBJECT><PROPERTY NAME="__type" VALUE=".com.infoblox.dns.fixed_address"/><PROPERTY NAME="ip_address" VALUE="10.0.0.50"/></OBJECT>
<OBJECT><PROPERTY NAME="__type" VALUE=".com.infoblox.dns.lease"/><PROPERTY NAME="vnode_id" VALUE="101"/><PROPERTY NAME="binding_state" VALUE="active"/><PROPERTY NAME="ip_address" VALUE="10.0.0.50"/></OBJECT>
</DATABASE>`
	bd := scanBreakdown(t, body)
	if bd.FixedAddress != 1 {
		t.Errorf("FixedAddress = %d, want 1", bd.FixedAddress)
	}
	// No cross-dedup: the shared IP is in BOTH the fixed set and the active-lease set.
	if bd.ActiveLeasesRaw != 1 {
		t.Errorf("ActiveLeasesRaw = %d, want 1 (counted independently of the fixed address)", bd.ActiveLeasesRaw)
	}
	// UDDI = fixed 1 + active-lease (1 raw + 1 corporate mirror = 2) + 0 reservations = 3.
	if bd.UDDIActiveIPs != 3 {
		t.Errorf("UDDIActiveIPs = %d, want 3 (fixed 1 + lease 2 [1 raw + mirror], no cross-dedup)", bd.UDDIActiveIPs)
	}
}
