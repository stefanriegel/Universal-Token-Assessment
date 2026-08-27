// Package calculator implements the Infoblox Universal DDI token math.
//
// Token sizing constants (from official Infoblox documentation):
//   - DDI Objects: 1 token per 25 objects (ceiling division)
//   - Active IPs:  1 token per 13 IPs (ceiling division)
//   - Managed Assets: 1 token per 3 assets (ceiling division)
//   - Grand total = DDITokens + IPTokens + AssetTokens
//
// NIOS Grid uses different (more generous) ratios because NIOS licensing
// covers multiple management domains per token:
//   - DDI Objects: 1 token per 50 objects
//   - Active IPs:  1 token per 25 IPs
//   - Managed Assets: 1 token per 13 assets
//
// NIOS rates apply to the current-state NIOS token count. When members
// migrate to NIOS-X (UDDI native), the native rates above apply instead.
package calculator

const (
	CategoryDDIObjects    = "DDI Objects"
	CategoryActiveIPs     = "Active IPs"
	CategoryManagedAssets = "Managed Assets"

	// Native UDDI rates (AWS, Azure, GCP, NIOS-X, BlueCat, EfficientIP).
	TokensPerDDIObject    = 25
	TokensPerActiveIP     = 13
	TokensPerManagedAsset = 3

	// NIOS Grid rates — for counting objects managed by an existing NIOS Grid.
	// These apply to FindingRows emitted by the NIOS scanner for current-state
	// token estimation. Migration Planner uses native rates for migrated members.
	NIOSTokensPerDDIObject    = 50
	NIOSTokensPerActiveIP     = 25
	NIOSTokensPerManagedAsset = 13
)

// FindingRow is the universal currency between all scanner phases and the results display.
// It represents a single resource type discovered by a provider.
type FindingRow struct {
	// Provider is the cloud/directory provider name (e.g. "aws", "azure", "gcp", "ad").
	Provider string
	// Source is the account identifier (AWS account ID, Azure subscription ID, GCP project ID, AD domain).
	Source string
	// Region is the cloud region this row was scanned from (e.g. "us-east-1"). Empty for global resources.
	Region string
	// Category is one of CategoryDDIObjects, CategoryActiveIPs, or CategoryManagedAssets.
	Category string
	// Item is the resource type name (e.g. "vpc", "subnet", "vm").
	Item string
	// Count is the number of discovered resources of this type.
	Count int
	// TokensPerUnit is the divisor used for ceiling division (25, 13, or 3).
	TokensPerUnit int
	// ManagementTokens is ceiling(Count / TokensPerUnit) for this individual row.
	ManagementTokens int
}

// TokenResult holds the aggregated token calculation across all findings.
type TokenResult struct {
	// DDITokens is the pooled DDI Objects token count: every row's Count is
	// divided by that row's own TokensPerUnit, the quotients are summed, and
	// exactly one ceiling is applied to the sum.
	DDITokens int
	// IPTokens is the pooled Active IPs token count (same contract as DDITokens).
	IPTokens int
	// AssetTokens is the pooled Managed Assets token count (same contract).
	AssetTokens int
	// GrandTotal is DDITokens + IPTokens + AssetTokens (SUM-native, matching the
	// official Infoblox engine: managementTokensTotal = reduce(+)).
	GrandTotal int
	// Findings is the original input slice, returned for traceability.
	Findings []FindingRow
}

// CeilDiv computes ceiling(n / d). Returns 0 if n is 0 to avoid division concerns.
// Panics if d is 0 (caller must supply non-zero divisor).
// Exported so that all scanners share the same integer ceiling-division logic.
func CeilDiv(n, d int) int {
	if n == 0 {
		return 0
	}
	return (n + d - 1) / d
}

// Calculate aggregates all findings by category, pools each category at the
// per-row rates, and returns a TokenResult.
//
// Rates are read from each row's TokensPerUnit, never assumed: a NIOS Grid row
// carries a NIOS rate (50/25/13) while a cloud or NIOS-X row carries a native
// rate (25/13/3), and the same category routinely contains both. Dividing every
// row by the native rate would report a NIOS-only assessment at roughly twice
// its real token cost.
//
// Pooling happens before division and exactly one ceiling is applied per
// category: all rows in a category are summed as exact fractions over their
// own rates, and the pooled fraction is ceiled once. Per-rate ceiling is
// forbidden — 1,010 objects at rate 50 plus 130 at rate 25 is 26 tokens
// pooled, but 21 + 6 = 27 if each side is ceiled separately, and a customer
// is billed on that difference.
func Calculate(findings []FindingRow) TokenResult {
	if findings == nil {
		findings = []FindingRow{}
	}

	ddi := ratePool{fallback: TokensPerDDIObject}
	ip := ratePool{fallback: TokensPerActiveIP}
	asset := ratePool{fallback: TokensPerManagedAsset}

	for _, row := range findings {
		switch row.Category {
		case CategoryDDIObjects:
			ddi.add(row.Count, row.TokensPerUnit)
		case CategoryActiveIPs:
			ip.add(row.Count, row.TokensPerUnit)
		case CategoryManagedAssets:
			asset.add(row.Count, row.TokensPerUnit)
		}
	}

	ddiTokens := ddi.tokens()
	ipTokens := ip.tokens()
	assetTokens := asset.tokens()

	return TokenResult{
		DDITokens:   ddiTokens,
		IPTokens:    ipTokens,
		AssetTokens: assetTokens,
		GrandTotal:  ddiTokens + ipTokens + assetTokens,
		Findings:    findings,
	}
}

// ratePool accumulates one category's counts keyed by the rate each row was
// measured at, so a category mixing NIOS-rate and native-rate rows is ceiled
// once over the pooled fraction rather than once per rate.
//
// fallback is the rate applied to a row whose TokensPerUnit is unset or
// non-positive — every scanner sets it, but a zero would panic on division and
// silently dropping the row would understate the total.
type ratePool struct {
	fallback int
	counts   map[int]int
}

func (p *ratePool) add(count, rate int) {
	if rate <= 0 {
		rate = p.fallback
	}
	if p.counts == nil {
		p.counts = make(map[int]int, 2)
	}
	p.counts[rate] += count
}

// tokens sums count/rate over a common denominator and applies one ceiling.
// The denominator is the LCM of the rates present, so the sum is exact — no
// floating point, no intermediate rounding. Both the LCM and the sum are
// order-independent, so map iteration order cannot change the result.
//
// The largest denominator reachable from the six rate constants is
// lcm(50,25,13,3) = 1950, so even a ten-million-object category leaves the
// numerator nine orders of magnitude below the 64-bit ceiling.
func (p *ratePool) tokens() int {
	den := 1
	for rate := range p.counts {
		den = lcm(den, rate)
	}
	num := 0
	for rate, count := range p.counts {
		num += count * (den / rate)
	}
	if num <= 0 {
		return 0
	}
	return (num + den - 1) / den
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func lcm(a, b int) int {
	return a / gcd(a, b) * b
}
