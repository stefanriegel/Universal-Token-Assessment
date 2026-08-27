package nios

import (
	"reflect"
	"testing"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/calculator"
)

// msFourScenarioFixtureLedger returns the fixed fixture ledger used across
// this task's behavior block: DDIObjects {Baseline 1140, Attributable 130,
// Retained 1010}; DDIObjectsDNS {Attributable 90, Retained 600};
// DDIObjectsDHCP {Attributable 40, Retained 410}; ActiveIPs {Baseline 300,
// Attributable 40, Retained 260}; ManagedAssets all zero.
func msFourScenarioFixtureLedger() *MicrosoftAllocationLedger {
	return &MicrosoftAllocationLedger{
		DDIObjects:     MSCategoryPartition{Baseline: 1140, Attributable: 130, Retained: 1010},
		DDIObjectsDNS:  MSServiceSplit{Attributable: 90, Retained: 600},
		DDIObjectsDHCP: MSServiceSplit{Attributable: 40, Retained: 410},
		ActiveIPs:      MSCategoryPartition{Baseline: 300, Attributable: 40, Retained: 260},
	}
}

// msFourScenarioFixtureBaseline is the baseline finding row set aggregating
// to 1140 DDI Objects and 300 Active IPs, matching the ledger's baselines.
func msFourScenarioFixtureBaseline() []calculator.FindingRow {
	return []calculator.FindingRow{
		{Category: calculator.CategoryDDIObjects, Count: 1140, TokensPerUnit: calculator.NIOSTokensPerDDIObject},
		{Category: calculator.CategoryActiveIPs, Count: 300, TokensPerUnit: calculator.NIOSTokensPerActiveIP},
	}
}

// TestMSAllocation_FourScenarios pins the exact four-scenario derivation for
// the fixture ledger, including the CONTEXT.md worked "both" case and the
// DNS-only/DHCP-independence guarantee.
func TestMSAllocation_FourScenarios(t *testing.T) {
	ledger := msFourScenarioFixtureLedger()
	baseline := msFourScenarioFixtureBaseline()
	diag := MSAllocationDiagnostic{Available: true}

	set := DeriveMSAllocationScenarios(ledger, diag, baseline)

	if set.BaselineTokens != 35 {
		t.Fatalf("BaselineTokens = %d, want 35 (ceil(1140/50)=23 + ceil(300/25)=12)", set.BaselineTokens)
	}
	if len(set.Scenarios) != 4 {
		t.Fatalf("len(Scenarios) = %d, want 4", len(set.Scenarios))
	}

	want := []struct {
		id                            string
		ddiNIOS, ddiNative, ddiTokens int
		ipNIOS, ipNative, ipTokens    int
		effective, delta              int
	}{
		{MSScenarioNone, 1140, 0, 23, 300, 0, 12, 35, 0},
		{MSScenarioDNSOnly, 1050, 90, 25, 300, 0, 12, 37, 2},
		{MSScenarioDHCPOnly, 1100, 40, 24, 260, 40, 14, 38, 3},
		{MSScenarioBoth, 1010, 130, 26, 260, 40, 14, 40, 5},
	}

	for i, w := range want {
		sc := set.Scenarios[i]
		t.Run(w.id, func(t *testing.T) {
			if sc.ID != w.id {
				t.Errorf("Scenarios[%d].ID = %q, want %q (fixed order)", i, sc.ID, w.id)
			}
			ddi := sc.Categories[0]
			if ddi.NIOSCount != w.ddiNIOS || ddi.NativeCount != w.ddiNative || ddi.Tokens != w.ddiTokens {
				t.Errorf("DDI category = %+v, want NIOS=%d Native=%d Tokens=%d", ddi, w.ddiNIOS, w.ddiNative, w.ddiTokens)
			}
			ip := sc.Categories[1]
			if ip.NIOSCount != w.ipNIOS || ip.NativeCount != w.ipNative || ip.Tokens != w.ipTokens {
				t.Errorf("Active IPs category = %+v, want NIOS=%d Native=%d Tokens=%d", ip, w.ipNIOS, w.ipNative, w.ipTokens)
			}
			if sc.EffectiveTokens != w.effective {
				t.Errorf("EffectiveTokens = %d, want %d", sc.EffectiveTokens, w.effective)
			}
			if sc.DeltaTokens != w.delta {
				t.Errorf("DeltaTokens = %d, want %d", sc.DeltaTokens, w.delta)
			}
			if sc.DeltaTokens < 0 {
				t.Errorf("DeltaTokens = %d, must never be negative (D-10: allocation is a cost)", sc.DeltaTokens)
			}
		})
	}

	t.Run("dns-only Active-IP category is deeply equal to none's", func(t *testing.T) {
		if !reflect.DeepEqual(set.Scenarios[1].Categories[1], set.Scenarios[0].Categories[1]) {
			t.Errorf("dns-only Active-IPs = %+v, none Active-IPs = %+v, want deeply equal",
				set.Scenarios[1].Categories[1], set.Scenarios[0].Categories[1])
		}
	})
}

