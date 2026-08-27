/**
 * ms-allocation-parity.test.tsx -- the browser end of MSPAR-04's cross-surface
 * parity proof (D-04, D-08).
 *
 * `server/microsoft_allocation_drift_test.go` pins the TypeScript
 * `MicrosoftAllocationAPI` wire interface's field *names* against the Go
 * domain and compares no values. `internal/scanner/nios/ms_allocation_parity_test.go`
 * (plans 08-01/08-02) pins every Go-reachable hop's *values* -- scanner,
 * session, API, export, workbook -- to the checked-in
 * `testdata/ms-allocation/*.json` snapshots. This file is the sixth hop: it
 * reads those exact same JSON files (never a browser-local copy, never a
 * regenerated fixture -- both languages read one file on disk), round-trips
 * each through the real session save/load path (`exportSession` ->
 * `importSession`), and asserts every number `<MSAllocationPanel/>` renders
 * against the snapshot it was given.
 *
 * Per D-08, no real-browser (Playwright) pass is in scope here -- this is a
 * jsdom unit render, and visual legibility/discoverability remain unproven
 * by any automated test (see the closing comment block below and
 * 08-04-SUMMARY.md).
 */
import { readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, it, expect, afterEach } from 'vitest';
import { render, screen, cleanup, within } from '@testing-library/react';

import { MSAllocationPanel } from '../ms-allocation-panel';
import type {
  MicrosoftAllocationAPI,
  MSAllocationScenarioAPI,
  MSCategoryTokensAPI,
} from '../../api-client';
import {
  exportSession,
  importSession,
  SESSION_FORMAT_VERSION,
  type SessionExportInput,
} from '../../session-io';
import { EstimatorDefaults } from '../../estimator-calc';

afterEach(cleanup);

function noop() {}

/**
 * Reads a checked-in Go/browser-shared snapshot directly off disk. A read or
 * parse failure throws and is never caught here -- a missing/renamed golden
 * must fail this suite, not skip it (see the fail-first proof recorded in
 * 08-04-SUMMARY.md). Vitest's root is `frontend/`, so `process.cwd()`
 * resolves there during test runs; the fixtures live two levels up at the
 * repo root (same pattern as `sizer/__tests__/cross-source.test.ts`).
 */
function loadMSAllocationSnapshot(name: string): MicrosoftAllocationAPI {
  const fixturePath = path.resolve(process.cwd(), '../testdata/ms-allocation/' + name + '.json');
  return JSON.parse(readFileSync(fixturePath, 'utf-8'));
}

type Branch = 'available' | 'absent' | 'unavailable';

/** One case per snapshot on disk (`ls testdata/ms-allocation/*.json` -> 8). */
const MS_PARITY_CASES: { name: string; selection: string; branch: Branch }[] = [
  { name: 'both', selection: 'both', branch: 'available' },
  { name: 'dns-only', selection: 'dns-only', branch: 'available' },
  { name: 'dhcp-only', selection: 'dhcp-only', branch: 'available' },
  { name: 'held-back', selection: 'both', branch: 'available' },
  { name: 'absent', selection: 'none', branch: 'absent' },
  { name: 'unavailable', selection: 'none', branch: 'unavailable' },
  { name: 'boundary-exact', selection: 'both', branch: 'available' },
  { name: 'boundary-plus-one', selection: 'both', branch: 'available' },
];

/** Minimal SessionExportInput -- everything unrelated to microsoftAllocation
 * is zero/empty, mirroring session-io.test.ts's baseInput factory. */
function baseSessionInput(): SessionExportInput {
  return {
    selectedProviders: [],
    findings: [],
    countOverrides: {},
    niosMigrationMap: new Map(),
    adMigrationMap: new Map(),
    niosServerMetrics: [],
    adServerMetrics: [],
    estimatorAnswers: { ...EstimatorDefaults },
    growthBufferPct: 0.2,
    serverGrowthBufferPct: 0.2,
    reportingDestEnabled: {},
    reportingDestEvents: {},
  };
}

/** Round-trips a snapshot through the real session save/load path and
 * returns the reloaded MicrosoftAllocationAPI -- never the originally
 * parsed one -- so the session round trip is genuinely in the render path. */
async function roundTripAllocation(
  snapshot: MicrosoftAllocationAPI,
  selection: string,
): Promise<MicrosoftAllocationAPI> {
  const input: SessionExportInput = {
    ...baseSessionInput(),
    microsoftAllocation: snapshot,
    selectedMSScenario: selection,
  };
  const json = exportSession(input, 'v0.0.0-parity');
  const file = new File([json], 'session.json', { type: 'application/json' });
  const reloaded = await importSession(file);
  return reloaded.microsoftAllocation!;
}

const MICROSOFT_ALLOCATION_KEYS: (keyof MicrosoftAllocationAPI)[] = [
  'diagnostic',
  'baselineTokens',
  'scenarios',
  'evidence',
];
const SCENARIO_KEYS: (keyof MSAllocationScenarioAPI)[] = [
  'id',
  'dnsEnabled',
  'dhcpEnabled',
  'categories',
  'effectiveTokens',
  'deltaTokens',
];
const CATEGORY_KEYS: (keyof MSCategoryTokensAPI)[] = [
  'category',
  'niosCount',
  'niosRate',
  'nativeCount',
  'nativeRate',
  'niosSubtotalNum',
  'nativeSubtotalNum',
  'subtotalDen',
  'tokens',
];

