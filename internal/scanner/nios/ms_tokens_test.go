package nios

import (
	"testing"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/calculator"
)

// TestMSAllocation_PoolThenCeil pins the pool-then-ceil-once rounding
// contract (D-06): pooling the NIOS-side and native-side raw counts before
// applying a single ceiling, never ceiling each side separately and adding.
func TestMSAllocation_PoolThenCeil(t *testing.T) {
	t.Run("worked example: pooling beats ceil-then-add by one token", func(t *testing.T) {
		tokens, niosNum, nativeNum, den := msPooledCategoryTokens(1010, 50, 130, 25)
		if tokens != 26 {
			t.Errorf("tokens = %d, want 26", tokens)
		}
		if niosNum != 25250 {
			t.Errorf("niosNum = %d, want 25250", niosNum)
		}
		if nativeNum != 6500 {
			t.Errorf("nativeNum = %d, want 6500", nativeNum)
		}
		if den != 1250 {
			t.Errorf("den = %d, want 1250", den)
		}

		ceilEachSide := calculator.CeilDiv(1010, 50) + calculator.CeilDiv(130, 25)
		if ceilEachSide != 27 {
			t.Fatalf("ceil-each-side sanity check = %d, want 27", ceilEachSide)
		}
		if tokens == ceilEachSide {
			t.Errorf("pooled tokens (%d) must differ from ceil-each-side (%d) for this worked example", tokens, ceilEachSide)
		}
	})

	t.Run("boundary: exact multiple of denominator", func(t *testing.T) {
		tokens, _, _, _ := msPooledCategoryTokens(1000, 50, 125, 25)
		if tokens != 25 {
			t.Errorf("tokens = %d, want 25", tokens)
		}
	})

	t.Run("boundary: one unit above exact multiple", func(t *testing.T) {
		tokens, _, _, _ := msPooledCategoryTokens(1000, 50, 126, 25)
		if tokens != 26 {
			t.Errorf("tokens = %d, want 26", tokens)
		}
	})

	t.Run("boundary: one unit below exact multiple", func(t *testing.T) {
		tokens, _, _, _ := msPooledCategoryTokens(999, 50, 125, 25)
		if tokens != 25 {
			t.Errorf("tokens = %d, want 25", tokens)
		}
	})

	t.Run("empty: zero both sides", func(t *testing.T) {
		tokens, niosNum, nativeNum, den := msPooledCategoryTokens(0, 50, 0, 25)
		if tokens != 0 {
			t.Errorf("tokens = %d, want 0", tokens)
		}
		if niosNum != 0 || nativeNum != 0 {
			t.Errorf("niosNum=%d nativeNum=%d, want 0, 0", niosNum, nativeNum)
		}
		if den != 1250 {
			t.Errorf("den = %d, want 1250 even when both counts are zero", den)
		}
	})

	t.Run("Active IPs rates", func(t *testing.T) {
		tokens, _, _, den := msPooledCategoryTokens(100, 25, 26, 13)
		if tokens != 6 {
			t.Errorf("tokens = %d, want 6", tokens)
		}
		if den != 325 {
			t.Errorf("den = %d, want 325", den)
		}
	})

	t.Run("Managed Assets rates", func(t *testing.T) {
		tokens, niosNum, nativeNum, den := msPooledCategoryTokens(0, 13, 1, 3)
		if niosNum != 0 {
			t.Errorf("niosNum = %d, want 0", niosNum)
		}
		if nativeNum != 13 {
			t.Errorf("nativeNum = %d, want 13", nativeNum)
		}
		if den != 39 {
			t.Errorf("den = %d, want 39", den)
		}
		if tokens != 1 {
			t.Errorf("tokens = %d, want 1", tokens)
		}
	})

	t.Run("ten-million-scale exactness", func(t *testing.T) {
		tokens, _, _, _ := msPooledCategoryTokens(10000000, 50, 10000000, 25)
		if tokens != 600000 {
			t.Errorf("tokens = %d, want 600000", tokens)
		}
	})
}

