// Shared synthetic fixtures for migration-planner-marginal-delta (and reused by
// migration-planner-usability per that change's task 1.3).
//
// Shape mirrors the real-world repro that motivated this change: one large
// Grid Manager member with a huge object/active-IP count, plus one regular
// member with modest counts. Generic member IDs only — no customer names,
// see `[[feedback_no_customer_names]]`.

import type { FindingRow } from '../../../mock-data';

const row = (source: string, category: FindingRow['category'], count: number): FindingRow => ({
  provider: 'nios',
  source,
  region: '',
  category,
  item: category,
  count,
  tokensPerUnit: 0,
  managementTokens: 0,
});

export const LARGE_MEMBER = 'grid-manager-01';
export const REGULAR_MEMBER = 'member-02';

// Large GM member: 318,828 objects / 1,507 active IPs (ZF repro shape, genericized).
// Regular member: modest counts.
export const marginalDeltaFindings: FindingRow[] = [
  row(LARGE_MEMBER, 'DDI Object', 318_828),
  row(LARGE_MEMBER, 'Active IP', 1_507),
  row(REGULAR_MEMBER, 'DDI Object', 4_200),
  row(REGULAR_MEMBER, 'Active IP', 340),
];
