/**
 * wizard-export.test.tsx — the Download XLSX call site in wizard.tsx.
 *
 * Drives the real <Wizard/> down the imported-session route (drop a session
 * file on the providers step, Next goes straight to the report) because that is
 * the case the stateless export endpoint exists for: no scan ID, so the old
 * handler fell back to a single-table HTML document named .xls.
 */
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

import { Wizard } from './wizard';
import { SESSION_FORMAT_VERSION, type SessionSnapshot } from './session-io';
import { EstimatorDefaults } from './estimator-calc';
import type { FindingRow } from './mock-data';
import type { NiosServerMetrics } from './nios-calc';
import type { MicrosoftAllocationAPI, MSAllocationScenarioAPI } from './api-client';

// jsdom's global Blob has no .stream(), which undici's Response constructor
// requires, so `new Response(new Blob([...]))` throws 'object.stream is not a
// function' on node 22 (the CI image). A byte array is consumed natively and
// still yields a real Blob from res.blob().
const xlsxBytes = () => new Uint8Array([0x50, 0x4b]);


class MockIntersectionObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}

const finding: FindingRow = {
  provider: 'aws',
  source: 'acct-1',
  region: 'us-east-1',
  category: 'DDI Object',
  item: 'VPCs',
  count: 100,
  tokensPerUnit: 25,
  managementTokens: 4,
};

const niosMember: NiosServerMetrics = {
  memberId: 'm1',
  memberName: 'member-1',
  role: 'DNS/DHCP',
  qps: 1200,
  lps: 30,
  objectCount: 50000,
  activeIPCount: 12000,
  model: 'IB-1425',
  platform: 'Physical',
  managedIPCount: 20000,
  staticHosts: 100,
  dynamicHosts: 200,
  dhcpUtilization: 268,
  runsDnsDhcp: true,
};

function allocationScenario(id: string, deltaTokens: number): MSAllocationScenarioAPI {
  const category = (tokens: number) => ({
    category: 'DDI Objects', niosCount: 0, niosRate: 50, nativeCount: 0, nativeRate: 25,
    niosSubtotalNum: 0, nativeSubtotalNum: 0, subtotalDen: 1, tokens,
  });
  return {
    id,
    dnsEnabled: id === 'dns-only' || id === 'both',
    dhcpEnabled: id === 'dhcp-only' || id === 'both',
    categories: [category(deltaTokens), category(0), category(0)],
    effectiveTokens: 1 + deltaTokens,
    deltaTokens,
  };
}

const microsoftAllocation: MicrosoftAllocationAPI = {
  diagnostic: { available: true },
  baselineTokens: 1,
  scenarios: [
    allocationScenario('none', 0),
    allocationScenario('dns-only', 2),
    allocationScenario('dhcp-only', 0),
    allocationScenario('both', 2),
  ],
  evidence: { relationshipRows: 0, duplicateRelationshipRows: 0, relationshipAnomalies: 0 },
};

function sessionFile(overrides: Partial<SessionSnapshot> = {}): File {
  const snapshot: SessionSnapshot = {
    version: SESSION_FORMAT_VERSION,
    exportedAt: '2026-07-27T00:00:00.000Z',
    toolVersion: '3.10.0',
    selectedProviders: ['aws'],
    findings: [finding],
    countOverrides: {},
    niosMigrationMap: {},
    adMigrationMap: {},
    niosServerMetrics: [],
    adServerMetrics: [],
    estimatorAnswers: { ...EstimatorDefaults },
    growthBufferPct: 0.5,
    serverGrowthBufferPct: 0.2,
    reportingDestEnabled: {},
    reportingDestEvents: {},
    serverMetricOverrides: {},
    ...overrides,
  };
  return new File([JSON.stringify(snapshot)], 'session.json', { type: 'application/json' });
}