// TestMSAllocation_Subtotals proves the exact-subtotal identity: the
// returned numerators over the shared denominator always re-derive the
// returned token count, and the single-rate degenerate case agrees with
// calculator.CeilDiv over a swept range (D-07, MSTOK-01).
func TestMSAllocation_Subtotals(t *testing.T) {
	cases := []struct {
		name                    string
		niosCount, niosRate     int
		nativeCount, nativeRate int
	}{
		{"worked example", 1010, 50, 130, 25},
		{"exact multiple", 1000, 50, 125, 25},
		{"one above multiple", 1000, 50, 126, 25},
		{"one below multiple", 999, 50, 125, 25},
		{"zero both", 0, 50, 0, 25},
		{"active IPs rates", 100, 25, 26, 13},
		{"managed assets rates", 0, 13, 1, 3},
		{"ten million scale", 10000000, 50, 10000000, 25},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tokens, niosNum, nativeNum, den := msPooledCategoryTokens(c.niosCount, c.niosRate, c.nativeCount, c.nativeRate)
			want := (niosNum + nativeNum + den - 1) / den
			if tokens != want {
				t.Errorf("tokens = %d, but (niosNum+nativeNum+den-1)/den = %d — subtotal identity broken", tokens, want)
			}
		})
	}

	t.Run("single-rate degenerate case matches calculator.CeilDiv (NIOS side)", func(t *testing.T) {
		niosRates := []int{calculator.NIOSTokensPerDDIObject, calculator.NIOSTokensPerActiveIP, calculator.NIOSTokensPerManagedAsset}
		nativeRates := []int{calculator.TokensPerDDIObject, calculator.TokensPerActiveIP, calculator.TokensPerManagedAsset}
		for i, niosRate := range niosRates {
			nativeRate := nativeRates[i]
			for n := 0; n <= 200; n++ {
				tokens, _, _, _ := msPooledCategoryTokens(n, niosRate, 0, nativeRate)
				want := calculator.CeilDiv(n, niosRate)
				if tokens != want {
					t.Fatalf("rate pair %d/%d, n=%d: tokens = %d, want calculator.CeilDiv = %d", niosRate, nativeRate, n, tokens, want)
				}
			}
		}
	})

	t.Run("single-rate degenerate case matches calculator.CeilDiv (native side)", func(t *testing.T) {
		niosRates := []int{calculator.NIOSTokensPerDDIObject, calculator.NIOSTokensPerActiveIP, calculator.NIOSTokensPerManagedAsset}
		nativeRates := []int{calculator.TokensPerDDIObject, calculator.TokensPerActiveIP, calculator.TokensPerManagedAsset}
		for i, niosRate := range niosRates {
			nativeRate := nativeRates[i]
			for n := 0; n <= 200; n++ {
				tokens, _, _, _ := msPooledCategoryTokens(0, niosRate, n, nativeRate)
				want := calculator.CeilDiv(n, nativeRate)
				if tokens != want {
					t.Fatalf("rate pair %d/%d, n=%d: tokens = %d, want calculator.CeilDiv = %d", niosRate, nativeRate, n, tokens, want)
				}
			}
		}
	})

	t.Run("msCategoryTokens populates all nine fields consistently", func(t *testing.T) {
		got := msCategoryTokens(0, 1010, 130)
		want := MSCategoryTokens{
			Category:          calculator.CategoryDDIObjects,
			NIOSCount:         1010,
			NIOSRate:          50,
			NativeCount:       130,
			NativeRate:        25,
			NIOSSubtotalNum:   25250,
			NativeSubtotalNum: 6500,
			SubtotalDen:       1250,
			Tokens:            26,
		}
		if got != want {
			t.Errorf("msCategoryTokens(0, 1010, 130) = %+v, want %+v", got, want)
		}
	})
}