describe('MS allocation cross-language parity (MSPAR-04)', () => {
  for (const tc of MS_PARITY_CASES) {
    describe(tc.name, () => {
      const snapshot = loadMSAllocationSnapshot(tc.name);

      it('carries every key MicrosoftAllocationAPI declares', () => {
        for (const key of MICROSOFT_ALLOCATION_KEYS) {
          expect(Object.prototype.hasOwnProperty.call(snapshot, key)).toBe(true);
        }
      });

      if (snapshot.scenarios) {
        it('one scenario carries every MSAllocationScenarioAPI key, and one category every MSCategoryTokensAPI key', () => {
          const scenario = snapshot.scenarios[0];
          for (const key of SCENARIO_KEYS) {
            expect(Object.prototype.hasOwnProperty.call(scenario, key)).toBe(true);
          }
          for (const key of CATEGORY_KEYS) {
            expect(Object.prototype.hasOwnProperty.call(scenario.categories[0], key)).toBe(true);
          }
        });
      }

      it('round-trips through exportSession -> importSession with every value unchanged', async () => {
        const input: SessionExportInput = {
          ...baseSessionInput(),
          microsoftAllocation: snapshot,
          selectedMSScenario: tc.selection,
        };
        const json = exportSession(input, 'v0.0.0-parity');
        const file = new File([json], 'session.json', { type: 'application/json' });
        const reloaded = await importSession(file);

        expect(reloaded.version).toBe(SESSION_FORMAT_VERSION);
        expect(reloaded.selectedMSScenario).toBe(tc.selection);
        expect(reloaded.microsoftAllocation).toEqual(snapshot);
      });

      it('renders the panel from the reloaded snapshot matching its diagnostic branch', async () => {
        const allocation = await roundTripAllocation(snapshot, tc.selection);

        if (tc.branch === 'absent') {
          const { container } = render(
            <MSAllocationPanel
              allocation={allocation}
              selected={tc.selection}
              onSelect={noop}
              evidenceOpen={false}
              setEvidenceOpen={noop}
            />,
          );
          expect(container.firstChild).toBeNull();
          return;
        }

        if (tc.branch === 'unavailable') {
          const { container } = render(
            <MSAllocationPanel
              allocation={allocation}
              selected={tc.selection}
              onSelect={noop}
              evidenceOpen={false}
              setEvidenceOpen={noop}
            />,
          );
          expect(screen.getByText(allocation.diagnostic.message!)).toBeInTheDocument();
          expect(document.getElementById('ms-allocation-dns')).toBeDisabled();
          expect(document.getElementById('ms-allocation-dhcp')).toBeDisabled();
          expect(container.textContent ?? '').not.toMatch(/\d/);
          return;
        }

        // tc.branch === 'available'.
        const scenario = allocation.scenarios!.find((s) => s.id === tc.selection)!;
        render(
          <MSAllocationPanel
            allocation={allocation}
            selected={tc.selection}
            onSelect={noop}
            evidenceOpen
            setEvidenceOpen={noop}
          />,
        );

        if (tc.selection !== 'none') {
          expect(
            screen.getByText(new RegExp(`\\+${scenario.deltaTokens.toLocaleString()} additional tokens vs all-NIOS`)),
          ).toBeInTheDocument();
        } else {
          expect(screen.queryByText(/additional tokens vs all-NIOS/)).toBeNull();
        }

        expect(
          screen.getByText(`Relationship rows observed: ${allocation.evidence.relationshipRows.toLocaleString()}`),
        ).toBeInTheDocument();
        expect(
          screen.getByText(
            `Duplicate relationship rows: ${allocation.evidence.duplicateRelationshipRows.toLocaleString()}`,
          ),
        ).toBeInTheDocument();
        expect(
          screen.getByText(`Relationship anomalies: ${allocation.evidence.relationshipAnomalies.toLocaleString()}`),
        ).toBeInTheDocument();
      });
    });
  }

  // ── D-10: all four selections driven through a real render (Task 2). ──
  describe('held-back fixture: all four selections (D-10)', () => {
    const snapshot = loadMSAllocationSnapshot('held-back');

    it.each(['none', 'dns-only', 'dhcp-only', 'both'] as const)(
      'selection=%s matches the snapshot delta line',
      async (selection) => {
        const allocation = await roundTripAllocation(snapshot, selection);
        const scenario = allocation.scenarios!.find((s) => s.id === selection)!;

        render(
          <MSAllocationPanel
            allocation={allocation}
            selected={selection}
            onSelect={noop}
            evidenceOpen={false}
            setEvidenceOpen={noop}
          />,
        );

        if (selection === 'none') {
          expect(screen.queryByText(/additional tokens vs all-NIOS/)).toBeNull();
        } else {
          expect(
            screen.getByText(new RegExp(`\\+${scenario.deltaTokens.toLocaleString()} additional tokens vs all-NIOS`)),
          ).toBeInTheDocument();
        }
      },
    );


  });
});

/**
 * What this on-screen hop does not prove:
 *
 * - The panel renders no category-level count and no per-category subtotal
 *   -- only the delta total, the held-back rollups, the held-back table, and
 *   the evidence counts. Category-level figures (niosCount/nativeCount/
 *   subtotal numerators) are pinned on the Go surfaces
 *   (internal/scanner/nios/ms_allocation_parity_test.go) and in the workbook
 *   (internal/exporter), never on this screen.
 * - Per D-08, visual legibility and discoverability remain a human
 *   judgement, unproven by any automated test here. A Playwright pass
 *   against the checked-in synthetic backup is recorded in 08-CONTEXT.md as
 *   the milestone's highest-value deferred item.
 */