// TestMSAllocation_ManagedAssetsZero proves every scenario's Managed Assets
// entry is an explicit, fully-populated zero row (D-11, D-12).
func TestMSAllocation_ManagedAssetsZero(t *testing.T) {
	ledger := msFourScenarioFixtureLedger()
	baseline := msFourScenarioFixtureBaseline()
	diag := MSAllocationDiagnostic{Available: true}

	set := DeriveMSAllocationScenarios(ledger, diag, baseline)

	for _, sc := range set.Scenarios {
		ma := sc.Categories[2]
		t.Run(sc.ID, func(t *testing.T) {
			if ma.NIOSCount != 0 || ma.NativeCount != 0 || ma.Tokens != 0 {
				t.Errorf("Managed Assets = %+v, want all-zero counts and tokens", ma)
			}
			if ma.Category != calculator.CategoryManagedAssets {
				t.Errorf("Managed Assets Category = %q, want %q", ma.Category, calculator.CategoryManagedAssets)
			}
			if ma.NIOSRate != calculator.NIOSTokensPerManagedAsset || ma.NativeRate != calculator.TokensPerManagedAsset {
				t.Errorf("Managed Assets rates = (nios=%d, native=%d), want (nios=%d, native=%d)",
					ma.NIOSRate, ma.NativeRate, calculator.NIOSTokensPerManagedAsset, calculator.TokensPerManagedAsset)
			}
		})
	}
}

// TestMSAllocation_BaselineDelta covers immutability, determinism, the
// nil-baseline edge, and the everything-moves adjacency edge.
func TestMSAllocation_BaselineDelta(t *testing.T) {
	t.Run("ledger and baseline are unchanged by derivation, and deriving twice agrees", func(t *testing.T) {
		ledger := msFourScenarioFixtureLedger()
		baseline := msFourScenarioFixtureBaseline()
		diag := MSAllocationDiagnostic{Available: true}

		ledgerBefore := *ledger
		baselineBefore := append([]calculator.FindingRow(nil), baseline...)

		first := DeriveMSAllocationScenarios(ledger, diag, baseline)

		if !reflect.DeepEqual(ledgerBefore, *ledger) {
			t.Errorf("ledger mutated by derivation:\nbefore: %+v\nafter:  %+v", ledgerBefore, *ledger)
		}
		if !reflect.DeepEqual(baselineBefore, baseline) {
			t.Errorf("baseline mutated by derivation:\nbefore: %+v\nafter:  %+v", baselineBefore, baseline)
		}

		second := DeriveMSAllocationScenarios(ledger, diag, baseline)
		if !reflect.DeepEqual(first, second) {
			t.Errorf("deriving twice from identical inputs disagreed:\nfirst:  %+v\nsecond: %+v", first, second)
		}
	})

	t.Run("nil baseline yields baseline 0 and four all-zero scenarios with no panic", func(t *testing.T) {
		ledger := &MicrosoftAllocationLedger{}
		diag := MSAllocationDiagnostic{Available: true}

		set := DeriveMSAllocationScenarios(ledger, diag, nil)

		if set.BaselineTokens != 0 {
			t.Errorf("BaselineTokens = %d, want 0", set.BaselineTokens)
		}
		if len(set.Scenarios) != 4 {
			t.Fatalf("len(Scenarios) = %d, want 4", len(set.Scenarios))
		}
		for _, sc := range set.Scenarios {
			if sc.EffectiveTokens != 0 || sc.DeltaTokens != 0 {
				t.Errorf("scenario %s: EffectiveTokens=%d DeltaTokens=%d, want 0/0", sc.ID, sc.EffectiveTokens, sc.DeltaTokens)
			}
		}
	})

	t.Run("everything-moves adjacency: both scenario's DDI NIOS side reaches exactly zero", func(t *testing.T) {
		ledger := &MicrosoftAllocationLedger{
			DDIObjects:     MSCategoryPartition{Baseline: 130, Attributable: 130, Retained: 0},
			DDIObjectsDNS:  MSServiceSplit{Attributable: 90, Retained: 0},
			DDIObjectsDHCP: MSServiceSplit{Attributable: 40, Retained: 0},
		}
		baseline := []calculator.FindingRow{
			{Category: calculator.CategoryDDIObjects, Count: 130, TokensPerUnit: calculator.NIOSTokensPerDDIObject},
		}
		diag := MSAllocationDiagnostic{Available: true}

		set := DeriveMSAllocationScenarios(ledger, diag, baseline)

		both := set.Scenarios[3]
		if both.ID != MSScenarioBoth {
			t.Fatalf("Scenarios[3].ID = %q, want %q", both.ID, MSScenarioBoth)
		}
		ddi := both.Categories[0]
		if ddi.NIOSCount != 0 {
			t.Errorf("both scenario DDI NIOSCount = %d, want 0 (everything moved)", ddi.NIOSCount)
		}
		if ddi.NativeCount != 130 {
			t.Errorf("both scenario DDI NativeCount = %d, want 130", ddi.NativeCount)
		}
		if ddi.Category != calculator.CategoryDDIObjects {
			t.Errorf("both scenario DDI Category = %q, want %q (category still fully populated at zero NIOS side)", ddi.Category, calculator.CategoryDDIObjects)
		}
	})
}

