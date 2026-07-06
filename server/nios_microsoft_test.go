package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestScanResultsResponseMicrosoftServersOmitempty(t *testing.T) {
	// nil → field omitted.
	b, _ := json.Marshal(ScanResultsResponse{ScanID: "x"})
	if strings.Contains(string(b), "niosMicrosoftServers") {
		t.Errorf("expected niosMicrosoftServers omitted when nil; got %s", b)
	}

	// set → field present with server fields.
	b, _ = json.Marshal(ScanResultsResponse{
		ScanID: "x",
		NiosMicrosoftServers: &NiosMicrosoftServers{
			Servers:      []NiosMicrosoftServer{{FQDN: "dc01.contoso.local", DNSManaged: true}},
			ManagedZones: 3,
		},
	})
	if !strings.Contains(string(b), `"niosMicrosoftServers"`) ||
		!strings.Contains(string(b), `"dc01.contoso.local"`) ||
		!strings.Contains(string(b), `"managedZones":3`) {
		t.Errorf("expected populated niosMicrosoftServers; got %s", b)
	}
}
