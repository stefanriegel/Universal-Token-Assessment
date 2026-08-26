// Package nios — Microsoft allocation scenario derivation.
//
// ms_allocation.go derives the four Microsoft allocation scenarios (DNS
// off/on x DHCP off/on) from the Phase 5 attribution ledger and the
// unchanged whole-grid baseline NIOS findings, in one pass (D-13). Every
// category token count and the whole-grid baseline route through plan
// 06-02's msCategoryTokens / msAllNIOSTokens — this file performs no
// arithmetic of its own.
package nios

import "github.com/stefanriegel/Universal-Token-Assessment/internal/calculator"

// The four Microsoft allocation scenario IDs (D-13). No fifth scenario may
// ever be added; the four switch states are exhaustive.
const (
	MSScenarioNone     = "none"
	MSScenarioDNSOnly  = "dns-only"
	MSScenarioDHCPOnly = "dhcp-only"
	MSScenarioBoth     = "both"
)

// msScenarioDef fixes one scenario's ID and its two switch states.
type msScenarioDef struct {
	id          string
	dnsEnabled  bool
	dhcpEnabled bool
}

// msScenarioOrder fixes the four scenarios' emission order, mirroring the
// existing fixed-order msCategoryNames idiom (ms_ledger.go) so the scenario
// set is deterministic rather than incidental.
var msScenarioOrder = [4]msScenarioDef{
	{MSScenarioNone, false, false},
	{MSScenarioDNSOnly, true, false},
	{MSScenarioDHCPOnly, false, true},
	{MSScenarioBoth, true, true},
}

// MSAllocationScenario is one of the four derived allocation states. ID is
// the only string field and carries one of the four MSScenario* constants
// above; no further string field may be added — a free-text field is capable
// of carrying a backup-derived identifier. Categories is a three-element
// array, not a slice, so the category ordering and arity are part of the
// type.
type MSAllocationScenario struct {
	ID              string              `json:"id"`
	DNSEnabled      bool                `json:"dnsEnabled"`
	DHCPEnabled     bool                `json:"dhcpEnabled"`
	Categories      [3]MSCategoryTokens `json:"categories"`
	EffectiveTokens int                 `json:"effectiveTokens"`
	// DeltaTokens is EffectiveTokens minus the whole-grid baseline. It is a
	// cost, never a saving: native rates are less generous than NIOS rates,
	// so allocation always raises the token count (D-10). This figure must
	// never be presented, named, or commented as a saving or a reduction.
	DeltaTokens int `json:"deltaTokens"`
}

// MSAllocationScenarioSet is the single-pass output of
// DeriveMSAllocationScenarios.
type MSAllocationScenarioSet struct {
	Diagnostic     MSAllocationDiagnostic `json:"diagnostic"`
	BaselineTokens int                    `json:"baselineTokens"`
	Scenarios      []MSAllocationScenario `json:"scenarios"`
}

// DeriveMSAllocationScenarios derives all four Microsoft allocation
// scenarios from ledger and diag (Phase 5's output) plus baseline (the
// scan's own whole-grid finding rows), in one pass (D-13). Both inputs are
// read-only: no field of ledger is written, no element of baseline is
// mutated or reordered, so all four scenarios read the same unchanged
// inputs.
//
// Branching mirrors msLedgerState.build's order and reuses the existing
// three-state diagnostic without inventing a fourth state:
//
//   - Available && ledger != nil: run the input-consistency gate first. On
//     any failure, suppress by returning zero scenarios with the diagnostic
//     replaced by Available false, the existing MSAllocationUnavailableCode,
//     and the existing fixed msAllocationUnavailableMessage — reusing that
//     constant, never composing a new string, is what keeps the suppression
//     path incapable of leaking a count or an identifier. Otherwise, derive
//     all four scenarios with the ledger's per-service attribution.
//   - Code == MSAllocationAbsentCode (D-14): produce all four scenarios with
//     every moved count at 0, so every effective total equals the baseline
//     and every delta is 0. Flipping a switch truthfully shows no movement.
//   - Any other non-Available diagnostic, including a zero-value one
//     (D-15): return zero scenarios and pass the diagnostic through
//     unchanged.
//
// In every branch, including the suppressed ones, the whole-grid baseline
// total is still reported (D-15) — the baseline NIOS scan remains valid and
// usable regardless of the Microsoft ledger's fate.
func DeriveMSAllocationScenarios(ledger *MicrosoftAllocationLedger, diag MSAllocationDiagnostic, baseline []calculator.FindingRow) MSAllocationScenarioSet {
	baselineTokens := msAllNIOSTokens(baseline)
	set := MSAllocationScenarioSet{
		Diagnostic:     diag,
		BaselineTokens: baselineTokens,
	}
	gridDDI, gridIP, gridAsset := msAggregateNIOSCategoryCounts(baseline)

	switch {
	case diag.Available && ledger != nil:
		if failing := msScenarioInputsConsistent(ledger, gridDDI, gridIP, gridAsset); len(failing) > 0 {
			set.Diagnostic = MSAllocationDiagnostic{
				Available: false,
				Code:      MSAllocationUnavailableCode,
				Message:   msAllocationUnavailableMessage,
			}
			return set
		}
		set.Scenarios = msBuildScenarios(gridDDI, gridIP,
			ledger.DDIObjectsDNS.Attributable, ledger.DDIObjectsDHCP.Attributable, ledger.ActiveIPs.Attributable,
			baselineTokens)
	case diag.Code == MSAllocationAbsentCode:
		set.Scenarios = msBuildScenarios(gridDDI, gridIP, 0, 0, 0, baselineTokens)
	}

	return set
}

