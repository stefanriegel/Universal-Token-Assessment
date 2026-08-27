import {
  BACKEND_PROVIDER_ID,
  TOKEN_RATES,
  type FindingRow,
  type ProviderType,
} from './mock-data';

export interface ManagementScenarioTotals {
  current: number;
  hybrid: number;
  full: number;
}

export interface AllocationDelta {
  total: number;
}

/**
 * Applies the selected Microsoft allocation to scenarios that still contain
 * NIOS-managed data. A full UDDI migration already prices those objects at
 * native rates, so adding the allocation delta there would double-count them.
 */
export function combineMicrosoftAllocationScenarios(
  niosScenarios: ManagementScenarioTotals,
  nonNiosTokens: number,
  allocationDelta: AllocationDelta,
  growthBufferPct: number,
  hybridIncludesAllNios = false,
): ManagementScenarioTotals {
  const grow = (value: number) => Math.ceil(value * (1 + growthBufferPct));
  const hybridBase = hybridIncludesAllNios ? niosScenarios.full : niosScenarios.hybrid;
  return {
    current: grow(nonNiosTokens + niosScenarios.current + allocationDelta.total),
    hybrid: grow(
      nonNiosTokens
        + hybridBase
        + (hybridIncludesAllNios ? 0 : allocationDelta.total),
    ),
    full: grow(nonNiosTokens + niosScenarios.full),
  };
}

/** Stale imported map keys cannot make a partial selection look complete. */
export function areAllSelectableNiosSourcesMigrated(
  selectableSources: Iterable<string>,
  migrationMap: Map<string, unknown>,
): boolean {
  const sources = Array.from(new Set(selectableSources));
  return sources.length > 0 && sources.every((source) => migrationMap.has(source));
}

interface NiosScenarioMetric {
  memberName: string;
  role?: string;
  runsDnsDhcp?: boolean;
}

/**
 * Build the canonical NIOS migration source list. Management-only GM/GMC
 * members are replaced by UDDI Portal and therefore cannot be migration
 * targets, even when an imported report still contains findings for them.
 */
export function selectableNiosScenarioSources(
  findings: Iterable<Pick<FindingRow, 'provider' | 'source'>>,
  metrics: Iterable<NiosScenarioMetric>,
): string[] {
  const metricList = Array.from(metrics);
  const metricsByName = new Map(metricList.map((metric) => [metric.memberName, metric]));
  const sources = new Set([
    ...Array.from(findings)
      .filter((finding) => finding.provider === 'nios')
      .map((finding) => finding.source),
    ...metricList.map((metric) => metric.memberName),
  ]);
  return Array.from(sources).filter((source) => {
    const metric = metricsByName.get(source);
    const isGm = metric?.role === 'GM' || metric?.role === 'GMC';
    return !isGm || Boolean(metric?.runsDnsDhcp);
  });
}

/** Ignore stale and non-selectable entries when pricing partial migration scenarios. */
export function filterNiosMigrationMap<T>(
  selectableSources: Iterable<string>,
  migrationMap: Map<string, T>,
): Map<string, T> {
  const selectable = new Set(selectableSources);
  return new Map(
    Array.from(migrationMap).filter(([source]) => selectable.has(source)),
  );
}

/** Only NIOS finding overrides invalidate the backup-derived MS allocation. */
export function hasNiosCountOverrides(countOverrides: Record<string, number>): boolean {
  return Object.keys(countOverrides).some((key) => key.startsWith('nios::'));
}

function gcd(a: number, b: number): number {
  while (b !== 0) {
    [a, b] = [b, a % b];
  }
  return a;
}

function lcm(a: number, b: number): number {
  return (a / gcd(a, b)) * b;
}

/**
 * Match calculator.go's ratePool exactly: sum count/rate fractions across all
 * effective rates in a category, then apply one ceiling to the category.
 */
function calcPooledTokensByEffectiveRates(
  rows: FindingRow[],
  effectiveRate: (row: FindingRow) => number,
): number {
  const categories = new Map<string, Map<number, number>>();
  for (const row of rows) {
    const fallback = TOKEN_RATES[row.category];
    const selectedRate = effectiveRate(row);
    const rate = selectedRate > 0 ? selectedRate : fallback;
    const countsByRate = categories.get(row.category) ?? new Map<number, number>();
    countsByRate.set(rate, (countsByRate.get(rate) ?? 0) + row.count);
    categories.set(row.category, countsByRate);
  }

  let total = 0;
  for (const countsByRate of categories.values()) {
    let denominator = 1;
    for (const rate of countsByRate.keys()) denominator = lcm(denominator, rate);
    let numerator = 0;
    for (const [rate, count] of countsByRate) {
      numerator += count * (denominator / rate);
    }
    if (numerator > 0) total += Math.ceil(numerator / denominator);
  }
  return total;
}

