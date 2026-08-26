// Package nios — mixed-rate pooled category token arithmetic.
//
// ms_tokens.go computes the Phase 6 management-token math for a Microsoft
// allocation scenario: for each of the three token categories (DDI Objects,
// Active IPs, Managed Assets), pool the NIOS-retained raw count and the
// native-UDDI raw count over their respective rates, and apply exactly one
// ceiling to the pooled result (D-06). This is genuinely new arithmetic:
// calculator.Calculate pools everything at native rates and ignores
// TokensPerUnit, and the exporter package's NIOS sheet pools everything at
// NIOS rates — neither can express a single category mixing both rate
// families.
package nios

import "github.com/stefanriegel/Universal-Token-Assessment/internal/calculator"

// msPooledCategoryTokens computes one category's management-token count by
// pooling a NIOS-rate count and a native-rate count and applying exactly one
// ceiling to the pooled numerator (D-06). Per-side rounding is forbidden:
// ceiling each side of the CONTEXT.md worked example separately (1,010 NIOS
// objects at rate 50, 130 native objects at rate 25) gives 21 + 6 = 27,
// while pooling first gives the correct answer of 26 — a real difference
// that a customer is billed on.
//
// The rounding contract is exact: all arithmetic is integer-only, the pooled
// numerator is ceiled exactly once using the same (n+d-1)/d idiom
// calculator.CeilDiv uses, and there is no other rounding step anywhere — no
// half-up, half-to-even, floor, or truncate, and no non-integer numeric
// type. There is no tie-breaking question because ceiling division is not a
// tie-breaking operation.
//
// niosNum and nativeNum are returned as integer numerators over the shared
// denominator den, rather than as formatted numbers, because den is always
// one of exactly three fixed rate products (50*25=1250, 25*13=325,
// 13*3=39), and a decimal expansion over 325 or 39 does not terminate — a
// formatted string could not represent the exact subtotal D-07 requires.
// (niosNum + nativeNum + den - 1) / den always equals the returned token
// count, so a displayed subtotal can never contradict the category total.
//
// int is 64-bit on every supported build target (amd64, arm64). The largest
// den is 1250, and a ten-million-object grid yields a pooled numerator near
// 7.5e8 — nine orders of magnitude below the 64-bit ceiling — so no
// overflow guard is warranted (T-06-05).
func msPooledCategoryTokens(niosCount, niosRate, nativeCount, nativeRate int) (tokens, niosNum, nativeNum, den int) {
	den = niosRate * nativeRate
	niosNum = niosCount * nativeRate
	nativeNum = nativeCount * niosRate

	pooled := niosNum + nativeNum
	if pooled == 0 {
		return 0, niosNum, nativeNum, den
	}
	tokens = (pooled + den - 1) / den
	return tokens, niosNum, nativeNum, den
}

// MSCategoryTokens reports one token category's raw counts, rates, and exact
// unrounded subtotals (D-07, MSTOK-01). It must have exactly nine fields —
// Category plus eight ints — and Category must carry only one of the three
// calculator.Category* constant values. No further string field may be
// added: a free-text field is capable of carrying a backup-derived identifier.
type MSCategoryTokens struct {
	Category          string `json:"category"`
	NIOSCount         int    `json:"niosCount"`
	NIOSRate          int    `json:"niosRate"`
	NativeCount       int    `json:"nativeCount"`
	NativeRate        int    `json:"nativeRate"`
	NIOSSubtotalNum   int    `json:"niosSubtotalNum"`
	NativeSubtotalNum int    `json:"nativeSubtotalNum"`
	SubtotalDen       int    `json:"subtotalDen"`
	Tokens            int    `json:"tokens"`
}

// msTokenCategoryRate fixes one category's name and its two rates.
type msTokenCategoryRate struct {
	category   string
	niosRate   int
	nativeRate int
}

// msTokenCategories fixes the three categories, in the same fixed order
// msCategoryNames already fixes: DDI Objects, Active IPs, Managed Assets.
// Every rate is read from the calculator package's exported constants — no
// numeric rate literal appears elsewhere in this file.
var msTokenCategories = []msTokenCategoryRate{
	{calculator.CategoryDDIObjects, calculator.NIOSTokensPerDDIObject, calculator.TokensPerDDIObject},
	{calculator.CategoryActiveIPs, calculator.NIOSTokensPerActiveIP, calculator.TokensPerActiveIP},
	{calculator.CategoryManagedAssets, calculator.NIOSTokensPerManagedAsset, calculator.TokensPerManagedAsset},
}

// msCategoryTokens builds one category's MSCategoryTokens report by pooling
// niosCount and nativeCount at msTokenCategories[idx]'s rates.
func msCategoryTokens(idx int, niosCount, nativeCount int) MSCategoryTokens {
	rate := msTokenCategories[idx]
	tokens, niosNum, nativeNum, den := msPooledCategoryTokens(niosCount, rate.niosRate, nativeCount, rate.nativeRate)
	return MSCategoryTokens{
		Category:          rate.category,
		NIOSCount:         niosCount,
		NIOSRate:          rate.niosRate,
		NativeCount:       nativeCount,
		NativeRate:        rate.nativeRate,
		NIOSSubtotalNum:   niosNum,
		NativeSubtotalNum: nativeNum,
		SubtotalDen:       den,
		Tokens:            tokens,
	}
}

// msAggregateNIOSCategoryCounts sums Count per category across findings,
// tolerating a nil or empty slice and ignoring any row whose category
// matches none of the three calculator.Category* constants. This is a third
// local copy of the same ten-line aggregation loop already present in
// calculator.Calculate and the exporter package's NIOS sheet builder
// (calcNiosTokens); the duplication is deliberate — importing the workbook
// layer into the scanner would invert the dependency direction permanently,
// and this loop is smaller than the coupling that would buy.
func msAggregateNIOSCategoryCounts(findings []calculator.FindingRow) (ddi, ip, asset int) {
	for _, row := range findings {
		switch row.Category {
		case calculator.CategoryDDIObjects:
			ddi += row.Count
		case calculator.CategoryActiveIPs:
			ip += row.Count
		case calculator.CategoryManagedAssets:
			asset += row.Count
		}
	}
	return ddi, ip, asset
}

// msAllNIOSTokens computes the D-09 whole-grid all-NIOS baseline
// management-token total: the number the scan already reports with both
// Microsoft switches off, covering Microsoft and non-Microsoft resources
// alike. It aggregates the three category counts from findings, then routes
// each through msPooledCategoryTokens with the whole-grid count on the NIOS
// side and zero on the native side, and sums the three resulting token
// counts — summing, never reducing to a maximum. Routing the baseline
// through the same pooling function as every allocation scenario is
// deliberate: it makes it impossible for the baseline and an allocated
// total to round by different rules, so the delta between them is
// arithmetically true. The Microsoft-attributable slice alone would not
// yield a true delta, because each ceiling lands on a pooled whole-grid
// count, not on the slice in isolation.
func msAllNIOSTokens(findings []calculator.FindingRow) int {
	ddi, ip, asset := msAggregateNIOSCategoryCounts(findings)
	counts := [3]int{ddi, ip, asset}

	var total int
	for i, rate := range msTokenCategories {
		tokens, _, _, _ := msPooledCategoryTokens(counts[i], rate.niosRate, 0, rate.nativeRate)
		total += tokens
	}
	return total
}
