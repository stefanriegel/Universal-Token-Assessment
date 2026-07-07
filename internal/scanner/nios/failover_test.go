package nios_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/calculator"
	"github.com/stefanriegel/Universal-Token-Assessment/internal/scanner"
	niosscanner "github.com/stefanriegel/Universal-Token-Assessment/internal/scanner/nios"
)

// failoverXML has two members. The active lease for 10.0.0.5 is replicated onto
// BOTH members (different vnode_id, both binding_state="active") — exactly how
// NIOS DHCP failover stores leases. Distinct active IPs are {10.0.0.1, 10.0.0.2,
// 10.0.0.5} = 3. A per-member sum would report 4 (double-counting 10.0.0.5).
const failoverXML = `<?xml version="1.0" encoding="UTF-8"?>
<DATABASE NAME="onedb" VERSION="9.0.6-test">
<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.one.virtual_node"/>
<PROPERTY NAME="virtual_oid" VALUE="101"/>
<PROPERTY NAME="host_name" VALUE="gm.test.local"/>
<PROPERTY NAME="is_grid_master" VALUE="true"/>
</OBJECT>
<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.one.virtual_node"/>
<PROPERTY NAME="virtual_oid" VALUE="102"/>
<PROPERTY NAME="host_name" VALUE="peer.test.local"/>
<PROPERTY NAME="is_grid_master" VALUE="false"/>
<PROPERTY NAME="enable_dhcp" VALUE="true"/>
</OBJECT>
<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.lease"/>
<PROPERTY NAME="vnode_id" VALUE="101"/>
<PROPERTY NAME="binding_state" VALUE="active"/>
<PROPERTY NAME="ip_address" VALUE="10.0.0.1"/>
</OBJECT>
<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.lease"/>
<PROPERTY NAME="vnode_id" VALUE="101"/>
<PROPERTY NAME="binding_state" VALUE="active"/>
<PROPERTY NAME="ip_address" VALUE="10.0.0.5"/>
</OBJECT>
<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.lease"/>
<PROPERTY NAME="vnode_id" VALUE="102"/>
<PROPERTY NAME="binding_state" VALUE="active"/>
<PROPERTY NAME="ip_address" VALUE="10.0.0.5"/>
</OBJECT>
<OBJECT>
<PROPERTY NAME="__type" VALUE=".com.infoblox.dns.lease"/>
<PROPERTY NAME="vnode_id" VALUE="102"/>
<PROPERTY NAME="binding_state" VALUE="active"/>
<PROPERTY NAME="ip_address" VALUE="10.0.0.2"/>
</OBJECT>
</DATABASE>`

func writeFailoverBackup(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	body := []byte(failoverXML)
	if err := tw.WriteHeader(&tar.Header{Name: "onedb.xml", Mode: 0600, Size: int64(len(body))}); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatalf("tar write: %v", err)
	}
	tw.Close()
	gw.Close()

	path := filepath.Join(t.TempDir(), "failover.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	return path
}

// TestNIOS_FailoverActiveIPDedup is the regression test for the active-IP
// inflation: DHCP failover replicates active leases across peers, and
// the grid Active-IP total must dedup them grid-wide (matching the corporate NIOS
// DB-analyzer) rather than summing per-member sets.
func TestNIOS_FailoverActiveIPDedup(t *testing.T) {
	path := writeFailoverBackup(t)
	rows, err := niosscanner.New().Scan(context.Background(), scanner.ScanRequest{
		Provider:    "nios",
		Credentials: map[string]string{"backup_path": path},
	}, func(scanner.Event) {})
	if err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	total := 0
	for _, r := range rows {
		if r.Category == calculator.CategoryActiveIPs {
			total += r.Count
		}
	}
	// 3 distinct active-lease IPs (the failover replica of 10.0.0.5 is deduped grid-wide),
	// plus the corporate DB-analyzer's +1 active-lease off-by-one that the scanner mirrors
	// (see counter.go). No fixed addresses or networks here, so total = 3 + 1 = 4.
	// A regression to per-member summing would instead yield 4 distinct + 1 = 5.
	if total != 4 {
		t.Errorf("Active IPs = %d, want 4 (3 distinct leases + corporate +1 mirror); rows: %+v", total, rows)
	}
}