// TestMSAllocation_DiagnosticBranching covers Absent, Unavailable, a
// zero-value diagnostic, and every inconsistent-input form (D-14, D-15).
func TestMSAllocation_DiagnosticBranching(t *testing.T) {
	baseline := msFourScenarioFixtureBaseline()

	t.Run("Absent yields four honest zero-delta scenarios and a usable baseline", func(t *testing.T) {
		diag := MSAllocationDiagnostic{Available: false, Code: MSAllocationAbsentCode}

		set := DeriveMSAllocationScenarios(nil, diag, baseline)

		if set.BaselineTokens != 35 {
			t.Fatalf("BaselineTokens = %d, want 35", set.BaselineTokens)
		}
		if len(set.Scenarios) != 4 {
			t.Fatalf("len(Scenarios) = %d, want 4", len(set.Scenarios))
		}
		for i, want := range []string{MSScenarioNone, MSScenarioDNSOnly, MSScenarioDHCPOnly, MSScenarioBoth} {
			sc := set.Scenarios[i]
			if sc.ID != want {
				t.Errorf("Scenarios[%d].ID = %q, want %q", i, sc.ID, want)
			}
			if sc.Categories[0].NativeCount != 0 || sc.Categories[1].NativeCount != 0 {
				t.Errorf("scenario %s: native counts = (ddi=%d, ip=%d), want 0/0 (nothing moves when Absent)",
					sc.ID, sc.Categories[0].NativeCount, sc.Categories[1].NativeCount)
			}
			if sc.EffectiveTokens != 35 {
				t.Errorf("scenario %s: EffectiveTokens = %d, want 35", sc.ID, sc.EffectiveTokens)
			}
			if sc.DeltaTokens != 0 {
				t.Errorf("scenario %s: DeltaTokens = %d, want 0", sc.ID, sc.DeltaTokens)
			}
		}
	})

	t.Run("Unavailable yields no scenarios and passes the diagnostic through byte-identically", func(t *testing.T) {
		diag := MSAllocationDiagnostic{Available: false, Code: MSAllocationUnavailableCode, Message: msAllocationUnavailableMessage}

		set := DeriveMSAllocationScenarios(nil, diag, baseline)

		if len(set.Scenarios) != 0 {
			t.Errorf("len(Scenarios) = %d, want 0", len(set.Scenarios))
		}
		if set.BaselineTokens != 35 {
			t.Errorf("BaselineTokens = %d, want 35", set.BaselineTokens)
		}
		if set.Diagnostic != diag {
			t.Errorf("Diagnostic = %+v, want byte-identical to input %+v", set.Diagnostic, diag)
		}
	})

	t.Run("a zero-value diagnostic yields no scenarios and is passed through unchanged", func(t *testing.T) {
		diag := MSAllocationDiagnostic{}

		set := DeriveMSAllocationScenarios(nil, diag, baseline)

		if len(set.Scenarios) != 0 {
			t.Errorf("len(Scenarios) = %d, want 0", len(set.Scenarios))
		}
		if set.Diagnostic != diag {
			t.Errorf("Diagnostic = %+v, want unchanged %+v", set.Diagnostic, diag)
		}
	})

	assertSuppressed := func(t *testing.T, set MSAllocationScenarioSet) {
		t.Helper()
		if len(set.Scenarios) != 0 {
			t.Errorf("len(Scenarios) = %d, want 0 (suppressed)", len(set.Scenarios))
		}
		if set.Diagnostic.Available {
			t.Errorf("Diagnostic.Available = true, want false (suppressed)")
		}
		if set.Diagnostic.Code != MSAllocationUnavailableCode {
			t.Errorf("Diagnostic.Code = %q, want %q (reuse the existing code, never a new one)", set.Diagnostic.Code, MSAllocationUnavailableCode)
		}
		if set.Diagnostic.Message != msAllocationUnavailableMessage {
			t.Errorf("Diagnostic.Message = %q, want the existing fixed message (never a new one)", set.Diagnostic.Message)
		}
	}

	t.Run("inconsistent inputs: ledger DDI baseline disagrees with aggregated baseline findings", func(t *testing.T) {
		ledger := msFourScenarioFixtureLedger()
		mismatched := []calculator.FindingRow{
			{Category: calculator.CategoryDDIObjects, Count: 1000, TokensPerUnit: calculator.NIOSTokensPerDDIObject},
			{Category: calculator.CategoryActiveIPs, Count: 300, TokensPerUnit: calculator.NIOSTokensPerActiveIP},
		}
		diag := MSAllocationDiagnostic{Available: true}

		set := DeriveMSAllocationScenarios(ledger, diag, mismatched)
		assertSuppressed(t, set)
	})

	t.Run("inconsistent inputs: DNS-side plus DHCP-side attributable does not sum to combined attributable", func(t *testing.T) {
		ledger := msFourScenarioFixtureLedger()
		ledger.DDIObjectsDNS.Attributable = 99 // 99 + 40 != 130
		diag := MSAllocationDiagnostic{Available: true}

		set := DeriveMSAllocationScenarios(ledger, diag, baseline)
		assertSuppressed(t, set)
	})

	t.Run("inconsistent inputs: ledger's Managed Assets partition is non-zero", func(t *testing.T) {
		ledger := msFourScenarioFixtureLedger()
		ledger.ManagedAssets = MSCategoryPartition{Baseline: 5, Attributable: 5, Retained: 0}
		diag := MSAllocationDiagnostic{Available: true}

		set := DeriveMSAllocationScenarios(ledger, diag, baseline)
		assertSuppressed(t, set)
	})

	t.Run("inconsistent inputs: ledger Active-IP baseline disagrees with aggregated Active-IP findings", func(t *testing.T) {
		ledger := msFourScenarioFixtureLedger()
		mismatched := []calculator.FindingRow{
			{Category: calculator.CategoryDDIObjects, Count: 1140, TokensPerUnit: calculator.NIOSTokensPerDDIObject},
			{Category: calculator.CategoryActiveIPs, Count: 250, TokensPerUnit: calculator.NIOSTokensPerActiveIP},
		}
		diag := MSAllocationDiagnostic{Available: true}

		set := DeriveMSAllocationScenarios(ledger, diag, mismatched)
		assertSuppressed(t, set)
	})
}