/** Health has to answer so the wizard is not in demo mode — demo skips the POST. */
function mockBackend(exportResponse: () => Response) {
  return vi.spyOn(globalThis, 'fetch').mockImplementation(async (input: RequestInfo | URL) => {
    const url = String(typeof input === 'string' || input instanceof URL ? input : input.url);
    if (url.includes('/health')) {
      return new Response(JSON.stringify({ status: 'ok', version: '3.10.0', platform: 'darwin' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }
    if (url.includes('/export')) return exportResponse();
    return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } });
  });
}

/** Renders the wizard, loads a session file, and lands on the report step. */
async function renderWizardOnResultsStep(file: File) {
  render(<Wizard />);
  fireEvent.change(screen.getByTestId('session-file-input'), { target: { files: [file] } });
  await waitFor(() => expect(screen.getByText('session.json')).toBeTruthy());
  fireEvent.click(screen.getByRole('button', { name: /^Next/ }));
  await waitFor(() => expect(screen.getByRole('button', { name: /download xlsx/i })).toBeTruthy());
}

describe('wizard Download XLSX', () => {
  beforeEach(() => {
    vi.stubGlobal('IntersectionObserver', MockIntersectionObserver);
    URL.createObjectURL = vi.fn(() => 'blob:mock');
    URL.revokeObjectURL = vi.fn();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('posts effective counts and displayed totals, not raw findings', async () => {
    const fetchMock = mockBackend(() => new Response(xlsxBytes(), { status: 200 }));

    await renderWizardOnResultsStep(
      sessionFile({ countOverrides: { 'aws::acct-1::VPCs': 250 } })
    );
    fireEvent.click(screen.getByRole('button', { name: /download xlsx/i }));

    await waitFor(() =>
      expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith('/api/v1/export'))).toBe(true)
    );
    const call = fetchMock.mock.calls.find(([url]) => String(url).endsWith('/api/v1/export'))!;
    const body = JSON.parse((call[1] as RequestInit).body as string);

    // The override is applied before the payload leaves the browser.
    expect(body.findings[0].count).toBe(250);
    // ...and translated to the names the Go exporter switches on.
    expect(body.findings[0].category).toBe('DDI Objects');
    expect(body.selectedProviders).toEqual(['aws']);
    // ceil(250/25) = 10 tokens, +50% growth buffer = 15.
    expect(body.totals.grandTotal).toBe(15);
    expect(body.totals.ddiTokens).toBe(15);
    // Provider subtotals use the same selected-scenario growth buffer as the
    // grand total, so the XLSX Summary cannot mix buffered and raw figures.
    expect(body.providerTotals.aws).toBe(15);
    expect(body.growthBufferPct).toBe(0.5);
    // No NIOS or AD in this assessment: the fields must be absent, because the
    // Go exporter gates those sheets on != nil, not on length.
    expect('niosServerMetrics' in body).toBe(false);
    expect('adServerMetrics' in body).toBe(false);
  });

  it('sends the per-member QPS/LPS the user typed, and the raw object split', async () => {
    const fetchMock = mockBackend(() => new Response(xlsxBytes(), { status: 200 }));

    await renderWizardOnResultsStep(
      sessionFile({
        selectedProviders: ['nios'],
        findings: [{ ...finding, provider: 'nios', source: 'member-1' }],
        niosServerMetrics: [niosMember],
        // Keyed by memberId, as applyServerOverrides reads it.
        serverMetricOverrides: { 'm1': { qps: 9000, lps: 400, objects: 70000 } },
      })
    );
    fireEvent.click(screen.getByRole('button', { name: /download xlsx/i }));

    await waitFor(() =>
      expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith('/api/v1/export'))).toBe(true)
    );
    const call = fetchMock.mock.calls.find(([url]) => String(url).endsWith('/api/v1/export'))!;
    const [member] = JSON.parse((call[1] as RequestInit).body as string).niosServerMetrics;

    // QPS and LPS map 1:1 onto the Go fields, so the override travels: the
    // workbook's tier and server tokens are sized from what the user typed.
    expect(member.qps).toBe(9000);
    expect(member.lps).toBe(400);
    // Detail fields stay raw while the sizing-only override travels through
    // serverObjectCount, which both the browser and exporter treat as canonical.
    expect(member.objectCount).toBe(50000);
    expect(member.activeIPCount).toBe(12000);
    expect(member.serverObjectCount).toBe(70000);
    const body = JSON.parse((call[1] as RequestInit).body as string);
    expect(body.niosServerObjectOverrides).toEqual({ m1: 70000 });
  });

  it('preserves an explicit zero Objects override in the XLSX payload', async () => {
    const fetchMock = mockBackend(() => new Response(xlsxBytes(), { status: 200 }));

    await renderWizardOnResultsStep(
      sessionFile({
        selectedProviders: ['nios'],
        findings: [{ ...finding, provider: 'nios', source: 'member-1' }],
        niosServerMetrics: [niosMember],
        niosMigrationMap: { 'member-1': 'nios-x' },
      }),
    );

    const planner = screen.getByTestId('results-migration-planner');
    fireEvent.click(within(planner).getByRole('button', { name: /50,000/ }));
    const objectInput = within(planner).getByRole('spinbutton');
    fireEvent.change(objectInput, { target: { value: '0' } });
    fireEvent.blur(objectInput);

    fireEvent.click(screen.getByRole('button', { name: /download xlsx/i }));
    await waitFor(() =>
      expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith('/api/v1/export'))).toBe(true),
    );

    const call = fetchMock.mock.calls.find(([url]) => String(url).endsWith('/api/v1/export'))!;
    const body = JSON.parse((call[1] as RequestInit).body as string);
    const [exportedMember] = body.niosServerMetrics;
    expect(exportedMember.objectCount).toBe(50_000);
    expect(exportedMember.serverObjectCount).toBe(0);
    expect(body.niosServerObjectOverrides).toEqual({ m1: 0 });
  });

  it('sizes a NIOS member by serverObjectCount without adding active IPs again', async () => {
    const fetchMock = mockBackend(() => new Response(xlsxBytes(), { status: 200 }));
    const tierBoundaryMember: NiosServerMetrics = {
      ...niosMember,
      role: 'GM',
      qps: 100,
      lps: 50,
      objectCount: 2000,
      serverObjectCount: 4000,
      activeIPCount: 12000,
    };

    await renderWizardOnResultsStep(
      sessionFile({
        selectedProviders: ['nios'],
        findings: [{ ...finding, provider: 'nios', source: 'member-1' }],
        niosServerMetrics: [tierBoundaryMember],
        serverGrowthBufferPct: 0,
      }),
    );
    fireEvent.click(screen.getByRole('button', { name: /download xlsx/i }));

    await waitFor(() =>
      expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith('/api/v1/export'))).toBe(true),
    );
    const call = fetchMock.mock.calls.find(([url]) => String(url).endsWith('/api/v1/export'))!;
    const body = JSON.parse((call[1] as RequestInit).body as string);

    // 4,000 objects crosses 2XS (3,000 max) into XS (250 tokens). Adding the
    // 12,000 active IPs again would incorrectly push the member into S (470).
    expect(body.totalServerTokens).toBe(250);
  });

  it('falls back to objectCount when imported serverObjectCount is explicitly zero', async () => {
    const fetchMock = mockBackend(() => new Response(xlsxBytes(), { status: 200 }));
    const legacyZeroMember: NiosServerMetrics = {
      ...niosMember,
      role: 'GM',
      qps: 0,
      lps: 0,
      objectCount: 4_000,
      serverObjectCount: 0,
      activeIPCount: 12_000,
    };

    await renderWizardOnResultsStep(
      sessionFile({
        selectedProviders: ['nios'],
        findings: [{ ...finding, provider: 'nios', source: 'member-1' }],
        niosServerMetrics: [legacyZeroMember],
        serverGrowthBufferPct: 0,
      }),
    );
    fireEvent.click(screen.getByRole('button', { name: /download xlsx/i }));

    await waitFor(() =>
      expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith('/api/v1/export'))).toBe(true),
    );
    const call = fetchMock.mock.calls.find(([url]) => String(url).endsWith('/api/v1/export'))!;
    const body = JSON.parse((call[1] as RequestInit).body as string);

    expect(body.totalServerTokens).toBe(250);
  });

  it('exports the selected NIOS current scenario, allocation delta, and growth consistently', async () => {
    const fetchMock = mockBackend(() => new Response(xlsxBytes(), { status: 200 }));

    await renderWizardOnResultsStep(
      sessionFile({
        selectedProviders: ['nios'],
        findings: [{
          ...finding,
          provider: 'nios',
          source: 'member-1',
          count: 50,
          tokensPerUnit: 50,
          managementTokens: 1,
        }],
        growthBufferPct: 0.2,
        microsoftAllocation,
        selectedMSScenario: 'both',
      }),
    );
    fireEvent.click(screen.getByRole('button', { name: /download xlsx/i }));

    await waitFor(() =>
      expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith('/api/v1/export'))).toBe(true),
    );
    const call = fetchMock.mock.calls.find(([url]) => String(url).endsWith('/api/v1/export'))!;
    const body = JSON.parse((call[1] as RequestInit).body as string);

    // ceil((1 pooled NIOS + 2 selected allocation) * 1.2) = 4.
    expect(body.totals.grandTotal).toBe(4);
    expect(body.providerTotals.nios).toBe(4);
    expect(body.selectedMSScenario).toBe('both');
  });

  it('suppresses the backup-derived allocation delta when counts are manually overridden', async () => {
    const fetchMock = mockBackend(() => new Response(xlsxBytes(), { status: 200 }));
    const overriddenFinding = {
      ...finding,
      provider: 'nios' as const,
      source: 'member-1',
      count: 50,
      tokensPerUnit: 50,
      managementTokens: 1,
    };

    await renderWizardOnResultsStep(
      sessionFile({
        selectedProviders: ['nios'],
        findings: [overriddenFinding],
        countOverrides: { 'nios::member-1::VPCs': 100 },
        growthBufferPct: 0,
        microsoftAllocation,
        selectedMSScenario: 'both',
      }),
    );
    const allocationWarning = screen.getByText(/manual count overrides invalidate the backup-derived delta/i);
    const dnsSwitch = screen.getByRole('switch', { name: /manage microsoft dns/i });
    const dhcpSwitch = screen.getByRole('switch', { name: /manage microsoft dhcp/i });
    expect(allocationWarning.id).toBe('ms-allocation-count-override-note');
    expect((dnsSwitch as HTMLButtonElement).disabled).toBe(true);
    expect((dhcpSwitch as HTMLButtonElement).disabled).toBe(true);
    expect(dnsSwitch.getAttribute('aria-describedby')).toBe(allocationWarning.id);
    expect(dhcpSwitch.getAttribute('aria-describedby')).toBe(allocationWarning.id);
    expect((screen.getByRole('button', { name: /show raw relationship evidence/i }) as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: /download xlsx/i }));

    await waitFor(() =>
      expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith('/api/v1/export'))).toBe(true),
    );
    const call = fetchMock.mock.calls.find(([url]) => String(url).endsWith('/api/v1/export'))!;
    const body = JSON.parse((call[1] as RequestInit).body as string);

    expect(body.findings[0].count).toBe(100);
    expect(body.totals.grandTotal).toBe(2);
    expect(body.providerTotals.nios).toBe(2);
    expect(body.selectedMSScenario).toBe('none');
    expect(body.microsoftAllocation).toBeUndefined();
  });

  it('re-sizes an overridden AD DC and leaves an un-edited one at the backend numbers', async () => {
    const fetchMock = mockBackend(() => new Response(xlsxBytes(), { status: 200 }));

    await renderWizardOnResultsStep(
      sessionFile({
        selectedProviders: ['microsoft'],
        findings: [{ ...finding, provider: 'microsoft', source: 'corp.example.com' }],
        adServerMetrics: [
          { hostname: 'DC01', dnsObjects: 1250, dhcpObjects: 340, dhcpObjectsWithOverhead: 408, qps: 2800, lps: 45, tier: '2XS', serverTokens: 130 },
          { hostname: 'DC02', dnsObjects: 1250, dhcpObjects: 340, dhcpObjectsWithOverhead: 408, qps: 2800, lps: 45, tier: '2XS', serverTokens: 130 },
        ],
        serverMetricOverrides: { 'DC01': { qps: 40000, lps: 300, objects: 60000 } },
      })
    );
    fireEvent.click(screen.getByRole('button', { name: /download xlsx/i }));

    await waitFor(() =>
      expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith('/api/v1/export'))).toBe(true)
    );
    const call = fetchMock.mock.calls.find(([url]) => String(url).endsWith('/api/v1/export'))!;
    const [dc01, dc02] = JSON.parse((call[1] as RequestInit).body as string).adServerMetrics;

    // The AD sheet prints tier and serverTokens verbatim (exporter.go:671-675),
    // so folding QPS without re-sizing would put the new QPS next to the old
    // tier. Both move together.
    expect(dc01.qps).toBe(40000);
    expect(dc01.lps).toBe(300);
    expect(dc01.tier).not.toBe('2XS');
    expect(dc01.serverTokens).toBeGreaterThan(130);
    // An un-edited DC is passed through untouched — no silent re-sizing.
    expect(dc02).toEqual({
      hostname: 'DC02', dnsObjects: 1250, dhcpObjects: 340, dhcpObjectsWithOverhead: 408,
      qps: 2800, lps: 45, tier: '2XS', serverTokens: 130,
    });
  });

  it('surfaces an error rather than silently emitting a legacy .xls', async () => {
    let fail = true;
    mockBackend(() =>
      fail
        ? new Response('export failed', { status: 500 })
        : new Response(xlsxBytes(), { status: 200 })
    );

    await renderWizardOnResultsStep(sessionFile());
    const button = screen.getByRole('button', { name: /download xlsx/i });
    fireEvent.click(button);

    // Next to the button that triggered it, not at the top of a long report.
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toMatch(/export failed/i);
    expect(alert.parentElement?.contains(button)).toBe(true);
    expect(URL.createObjectURL).not.toHaveBeenCalled();

    // A later success clears it — a stale banner is its own wrong signal.
    fail = false;
    fireEvent.click(button);
    // Wait on the export completing, not on the banner clearing: the banner is
    // cleared when the click starts, so waiting for its absence resolves on the
    // first tick and races the fetch.
    await waitFor(() => expect(URL.createObjectURL).toHaveBeenCalled());
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('falls back to the legacy HTML export only in demo mode, never posting to /export', async () => {
    // /health fails on every attempt, so use-backend.ts never reaches
    // 'connected' and isDemo stays true — there is no server to render a
    // real workbook.
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response('unreachable', { status: 500 }));

    await renderWizardOnResultsStep(sessionFile());
    fireEvent.click(screen.getByRole('button', { name: /download xlsx/i }));

    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith('/api/v1/export'))).toBe(
      false
    );
    // 5s, not the 1000ms default: the success path builds a real Blob and the
    // shared CI runner is ~14x slower than local here (107ms local, >1s in CI).
    await waitFor(() => expect(URL.createObjectURL).toHaveBeenCalled(), { timeout: 5000 });
    const blob = (URL.createObjectURL as ReturnType<typeof vi.fn>).mock.calls[0][0] as Blob;
    expect(blob.type).toBe('application/vnd.ms-excel');
  });
});
