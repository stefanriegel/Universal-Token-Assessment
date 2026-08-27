// Package nios — coverage invariant test for the Microsoft allocation ledger.
package nios

import "testing"

// TestMSLedger_E7CoverageInvariant proves the E7 coverage invariant: every
// DDIFamilies member that increments countResult.gridDDI during processObject
// credits exactly one of Attributable or Retained in the msLedger's
// DDIObjects partition — equivalently, for each family, the gridDDI delta
// must equal the ddiObjects.Attributable+Retained delta.
//
// This guards against the regression a caught blocker in this phase actually
// hit: NiosFamilyHostObject had its own dedicated `case` in processObject's
// switch (counter.go) that incremented gridDDI, but reached no msLedger
// classifier call at all. gridDDI advanced while neither ledger partition
// moved, so checkMSConservation's invariant (Baseline == Attributable +
// Retained) silently broke and D-16 suppressed the entire Microsoft ledger on
// any backup containing a host object — a ubiquitous NIOS construct — while
// every existing test passed. All three `gridDDI += delta` sites (the
// NiosFamilyNetwork case, the NiosFamilyHostObject case, and the default
// case) are correctly hooked today; this test exists to catch a FUTURE
// family added with a dedicated case and no hook.
func TestMSLedger_E7CoverageInvariant(t *testing.T) {
	incremented := 0
	for family := range DDIFamilies {
		t.Run(family, func(t *testing.T) {
			result := newCountResult()
			result.msLedger = newMSLedgerState() // set explicitly — Scan does this, not newCountResult (counter.go:229-231)

			gridDDIBefore := result.gridDDI
			ddiBefore := result.msLedger.ddiObjects.Attributable + result.msLedger.ddiObjects.Retained

			// A generic, deliberately empty props map and a nil resolver: every
			// DDIFamilies member's gridDDI increment in processObject is
			// unconditional on family membership alone — none of the three
			// `gridDDI += delta` sites gate on a prop value. Only the
			// *attribution* path (member vs. unresolved, Attributable vs.
			// Retained) depends on props/resolver, and this test does not care
			// which side of that split the object lands on, only that it lands
			// on exactly one. resolveDNSMember/resolveNetworkMember/
			// resolveIPMember all nil-check their receiver, so a nil resolver
			// is safe here.
			result.processObject(family, map[string]string{}, "", nil, "", nil)

			gridDDIDelta := result.gridDDI - gridDDIBefore
			ddiDelta := (result.msLedger.ddiObjects.Attributable + result.msLedger.ddiObjects.Retained) - ddiBefore

			if gridDDIDelta != ddiDelta {
				t.Errorf("family %q: gridDDI delta = %d, ddiObjects(Attributable+Retained) delta = %d — this family reaches a gridDDI increment with no corresponding (or a double) ledger credit",
					family, gridDDIDelta, ddiDelta)
			}
			if gridDDIDelta > 0 {
				incremented++
			}
		})
	}

	// Anti-vacuity guard: as of writing, every one of DDIFamilies' 28 members
	// increments gridDDI unconditionally the moment processObject is called
	// with that family — verified by running this test. A per-family
	// assertion of 0 == 0 above would pass trivially if this empty-props call
	// stopped exercising the code (e.g. a future family gated its increment
	// behind a non-empty prop), silently turning this test into decoration.
	// If that ever happens, incremented drops below 28 and this fails loudly.
	const wantMinIncremented = 28
	if incremented < wantMinIncremented {
		t.Errorf("only %d/%d DDIFamilies members incremented gridDDI with empty props, want at least %d — the per-family assertions above may be passing vacuously (0==0)",
			incremented, len(DDIFamilies), wantMinIncremented)
	}
}
