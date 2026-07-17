import { describe, expect, it } from 'vitest';
import { calcNiosScenarios, calcNiosTokens, calcUddiTokensAggregated, computeMarginalDelta } from './mock-data';
import type { FindingRow, ServerFormFactor } from './mock-data';
import {
  LARGE_MEMBER,
  REGULAR_MEMBER,
  marginalDeltaFindings,
} from './results/__tests__/fixtures/marginal-delta';

function row(category: FindingRow['category'], count: number): FindingRow {
  return {
    provider: 'nios',
    source: 'test-member',
    region: '',
    category,
    item: category,
    count,
    tokensPerUnit: 0,
    managementTokens: 0,
  };
}

describe('calcNiosTokens', () => {
  it('sums per-category ceilings instead of taking the max', () => {
    // DDI: 100/50=2, Active IP: 26/25=2, Asset: 14/13=2 -> sum=6, not max=2
    const rows = [row('DDI Object', 100), row('Active IP', 26), row('Asset', 14)];
    expect(calcNiosTokens(rows)).toBe(6);
  });

  it('matches calcUddiTokensAggregated shape (sum-of-three, not max-of-three)', () => {
    const rows = [row('DDI Object', 1), row('Active IP', 1), row('Asset', 1)];
    expect(calcNiosTokens(rows)).toBe(3);
    expect(calcUddiTokensAggregated(rows)).toBe(3);
  });
});

describe('computeMarginalDelta', () => {
  function stayingTotal(findings: FindingRow[], map: Map<string, ServerFormFactor>): number {
    return calcNiosTokens(findings.filter((f) => !map.has(f.source)));
  }
  function migratingTotal(findings: FindingRow[], map: Map<string, ServerFormFactor>): number {
    return calcUddiTokensAggregated(findings.filter((f) => map.has(f.source)));
  }

  it('single-member toggle delta matches actual before/after diff', () => {
    const before = new Map<string, ServerFormFactor>();
    const after = new Map<string, ServerFormFactor>([[LARGE_MEMBER, 'nios-x']]);

    const delta = computeMarginalDelta(marginalDeltaFindings, before, after);

    expect(delta.stayingDelta).toBe(
      stayingTotal(marginalDeltaFindings, after) - stayingTotal(marginalDeltaFindings, before),
    );
    expect(delta.migratingDelta).toBe(
      migratingTotal(marginalDeltaFindings, after) - migratingTotal(marginalDeltaFindings, before),
    );
    // Sanity: moving the large member out of `staying` and into `migrating`
    // must not be a wash — this is exactly the non-additivity the bug exploited.
    expect(delta.stayingDelta).not.toBe(0);
    expect(delta.migratingDelta).not.toBe(0);
  });

  it('toggle-then-toggle-back returns exactly to prior totals', () => {
    const before = new Map<string, ServerFormFactor>([[REGULAR_MEMBER, 'nios-x']]);
    const afterToggleOn = new Map<string, ServerFormFactor>([
      [REGULAR_MEMBER, 'nios-x'],
      [LARGE_MEMBER, 'nios-x'],
    ]);

    const forward = computeMarginalDelta(marginalDeltaFindings, before, afterToggleOn);
    const back = computeMarginalDelta(marginalDeltaFindings, afterToggleOn, before);

    expect(forward.stayingDelta + back.stayingDelta).toBe(0);
    expect(forward.migratingDelta + back.migratingDelta).toBe(0);
  });

  it('bulk toggle produces one aggregate delta, not a sum of individual deltas', () => {
    const before = new Map<string, ServerFormFactor>();
    const afterBulk = new Map<string, ServerFormFactor>([
      [LARGE_MEMBER, 'nios-x'],
      [REGULAR_MEMBER, 'nios-x'],
    ]);

    const bulkDelta = computeMarginalDelta(marginalDeltaFindings, before, afterBulk);

    // Sum of two INDEPENDENTLY-computed single-member deltas (each measured
    // from the same `before` baseline — the WRONG approach this change
    // forbids) must differ from the true aggregate delta because
    // ceil(a) + ceil(b) != ceil(a+b).
    const afterLargeOnly = new Map<string, ServerFormFactor>([[LARGE_MEMBER, 'nios-x']]);
    const afterRegularOnly = new Map<string, ServerFormFactor>([[REGULAR_MEMBER, 'nios-x']]);
    const deltaLargeOnly = computeMarginalDelta(marginalDeltaFindings, before, afterLargeOnly);
    const deltaRegularOnly = computeMarginalDelta(marginalDeltaFindings, before, afterRegularOnly);
    const summedIndividualDeltas =
      deltaLargeOnly.stayingDelta + deltaRegularOnly.stayingDelta;

    expect(bulkDelta.stayingDelta).not.toBe(summedIndividualDeltas);
    expect(bulkDelta.stayingDelta).toBe(
      stayingTotal(marginalDeltaFindings, afterBulk) - stayingTotal(marginalDeltaFindings, before),
    );
    expect(bulkDelta.migratingDelta).toBe(
      migratingTotal(marginalDeltaFindings, afterBulk) - migratingTotal(marginalDeltaFindings, before),
    );
  });
});

describe('calcNiosScenarios', () => {
  const findings: FindingRow[] = [
    { provider: 'nios', source: 'gm.example.com', region: '', category: 'DDI Object', item: 'Networks', count: 500, tokensPerUnit: 50, managementTokens: 10 },
    { provider: 'nios', source: 'member1.example.com', region: '', category: 'DDI Object', item: 'Networks', count: 500, tokensPerUnit: 50, managementTokens: 10 },
  ];
  const allMemberNames = ['gm.example.com', 'member1.example.com'];

  it('current equals hybrid when migration map is empty', () => {
    const result = calcNiosScenarios(findings, new Map(), allMemberNames);
    expect(result.current).toBe(result.hybrid);
  });

  it('complete (all Native) is greater than current (all NIOS)', () => {
    const result = calcNiosScenarios(findings, new Map(), allMemberNames);
    expect(result.complete).toBeGreaterThan(result.current);
  });

  it('hybrid sits between current and complete when partially migrated', () => {
    const niosMigrationMap = new Map<string, ServerFormFactor>([['member1.example.com', 'nios-x']]);
    const result = calcNiosScenarios(findings, niosMigrationMap, allMemberNames);
    expect(result.current).toBeLessThanOrEqual(result.hybrid);
    expect(result.hybrid).toBeLessThanOrEqual(result.complete);
  });
});