// msScenarioInputsConsistent returns the names of every way ledger and the
// aggregated whole-grid counts (ddi, ip, asset) fail to describe the same
// scan, following checkMSConservation's idiom of returning the names of
// what failed rather than a boolean. This is the precondition for the
// baseline-vs-scenario delta being arithmetically true rather than a
// subtraction of two unrelated numbers.
func msScenarioInputsConsistent(ledger *MicrosoftAllocationLedger, ddi, ip, asset int) []string {
	if ledger == nil {
		return []string{"ledger is nil"}
	}

	// Re-assert the per-category Baseline == Attributable + Retained
	// invariant checkMSConservation/msConservationCheck (ms_ledger.go)
	// exists to enforce. Every ledger produced by Scan() already passed
	// this gate before Available: true is ever set, but this function is
	// also reachable with a caller-supplied ledger/diag pair (D-15), so it
	// must not assume that gate ran.
	failing := append([]string{}, msConservationCheck(ledger)...)
	if ledger.DDIObjects.Baseline != ddi {
		failing = append(failing, "DDI Objects baseline disagrees with the aggregated whole-grid count")
	}
	if ledger.ActiveIPs.Baseline != ip {
		failing = append(failing, "Active IPs baseline disagrees with the aggregated whole-grid count")
	}
	if asset != 0 {
		failing = append(failing, "aggregated Managed Assets count is non-zero")
	}
	if ledger.ManagedAssets != (MSCategoryPartition{}) {
		failing = append(failing, "ledger Managed Assets partition is not the zero value")
	}
	dnsAttributable, dhcpAttributable := ledger.DDIObjectsDNS.Attributable, ledger.DDIObjectsDHCP.Attributable
	if dnsAttributable+dhcpAttributable != ledger.DDIObjects.Attributable {
		failing = append(failing, "DNS-side plus DHCP-side attributable counts do not equal the combined attributable count")
	}
	dnsRetained, dhcpRetained := ledger.DDIObjectsDNS.Retained, ledger.DDIObjectsDHCP.Retained
	if dnsRetained+dhcpRetained != ledger.DDIObjects.Retained {
		failing = append(failing, "DNS-side plus DHCP-side retained counts do not equal the combined retained count")
	}
	return failing
}

// msBuildScenarios computes all four scenarios' category tokens, effective
// total, and delta. dnsDDI and dhcpDDI are the per-service Microsoft-
// attributable DDI Object counts; dhcpIP is the Microsoft-attributable
// Active IP count, credited exclusively from DHCP paths (D-01) — it is
// never gated on the DNS switch, so the DNS-only scenario's Active-IP entry
// is always identical to the both-off scenario's. Managed Assets moved is
// the constant 0 in every scenario (D-11): no code path in this function
// reads a server count.
func msBuildScenarios(gridDDI, gridIP, dnsDDI, dhcpDDI, dhcpIP, baselineTokens int) []MSAllocationScenario {
	scenarios := make([]MSAllocationScenario, 0, len(msScenarioOrder))
	for _, def := range msScenarioOrder {
		movedDDI := 0
		if def.dnsEnabled {
			movedDDI += dnsDDI
		}
		if def.dhcpEnabled {
			movedDDI += dhcpDDI
		}
		movedIP := 0
		if def.dhcpEnabled {
			movedIP = dhcpIP
		}

		var categories [3]MSCategoryTokens
		categories[0] = msCategoryTokens(0, gridDDI-movedDDI, movedDDI)
		categories[1] = msCategoryTokens(1, gridIP-movedIP, movedIP)
		categories[2] = msCategoryTokens(2, 0, 0)

		// Sum the three category token counts — never reduce them to their
		// largest value (PROJECT.md's SUM-not-max rule).
		effective := categories[0].Tokens + categories[1].Tokens + categories[2].Tokens

		scenarios = append(scenarios, MSAllocationScenario{
			ID:              def.id,
			DNSEnabled:      def.dnsEnabled,
			DHCPEnabled:     def.dhcpEnabled,
			Categories:      categories,
			EffectiveTokens: effective,
			DeltaTokens:     effective - baselineTokens,
		})
	}
	return scenarios
}
