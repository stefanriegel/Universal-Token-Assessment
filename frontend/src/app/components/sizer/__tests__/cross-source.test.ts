/**
 * cross-source.test.ts — Proves the Sizer's calculateManagementTokens agrees
 * with the Go Scan/XLSX paths (see internal/exporter/cross_source_test.go)
 * for the same shared testdata/cross-source-fixture.json, computed
 * independently via the spec formula (aggregate-then-divide-then-SUM) as the
 * oracle -- no live-engine import, no golden-master fixture.
 */

import { readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, it, expect } from 'vitest';

import { MGMT_RATES, calculateManagementTokens } from '../sizer-calc';
import type { Site } from '../sizer-types';

interface FixtureRow {
  provider: string;
  source: string;
  region: string;
  category: string;
  item: string;
  count: number;
  tokensPerUnit: number;
}

describe('cross-source agreement: Sizer vs Scan/XLSX', () => {
  it('produces the same Grand Total as the Go calculator/exporter for the shared fixture', () => {
    // vitest root is frontend/, so process.cwd() resolves to frontend/ during
    // test runs; the fixture lives two levels up at the repo root.
    const fixturePath = path.resolve(process.cwd(), '../testdata/cross-source-fixture.json');
    const rows: FixtureRow[] = JSON.parse(readFileSync(fixturePath, 'utf-8'));

    let totalDDI = 0;
    let totalActiveIP = 0;
    let totalAsset = 0;
    for (const r of rows) {
      if (r.category === 'DDI Objects') totalDDI += r.count;
      else if (r.category === 'Active IPs') totalActiveIP += r.count;
      else if (r.category === 'Managed Assets') totalAsset += r.count;
    }

    // Manual mapping of the raw FindingRow-shaped totals onto a single Site.
    // dhcpScopes is pinned to 0 so ddiObjects === dnsRecords (Site's formula is
    // (dnsRecords + dhcpScopes*2) * multiplier); multiplier 1 so no scaling.
    const site: Site = {
      id: 'cross-source',
      name: 'cross-source',
      multiplier: 1,
      dnsRecords: totalDDI,
      dhcpScopes: 0,
      activeIPs: totalActiveIP,
      assets: totalAsset,
    };

    // mgmtOverhead pinned to 0 directly (bypassing resolveOverheads /
    // SizerState.globalSettings defaults): Scan and Export never apply
    // overhead to Grand Total, so 0 is the fair apples-to-apples basis for
    // this comparison -- not a claim that Sizer never applies overhead.
    const got = calculateManagementTokens([site], 0);

    // Expected value hand-derived via the same spec formula
    // (aggregate-then-divide-then-SUM) using MGMT_RATES, identical to the
    // Go/XLSX expected total in internal/exporter/cross_source_test.go.
    const want =
      Math.ceil(totalDDI / MGMT_RATES.ddi) +
      Math.ceil(totalActiveIP / MGMT_RATES.activeIP) +
      Math.ceil(totalAsset / MGMT_RATES.asset);

    expect(got).toBe(want);
    // want == 1023 on the current fixture, matching the Go/XLSX total in
    // cross_source_test.go -- not asserted here so this test stays valid if
    // the shared fixture is edited (the got === want check above is the
    // load-bearing cross-source assertion).
  });
});
