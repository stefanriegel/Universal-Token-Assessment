package nios

import "testing"

// TestBuildMetrics_UnresolvedDDIExcludedFromGMObjectCount verifies that unresolved
// DDI objects (not attributable to any specific member) are no longer folded into
// the Grid Master's own ObjectCount/ServerObjectCount, since that inflated the GM's
// per-server sizing count even though grid-total management-token math (which sums
// unresolved DDI separately as its own FindingRow) was unaffected. Also verifies
// RunsDnsDhcp is populated independently of structural GM/GMC role.
func TestBuildMetrics_UnresolvedDDIExcludedFromGMObjectCount(t *testing.T) {
	vnodeMap := map[string]string{"1": "gm.example.com", "2": "member1.example.com"}
	memberProps := map[string]map[string]string{
		"gm.example.com": {
			"is_master":   "true",
			"enable_dns":  "true",
			"enable_dhcp": "true",
		},
		"member1.example.com": {
			"enable_dns":  "true",
			"enable_dhcp": "true",
		},
	}
	result := countResult{
		memberAccs: map[string]*memberAcc{
			"gm.example.com": {
				ddiCount:         10,
				memberIPSet:      map[string]struct{}{},
				hostAddressIPSet: map[string]struct{}{},
				leaseIPSet:       map[string]struct{}{},
				fixedIPSet:       map[string]struct{}{},
			},
			"member1.example.com": {
				ddiCount:         5,
				memberIPSet:      map[string]struct{}{},
				hostAddressIPSet: map[string]struct{}{},
				leaseIPSet:       map[string]struct{}{},
				fixedIPSet:       map[string]struct{}{},
			},
		},
		unresolvedDDI: map[string]int{NiosFamilyNetwork: 100},
	}

	metrics := buildMetrics(vnodeMap, memberProps, result, "gm.example.com", nil)

	byHost := make(map[string]*NiosServerMetric, len(metrics))
	for i := range metrics {
		byHost[metrics[i].MemberID] = &metrics[i]
	}

	gm := byHost["gm.example.com"]
	member := byHost["member1.example.com"]

	if gm.ObjectCount != 10 {
		t.Errorf("GM ObjectCount = %d, want 10 (unresolved DDI must not be folded in)", gm.ObjectCount)
	}
	if !gm.RunsDnsDhcp {
		t.Error("GM RunsDnsDhcp = false, want true (enable_dns/enable_dhcp set)")
	}
	if !member.RunsDnsDhcp {
		t.Error("member1 RunsDnsDhcp = false, want true (enable_dns/enable_dhcp set)")
	}
}
