import { describe, expect, it } from 'vitest';

import type { FindingRow } from './mock-data';
import {
  areAllSelectableNiosSourcesMigrated,
  buildReconciledSourceBreakdown,
  buildSelectedProviderTotals,
  calculateManagementScenarioTotals,
  calcPooledTokensAtNativeRates,
  calcPooledTokensByCurrentRates,
  combineMicrosoftAllocationScenarios,
  escapeCSVField,
  filterNiosMigrationMap,
  hasNiosCountOverrides,
  selectableNiosScenarioSources,
  serializeCSVRow,
} from './scenario-parity';

function finding(over: Partial<FindingRow> = {}): FindingRow {
  return {
    provider: 'nios',
    source: 'member-1',
    region: '',
    category: 'DDI Object',
    item: 'DNS records',
    count: 25,
    tokensPerUnit: 50,
    managementTokens: 1,
    ...over,
  };
}

describe('combined management scenario parity', () => {
  it('locks the supplied backup progression without double-counting Full UDDI', () => {
    const scenarios = combineMicrosoftAllocationScenarios(
      { current: 15_823, hybrid: 18_135, full: 31_002 },
      0,
      { total: 2_190 },
      0,
    );

    expect(scenarios).toEqual({ current: 18_013, hybrid: 20_325, full: 31_002 });
  });

  it('applies growth once to each complete pooled scenario', () => {
    const scenarios = combineMicrosoftAllocationScenarios(
      { current: 100, hybrid: 150, full: 200 },
      10,
      { total: 20 },
      0.2,
    );

    expect(scenarios.current).toBe(156); // ceil((10 + 100 + 20) * 1.2)
    expect(scenarios.hybrid).toBe(216); // ceil((10 + 150 + 20) * 1.2)
    expect(scenarios.full).toBe(252); // no allocation delta at Full UDDI
  });

  it('collapses Hybrid to Full when all valid sources are selected, ignoring stale keys', () => {
    const migrationMap = new Map<string, unknown>([['a', true], ['b', true], ['stale-import-key', true]]);
    expect(areAllSelectableNiosSourcesMigrated(['a', 'b'], migrationMap)).toBe(true);
    expect(areAllSelectableNiosSourcesMigrated(['a', 'b', 'c'], migrationMap)).toBe(false);

    const scenarios = combineMicrosoftAllocationScenarios(
      { current: 100, hybrid: 150, full: 200 },
      0,
      { total: 20 },
      0,
      true,
    );
    expect(scenarios.hybrid).toBe(scenarios.full);
  });

  it('does not classify an empty or stale-only migration map as Full', () => {
    expect(areAllSelectableNiosSourcesMigrated([], new Map())).toBe(false);
    expect(
      areAllSelectableNiosSourcesMigrated([], new Map([['stale-import-key', true]])),
    ).toBe(false);
  });

  it('removes stale and infra-only entries before partial scenario pricing', () => {
    const selectable = selectableNiosScenarioSources(
      [
        finding({ source: 'migrate.example' }),
        finding({ source: 'infra-gm.example' }),
      ],
      [
        { memberName: 'migrate.example', role: 'DNS/DHCP', runsDnsDhcp: true },
        { memberName: 'stay.example', role: 'DNS/DHCP', runsDnsDhcp: true },
        { memberName: 'infra-gm.example', role: 'GM', runsDnsDhcp: false },
      ],
    );
    expect(selectable).toEqual(['migrate.example', 'stay.example']);

    const filtered = filterNiosMigrationMap(
      selectable,
      new Map([
        ['migrate.example', 'nios-x'],
        ['infra-gm.example', 'nios-x'],
        ['stale.example', 'nios-x'],
      ]),
    );
    expect(Array.from(filtered.keys())).toEqual(['migrate.example']);
  });

  it('invalidates Microsoft allocation only for NIOS count overrides', () => {
    expect(hasNiosCountOverrides({ 'nios::member-1::DNS records': 100 })).toBe(true);
    expect(hasNiosCountOverrides({ 'aws::acct-1::VPCs': 250 })).toBe(false);
  });
});

describe('authoritative totals and traceability', () => {
  it('pools same-rate rows before rounding', () => {
    const rows = [
      finding({ source: 'a', count: 1, managementTokens: 1 }),
      finding({ source: 'b', count: 1, managementTokens: 1 }),
    ];
    expect(rows.reduce((sum, row) => sum + row.managementTokens, 0)).toBe(2);
    expect(calcPooledTokensByCurrentRates(rows)).toBe(1);
  });

  it('pools mixed rates as exact fractions before the category ceiling', () => {
    const rows = [
      finding({ source: 'nios-member', count: 1_010, tokensPerUnit: 50 }),
      finding({ provider: 'aws', source: 'cloud-account', count: 130, tokensPerUnit: 25 }),
    ];

    // ceil(1010/50 + 130/25) = ceil(25.4) = 26. Per-rate ceilings would be 27.
    expect(calcPooledTokensByCurrentRates(rows)).toBe(26);
    expect(calcPooledTokensAtNativeRates(rows)).toBe(46);
  });

  it('uses one mixed-rate category pool for Current, Hybrid, and Full', () => {
    const rows = [
      finding({ source: 'migrate', count: 1_010, tokensPerUnit: 50 }),
      finding({ source: 'stay', count: 20, tokensPerUnit: 50 }),
      finding({ provider: 'aws', source: 'cloud', count: 130, tokensPerUnit: 25 }),
    ];

    const scenarios = calculateManagementScenarioTotals(
      rows,
      new Set(['migrate']),
      { total: 2 },
      0,
    );

    expect(scenarios).toEqual({
      current: 28, // ceil(1010/50 + 20/50 + 130/25) + allocation
      hybrid: 48, // ceil(1010/25 + 20/50 + 130/25) + allocation
      full: 47, // ceil((1010 + 20 + 130)/25), allocation is never added
    });
  });

  it('adds explicit adjustments so source evidence reconciles to the hero total', () => {
    const rows = buildReconciledSourceBreakdown(
      [
        finding({ source: 'a', managementTokens: 10_000 }),
        finding({ source: 'b', managementTokens: 5_850 }),
      ],
      15_823,
      { total: 2_190 },
      18_013,
    );

    expect(rows.find((row) => row.kind === 'pooling')?.tokens).toBe(-27);
    expect(rows.find((row) => row.kind === 'allocation')?.tokens).toBe(2_190);
    expect(rows.reduce((sum, row) => sum + row.tokens, 0)).toBe(18_013);
  });
});

describe('export parity', () => {
  it('exports selected current provider totals with allocation and growth', () => {
    const totals = buildSelectedProviderTotals(
      [
        finding({ provider: 'nios', count: 50 }),
        finding({ provider: 'aws', source: 'acct', count: 25, tokensPerUnit: 25 }),
      ],
      { total: 2 },
      0.2,
    );
    expect(totals.nios).toBe(4); // ceil((1 NIOS + 2 allocation) * 1.2)
    expect(totals.aws).toBe(2); // ceil(1 UDDI * 1.2)
  });

  it('quotes commas, quotes, and newlines according to RFC 4180', () => {
    expect(escapeCSVField('DNS records, A and AAAA')).toBe('"DNS records, A and AAAA"');
    expect(escapeCSVField('a "quoted" value')).toBe('"a ""quoted"" value"');
    expect(serializeCSVRow(['plain', 'line\nbreak', 3])).toBe('plain,"line\nbreak",3');
  });
});