// TestMSAllocation_BaselineAggregate proves the D-09 whole-grid all-NIOS
// baseline total is derivable from the scanner's own finding rows, through
// msPooledCategoryTokens, with no dependence on row order or the exporter
// package.
func TestMSAllocation_BaselineAggregate(t *testing.T) {
	t.Run("nil findings aggregate to zero", func(t *testing.T) {
		ddi, ip, asset := msAggregateNIOSCategoryCounts(nil)
		if ddi != 0 || ip != 0 || asset != 0 {
			t.Errorf("got (%d, %d, %d), want (0, 0, 0)", ddi, ip, asset)
		}
	})

	t.Run("empty findings aggregate to zero", func(t *testing.T) {
		ddi, ip, asset := msAggregateNIOSCategoryCounts([]calculator.FindingRow{})
		if ddi != 0 || ip != 0 || asset != 0 {
			t.Errorf("got (%d, %d, %d), want (0, 0, 0)", ddi, ip, asset)
		}
	})

	sampleRows := []calculator.FindingRow{
		{Provider: "nios", Source: "member-a", Category: calculator.CategoryDDIObjects, Item: "A records", Count: 400},
		{Provider: "nios", Source: "member-b", Category: calculator.CategoryDDIObjects, Item: "networks", Count: 600},
		{Provider: "nios", Source: "member-a", Category: calculator.CategoryActiveIPs, Item: "Active IPs", Count: 160},
		{Provider: "nios", Source: "member-b", Category: calculator.CategoryActiveIPs, Item: "Active IPs", Count: 100},
		{Provider: "nios", Source: "member-a", Category: "Unrecognized Category", Item: "ignored", Count: 9999},
	}

	t.Run("sums per category regardless of source, ignores unrecognised category", func(t *testing.T) {
		ddi, ip, asset := msAggregateNIOSCategoryCounts(sampleRows)
		if ddi != 1000 {
			t.Errorf("ddi = %d, want 1000", ddi)
		}
		if ip != 260 {
			t.Errorf("ip = %d, want 260", ip)
		}
		if asset != 0 {
			t.Errorf("asset = %d, want 0", asset)
		}
	})

	t.Run("nil findings yield zero baseline tokens", func(t *testing.T) {
		if got := msAllNIOSTokens(nil); got != 0 {
			t.Errorf("msAllNIOSTokens(nil) = %d, want 0", got)
		}
	})

	t.Run("baseline sums category tokens, never maxes them", func(t *testing.T) {
		got := msAllNIOSTokens(sampleRows)
		want := calculator.CeilDiv(1000, calculator.NIOSTokensPerDDIObject) + calculator.CeilDiv(260, calculator.NIOSTokensPerActiveIP)
		if want != 31 {
			t.Fatalf("sanity check want = %d, expected 31", want)
		}
		if got != want {
			t.Errorf("msAllNIOSTokens = %d, want %d", got, want)
		}
	})

	t.Run("row order does not affect the baseline total", func(t *testing.T) {
		reversed := make([]calculator.FindingRow, len(sampleRows))
		for i, row := range sampleRows {
			reversed[len(sampleRows)-1-i] = row
		}
		forward := msAllNIOSTokens(sampleRows)
		back := msAllNIOSTokens(reversed)
		if forward != back {
			t.Errorf("shuffled order gave %d, forward order gave %d", back, forward)
		}
	})

	t.Run("baseline equals per-category CeilDiv sum for randomised triples", func(t *testing.T) {
		triples := [][3]int{
			{0, 0, 0},
			{1, 1, 1},
			{50, 25, 13},
			{1234, 5678, 9},
			{999999, 1, 2},
		}
		for _, tr := range triples {
			rows := []calculator.FindingRow{
				{Category: calculator.CategoryDDIObjects, Count: tr[0]},
				{Category: calculator.CategoryActiveIPs, Count: tr[1]},
				{Category: calculator.CategoryManagedAssets, Count: tr[2]},
			}
			got := msAllNIOSTokens(rows)
			want := calculator.CeilDiv(tr[0], calculator.NIOSTokensPerDDIObject) +
				calculator.CeilDiv(tr[1], calculator.NIOSTokensPerActiveIP) +
				calculator.CeilDiv(tr[2], calculator.NIOSTokensPerManagedAsset)
			if got != want {
				t.Errorf("triple %v: msAllNIOSTokens = %d, want %d", tr, got, want)
			}
		}
	})
}
