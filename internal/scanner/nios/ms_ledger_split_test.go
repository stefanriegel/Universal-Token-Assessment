// Package nios — per-family and per-service structural proofs for the DDI
// attribution split added in ms_ledger.go.
package nios

import "testing"

// TestMSLedgerSplit_Disjoint proves the per-service DDI split is exhaustive
// and disjoint: for every DDIFamilies member, the delta of
// ddiObjectsDNS+ddiObjectsDHCP equals the delta of the combined ddiObjects
// counter, for both Attributable and Retained. Structurally mirrors
// TestMSLedger_E7CoverageInvariant (ms_ledger_coverage_test.go).
func TestMSLedgerSplit_Disjoint(t *testing.T) {
	incremented := 0
	for family := range DDIFamilies {
		t.Run(family, func(t *testing.T) {
			result := newCountResult()
			result.msLedger = newMSLedgerState() // set explicitly — Scan does this, not newCountResult (counter.go:229-231)

			combinedBefore := result.msLedger.ddiObjects.Attributable + result.msLedger.ddiObjects.Retained
			attributableBefore := result.msLedger.ddiObjectsDNS.Attributable + result.msLedger.ddiObjectsDHCP.Attributable
			retainedBefore := result.msLedger.ddiObjectsDNS.Retained + result.msLedger.ddiObjectsDHCP.Retained

			// A generic, deliberately empty props map and a nil resolver — see
			// TestMSLedger_E7CoverageInvariant's doc comment for why this is
			// safe and sufficient: every DDIFamilies member's gridDDI increment
			// (and therefore its ledger credit) is unconditional on family
			// membership alone.
			result.processObject(family, map[string]string{}, "", nil, "", nil)

			combinedDelta := (result.msLedger.ddiObjects.Attributable + result.msLedger.ddiObjects.Retained) - combinedBefore
			attributableDelta := (result.msLedger.ddiObjectsDNS.Attributable + result.msLedger.ddiObjectsDHCP.Attributable) - attributableBefore
			retainedDelta := (result.msLedger.ddiObjectsDNS.Retained + result.msLedger.ddiObjectsDHCP.Retained) - retainedBefore

			if splitDelta := attributableDelta + retainedDelta; splitDelta != combinedDelta {
				t.Errorf("family %q: combined ddiObjects delta = %d, ddiObjectsDNS+ddiObjectsDHCP delta = %d — this family's per-service split disagrees with the combined counter",
					family, combinedDelta, splitDelta)
			}
			if combinedDelta > 0 {
				incremented++
			}
		})
	}

	// Anti-vacuity guard, identical in intent to TestMSLedger_E7CoverageInvariant's:
	// if a future family's increment becomes conditional on a non-empty prop,
	// incremented drops below 28 and this fails loudly rather than letting the
	// per-family assertions above silently degrade into 0==0 decoration.
	const wantMinIncremented = 28
	if incremented < wantMinIncremented {
		t.Errorf("only %d/%d DDIFamilies members incremented the combined DDI counter with empty props, want at least %d — the per-family assertions above may be passing vacuously (0==0)",
			incremented, len(DDIFamilies), wantMinIncremented)
	}
}

// TestMSLedgerSplit_DNSNeverCreditsActiveIPs is the structural guard behind
// D-01's promise that enabling DNS never changes an Active IP count: every
// family classifyDNSObject ever handles (the 15 msDNSRecordFamilies plus
// NiosFamilyDNSZone, NiosFamilyHostObject, and NiosFamilyHostAlias) must
// leave activeIPs untouched.
func TestMSLedgerSplit_DNSNeverCreditsActiveIPs(t *testing.T) {
	families := make([]string, 0, len(msDNSRecordFamilies)+3)
	for family := range msDNSRecordFamilies {
		families = append(families, family)
	}
	families = append(families, NiosFamilyDNSZone, NiosFamilyHostObject, NiosFamilyHostAlias)

	for _, family := range families {
		t.Run(family, func(t *testing.T) {
			l := newMSLedgerState()
			l.classifyDNSObject(family, map[string]string{})

			if l.activeIPs.Attributable != 0 {
				t.Errorf("family %q: activeIPs.Attributable = %d, want 0 — classifyDNSObject must never touch Active IPs", family, l.activeIPs.Attributable)
			}
			if l.activeIPs.Retained != 0 {
				t.Errorf("family %q: activeIPs.Retained = %d, want 0 — classifyDNSObject must never touch Active IPs", family, l.activeIPs.Retained)
			}
		})
	}
}