func TestMSAllocation_ConservationGateReachedIndependently(t *testing.T) {
	ledger := &MicrosoftAllocationLedger{
		// Baseline (1140) != Attributable (2000) + Retained (1010): the
		// conservation invariant is broken, but every other
		// msScenarioInputsConsistent check below still passes.
		DDIObjects:     MSCategoryPartition{Baseline: 1140, Attributable: 2000, Retained: 1010},
		DDIObjectsDNS:  MSServiceSplit{Attributable: 1000, Retained: 600},
		DDIObjectsDHCP: MSServiceSplit{Attributable: 1000, Retained: 410},
		ActiveIPs:      MSCategoryPartition{Baseline: 300, Attributable: 40, Retained: 260},
	}
	baseline := msFourScenarioFixtureBaseline() // aggregates to 1140 DDI, 300 Active IPs
	diag := MSAllocationDiagnostic{Available: true}

	set := DeriveMSAllocationScenarios(ledger, diag, baseline)

	if len(set.Scenarios) != 0 {
		t.Fatalf("len(Scenarios) = %d, want 0 (suppressed): a negative NIOSCount must never reach the caller", len(set.Scenarios))
	}
	if set.Diagnostic.Available {
		t.Errorf("Diagnostic.Available = true, want false (suppressed)")
	}
	if set.Diagnostic.Code != MSAllocationUnavailableCode {
		t.Errorf("Diagnostic.Code = %q, want %q (reuse the existing code, never a new one)", set.Diagnostic.Code, MSAllocationUnavailableCode)
	}
	if set.Diagnostic.Message != msAllocationUnavailableMessage {
		t.Errorf("Diagnostic.Message = %q, want the existing fixed message (never a new one)", set.Diagnostic.Message)
	}
}