/** Aggregate current row rates with one exact fractional pool per category. */
export function calcPooledTokensByCurrentRates(rows: FindingRow[]): number {
  return calcPooledTokensByEffectiveRates(rows, (row) => row.tokensPerUnit);
}

/** Price every row at its native Universal DDI category rate. */
export function calcPooledTokensAtNativeRates(rows: FindingRow[]): number {
  return calcPooledTokensByEffectiveRates(rows, (row) => TOKEN_RATES[row.category]);
}

/**
 * Canonical Current/Hybrid/Full scenario calculator shared by hero cards and
 * exports. A Hybrid category can contain traditional NIOS and native UDDI
 * rates; they remain one fractional pool and are ceiled only after combining.
 */
export function calculateManagementScenarioTotals(
  findings: FindingRow[],
  migratedNiosSources: ReadonlySet<string>,
  allocationDelta: AllocationDelta,
  growthBufferPct: number,
  hybridIncludesAllNios = false,
): ManagementScenarioTotals {
  const grow = (value: number) => Math.ceil(value * (1 + growthBufferPct));
  const current = calcPooledTokensByCurrentRates(findings);
  const hybrid = hybridIncludesAllNios
    ? calcPooledTokensAtNativeRates(findings)
    : calcPooledTokensByEffectiveRates(findings, (row) =>
        row.provider === 'nios' && migratedNiosSources.has(row.source)
          ? TOKEN_RATES[row.category]
          : row.tokensPerUnit,
      );
  const full = calcPooledTokensAtNativeRates(findings);

  return {
    current: grow(current + allocationDelta.total),
    hybrid: grow(hybrid + (hybridIncludesAllNios ? 0 : allocationDelta.total)),
    full: grow(full),
  };
}

/**
 * Provider subtotals are exported as the selected current scenario. NIOS uses
 * its traditional pooled rates plus the selected Microsoft allocation; other
 * providers use native UDDI rates. The same growth buffer is applied to every
 * provider subtotal.
 */
export function buildSelectedProviderTotals(
  findings: FindingRow[],
  allocationDelta: AllocationDelta,
  growthBufferPct: number,
): Record<string, number> {
  const totals: Record<string, number> = {};
  const providers = new Set(findings.map((finding) => finding.provider));
  const grow = (value: number) => Math.ceil(value * (1 + growthBufferPct));

  for (const provider of providers) {
    const rows = findings.filter((finding) => finding.provider === provider);
    const raw = calcPooledTokensByCurrentRates(rows)
      + (provider === 'nios' ? allocationDelta.total : 0);
    totals[BACKEND_PROVIDER_ID[provider]] = grow(raw);
  }
  return totals;
}

export interface ReconciledSourceBreakdownRow {
  key: string;
  label: string;
  provider?: ProviderType;
  tokens: number;
  kind: 'source' | 'pooling' | 'allocation' | 'growth';
}

/**
 * Keeps traceable per-source row totals while making their non-additivity
 * explicit. The generated adjustment rows always reconcile exactly to the
 * authoritative hero total.
 */
export function buildReconciledSourceBreakdown(
  findings: FindingRow[],
  pooledBaseline: number,
  allocationDelta: AllocationDelta,
  displayedTotal: number,
): ReconciledSourceBreakdownRow[] {
  const sourceMap = new Map<string, ReconciledSourceBreakdownRow>();
  for (const finding of findings) {
    const key = `${finding.provider}::${finding.source}`;
    const current = sourceMap.get(key) ?? {
      key,
      label: finding.source,
      provider: finding.provider,
      tokens: 0,
      kind: 'source' as const,
    };
    current.tokens += finding.managementTokens;
    sourceMap.set(key, current);
  }

  const rows = Array.from(sourceMap.values()).sort((a, b) => b.tokens - a.tokens);
  const rawRowTotal = rows.reduce((sum, row) => sum + row.tokens, 0);
  const poolingAdjustment = pooledBaseline - rawRowTotal;
  if (poolingAdjustment !== 0) {
    rows.push({
      key: '__pooling__',
      label: 'Pooled rounding adjustment',
      tokens: poolingAdjustment,
      kind: 'pooling',
    });
  }
  if (allocationDelta.total !== 0) {
    rows.push({
      key: '__allocation__',
      label: 'Microsoft DNS/DHCP allocation',
      tokens: allocationDelta.total,
      kind: 'allocation',
    });
  }
  const preGrowthTotal = pooledBaseline + allocationDelta.total;
  const growthAdjustment = displayedTotal - preGrowthTotal;
  if (growthAdjustment !== 0) {
    rows.push({
      key: '__growth__',
      label: 'Growth buffer adjustment',
      tokens: growthAdjustment,
      kind: 'growth',
    });
  }
  return rows;
}

export function escapeCSVField(value: string | number): string {
  const text = String(value);
  return /[",\r\n]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text;
}

export function serializeCSVRow(values: Array<string | number>): string {
  return values.map(escapeCSVField).join(',');
}
