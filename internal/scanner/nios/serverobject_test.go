package nios_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/scanner"
	niosscanner "github.com/stefanriegel/Universal-Token-Assessment/internal/scanner/nios"
)

// metricsByHost scans the given backup and returns metrics keyed by MemberID.
func metricsByHost(t *testing.T, backupPath, selected string) map[string]niosscanner.NiosServerMetric {
	t.Helper()
	s := niosscanner.New()
	req := scanner.ScanRequest{
		Provider: "nios",
		Credentials: map[string]string{
			"backup_path":      backupPath,
			"selected_members": selected,
		},
	}
	if _, err := s.Scan(context.Background(), req, func(scanner.Event) {}); err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	data := any(s).(NiosResultScanner).GetNiosServerMetricsJSON()
	var metrics []niosscanner.NiosServerMetric
	if err := json.Unmarshal(data, &metrics); err != nil {
		t.Fatal(err)
	}
	byHost := make(map[string]niosscanner.NiosServerMetric, len(metrics))
	for _, m := range metrics {
		byHost[m.MemberID] = m
	}
	return byHost
}

// TestServerObjectCount_DDIPlusDHCP verifies ServerObjectCount adds active leases
// and fixed addresses on top of the DDI ObjectCount. On the minimal fixture the GM
// has ObjectCount 8 (DDI), 3 active leases and 1 fixed address (ActiveIPCount 4),
// so ServerObjectCount = 8 + 3 + 1 = 12.
func TestServerObjectCount_DDIPlusDHCP(t *testing.T) {
	byHost := metricsByHost(t, openFixture(t), "gm.test.local,dns1.test.local,dhcp1.test.local")
	gm := byHost["gm.test.local"]
	if gm.ObjectCount != 8 {
		t.Fatalf("precondition: GM ObjectCount = %d, want 8", gm.ObjectCount)
	}
	if gm.ServerObjectCount != 12 {
		t.Errorf("GM ServerObjectCount = %d, want 12 (8 DDI + 3 active leases + 1 fixed)", gm.ServerObjectCount)
	}
}

// TestServerObjectCount_FailoverUndeduped verifies each failover peer counts its own
// leases. The failover fixture has 3 distinct active lease IPs (grid-deduped) but the
// per-server counts must sum to 4: gm={10.0.0.1,10.0.0.5}=2, peer={10.0.0.5,10.0.0.2}=2.
func TestServerObjectCount_FailoverUndeduped(t *testing.T) {
	byHost := metricsByHost(t, writeFailoverBackup(t), "gm.test.local,peer.test.local")
	if got := byHost["gm.test.local"].ServerObjectCount; got != 2 {
		t.Errorf("gm ServerObjectCount = %d, want 2", got)
	}
	if got := byHost["peer.test.local"].ServerObjectCount; got != 2 {
		t.Errorf("peer ServerObjectCount = %d, want 2 (own replica of 10.0.0.5, undeduped)", got)
	}
}
