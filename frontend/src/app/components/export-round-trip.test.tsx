/**
 * export-round-trip.test.tsx — the promise the v2 session format exists to keep:
 * a session saved and reloaded exports the same workbook.
 *
 * Driven through the real <Wizard/> rather than through an extracted payload
 * builder, so the whole chain is under test: saveSession -> exportSession ->
 * validateSessionSchema -> mergeSnapshots -> applySnapshot -> buildReportPayload.
 * A field dropped by any one of those links fails this test.
 *
 * Payloads are compared, not xlsx bytes: excelize stamps docProps/core.xml with
 * creation and modification times and the zip entries carry mtimes, so two
 * renders are never byte-equal. The payload is the complete input to the
 * renderer, so payload equality is workbook equality — and a mismatch names the
 * field instead of saying "files differ".
 */
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

import { Wizard } from './wizard';
import { SESSION_FORMAT_VERSION, validateSessionSchema, type SessionSnapshot } from './session-io';
import { calcEstimator, EstimatorDefaults } from './estimator-calc';
import type { ReportExportPayload, MicrosoftAllocationAPI } from './api-client';
import type { NiosServerMetrics } from './nios-calc';

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

/**
 * Every field carries a distinctive, non-default value. A field left at its
 * zero value cannot fail this test, which is the one way the round trip could
 * quietly stop protecting anything — `exercisesEveryField` below enforces that.
 *
 * 'microsoft' is deliberately among the providers: Go sees it as 'ad', so it
 * pins the namespace translation on both the findings and the errors path.
 */
const FIXTURE: SessionSnapshot = {
  version: SESSION_FORMAT_VERSION,
  exportedAt: '2026-07-27T00:00:00.000Z',
  toolVersion: '3.10.0',
  selectedProviders: ['aws', 'microsoft', 'nios'],
  findings: [
    { provider: 'aws', source: 'acct-1', region: 'us-east-1', category: 'DDI Object', item: 'VPCs', count: 100, tokensPerUnit: 25, managementTokens: 4 },
    { provider: 'microsoft', source: 'corp.example.com', region: '', category: 'Active IP', item: 'AD Computers', count: 640, tokensPerUnit: 13, managementTokens: 50 },
    { provider: 'nios', source: 'member-1', region: '', category: 'Asset', item: 'Discovered Devices', count: 900, tokensPerUnit: 3, managementTokens: 300 },
  ],
  countOverrides: { 'aws::acct-1::VPCs': 250 },
  niosMigrationMap: { 'member-1': 'nios-x' },
  adMigrationMap: { DC01: 'nios-x', DC02: 'nios-xaas' },
  niosServerMetrics: [niosMember],
  adServerMetrics: [
    { hostname: 'DC01', dnsObjects: 1250, dhcpObjects: 340, dhcpObjectsWithOverhead: 408, qps: 2800, lps: 45, tier: '2XS', serverTokens: 130 },
    { hostname: 'DC02', dnsObjects: 8500, dhcpObjects: 1200, dhcpObjectsWithOverhead: 1440, qps: 12000, lps: 120, tier: 'XS', serverTokens: 250 },
  ],
  // Protocol logging on, or monthlyLogVolume is 0 and reportingTokens with it.
  estimatorAnswers: {
    ...EstimatorDefaults,
    activeIPs: 5000,
    sites: 3,
    networksPerSite: 9,
    enableDNSProtocol: true,
    enableDHCPLog: true,
  },
  growthBufferPct: 0.35,
  serverGrowthBufferPct: 0.45,
  reportingDestEnabled: { csp: true, s3: false, ecosystem: true, 'local-syslog': true },
  reportingDestEvents: { csp: 4_000_000, s3: 2_500_000, ecosystem: 1_000_000, 'local-syslog': 900_000 },
  serverMetricOverrides: {
    m1: { qps: 9000, lps: 400, objects: 70000 },
    DC01: { qps: 40000, lps: 300, objects: 60000 },
  },
  errors: [
    { provider: 'ad', resource: 'corp.example.com', message: 'WinRM timeout on DC03' },
    { provider: 'aws', resource: 'us-west-2', message: 'AccessDenied on DescribeVpcs' },
  ],
  niosMicrosoftServers: {
    servers: [
      { fqdn: 'ms-dns-1.corp.example.com', address: '10.0.0.9', os: 'Windows Server 2022', adDomain: 'corp.example.com', dnsManaged: true, dhcpManaged: false, dhcpHosts: 0, readOnly: false },
    ],
    managedZones: 12,
  },
  niosMigrationFlags: {
    dhcpOptions: [
      { network: '10.0.0.0/24', optionNumber: 43, optionName: 'vendor-encapsulated-options', optionType: 'binary', flag: 'VALIDATION_NEEDED', member: 'member-1' },
    ],
    hostRoutes: [{ network: '10.0.0.0/24', member: 'member-1' }],
  },
  variantOverrides: { m1: 2 },
  microsoftAllocation: msAllocationFixture(),
  selectedMSScenario: 'dhcp-only',
};

/** Four-scenario allocation fixture, matching the shape ms-allocation-panel.test.tsx uses. */
function msAllocationFixture(): MicrosoftAllocationAPI {
  const category = (tokens: number) => ({
    category: 'DDI Objects', niosCount: 0, niosRate: 50, nativeCount: 0, nativeRate: 25,
    niosSubtotalNum: 0, nativeSubtotalNum: 0, subtotalDen: 1, tokens,
  });
  return {
    diagnostic: { available: true },
    baselineTokens: 1000,
    scenarios: [
      { id: 'none', dnsEnabled: false, dhcpEnabled: false, categories: [category(0), category(0), category(0)], effectiveTokens: 1000, deltaTokens: 0 },
      { id: 'dns-only', dnsEnabled: true, dhcpEnabled: false, categories: [category(40), category(10), category(5)], effectiveTokens: 1055, deltaTokens: 55 },
      { id: 'dhcp-only', dnsEnabled: false, dhcpEnabled: true, categories: [category(20), category(30), category(5)], effectiveTokens: 1055, deltaTokens: 55 },
      { id: 'both', dnsEnabled: true, dhcpEnabled: true, categories: [category(40), category(30), category(8)], effectiveTokens: 1078, deltaTokens: 78 },
    ],
    evidence: { relationshipRows: 900, duplicateRelationshipRows: 5, relationshipAnomalies: 1 },
  };
}

function fileFrom(snapshot: SessionSnapshot | string, name = 'session.json'): File {
  const text = typeof snapshot === 'string' ? snapshot : JSON.stringify(snapshot);
  return new File([text], name, { type: 'application/json' });
}

/** Health has to answer so the wizard is not in demo mode — demo skips the POST. */
function mockBackend() {
  return vi.spyOn(globalThis, 'fetch').mockImplementation(async (input: RequestInfo | URL) => {
    const url = String(typeof input === 'string' || input instanceof URL ? input : input.url);
    if (url.includes('/health')) {
      return new Response(JSON.stringify({ status: 'ok', version: '3.10.0', platform: 'darwin' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }
    if (url.includes('/export')) return new Response(xlsxBytes(), { status: 200 });
    return new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } });
  });
}

/** Renders the wizard, loads the given session files, and lands on the report step. */
async function renderReport(files: File[]) {
  render(<Wizard />);
  fireEvent.change(screen.getByTestId('session-file-input'), { target: { files } });
  for (const file of files) {
    await waitFor(() => expect(screen.getByText(file.name)).toBeTruthy());
  }
  fireEvent.click(screen.getByRole('button', { name: /^Next/ }));
  await waitFor(() => expect(screen.getByRole('button', { name: /download xlsx/i })).toBeTruthy());
}

/** Clicks Download XLSX and returns the report payload the wizard posted. */
async function clickExport(fetchMock: ReturnType<typeof mockBackend>): Promise<ReportExportPayload> {
  fireEvent.click(screen.getByRole('button', { name: /download xlsx/i }));
  await waitFor(() =>
    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith('/api/v1/export'))).toBe(true)
  );
  const call = fetchMock.mock.calls.find(([url]) => String(url).endsWith('/api/v1/export'))!;
  return JSON.parse((call[1] as RequestInit).body as string) as ReportExportPayload;
}

/**
 * Clicks Save Session and returns the JSON the browser would have downloaded.
 * saveSession hands its Blob to URL.createObjectURL, which the suite stubs.
 */
async function clickSaveSession(blobs: Blob[]): Promise<string> {
  const before = blobs.length;
  fireEvent.click(screen.getByRole('button', { name: /save session/i }));
  await waitFor(() => expect(blobs.length).toBeGreaterThan(before));
  return blobs[blobs.length - 1].text();
}

/** generatedAt is wall-clock and is expected to differ between two exports. */
const withoutClock = (p: ReportExportPayload) => ({ ...p, generatedAt: '' });

/**
 * One predicate per payload field, asserting the fixture drives it away from its
 * default. Typed as Record<keyof ReportExportPayload, ...> so a field added to
 * the payload later fails to compile until the fixture exercises it too.
 */
const exercisesEveryField: Record<keyof ReportExportPayload, (v: unknown) => boolean> = {
  generatedAt: (v) => typeof v === 'string' && v.length > 0,
  // More than one provider, and one of them namespace-translated ('microsoft' -> 'ad').
  selectedProviders: (v) => Array.isArray(v) && v.length > 1 && v.includes('ad'),
  findings: (v) => Array.isArray(v) && new Set(v.map((f) => f.provider)).size > 1 && v.some((f) => f.provider === 'ad'),
  totals: (v) => Object.values(v as Record<string, number>).every((n) => n > 0),
  providerTotals: (v) => Object.keys(v as object).length > 1,
  totalServerTokens: (v) => typeof v === 'number' && v > 0,
  reportingTokens: (v) => typeof v === 'number' && v > 0,
  niosServerMetrics: (v) => Array.isArray(v) && v.length > 0,
  adServerMetrics: (v) => Array.isArray(v) && v.length > 0,
  niosMigrationMap: (v) => Object.keys(v as object).length > 0,
  adMigrationMap: (v) => Object.keys(v as object).length > 0,
  // 0.20 is the default for both buffers, so it would prove nothing.
  growthBufferPct: (v) => typeof v === 'number' && v > 0 && v !== 0.2,
  serverGrowthBufferPct: (v) => typeof v === 'number' && v > 0 && v !== 0.2,
  variantOverrides: (v) => Object.keys(v as object).length > 0,
  errors: (v) => Array.isArray(v) && v.length > 0 && v.some((e) => e.provider === 'ad'),
  niosMicrosoftServers: (v) => (v as { servers: unknown[] }).servers.length > 0,
  niosMigrationFlags: (v) => {
    const f = v as { dhcpOptions: unknown[]; hostRoutes: unknown[] };
    return f.dhcpOptions.length > 0 && f.hostRoutes.length > 0;
  },
  microsoftAllocation: (v) => (v as { scenarios: unknown[] }).scenarios.length === 4,
  selectedMSScenario: (v) => v === 'dhcp-only',
};

describe('export round-trip', () => {
  let blobs: Blob[];

  beforeEach(() => {
    blobs = [];
    vi.stubGlobal('IntersectionObserver', MockIntersectionObserver);
    URL.createObjectURL = vi.fn((blob: Blob) => {
      blobs.push(blob);
      return 'blob:mock';
    });
    URL.revokeObjectURL = vi.fn();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('produces an identical export payload after save and reload', async () => {
    const fetchMock = mockBackend();

    await renderReport([fileFrom(FIXTURE)]);
    const original = await clickExport(fetchMock);

    // The fixture is the whole test: a field at its zero value cannot fail it.
    // Checked against the payload's OWN keys at runtime rather than by the type
    // alone — this repo has no tsc gate in CI, and an optional field added to
    // ReportExportPayload later would satisfy the Record without the fixture
    // touching it. Either direction of disagreement shows up in this diff.
    const exercisedKeys = Object.keys(original);
    expect(
      exercisedKeys.sort(),
      'payload fields and fixture predicates disagree — a field is unexercised or missing'
    ).toEqual(Object.keys(exercisesEveryField).sort());
    for (const field of exercisedKeys) {
      const value = (original as unknown as Record<string, unknown>)[field];
      expect(
        exercisesEveryField[field as keyof typeof exercisesEveryField](value),
        `fixture leaves ${field} at a default or empty value, so the round trip cannot fail on it`
      ).toBe(true);
    }

    const savedJson = await clickSaveSession(blobs);
    expect(validateSessionSchema(JSON.parse(savedJson)).valid).toBe(true);

    // A round trip is blind to anything symmetric across it: a field applySnapshot
    // drops on read is dropped on both legs, the payloads still match, and the data
    // is gone. So pin the write side against the fixture directly, before the
    // second leg can hide the loss.
    expect(JSON.parse(savedJson)).toEqual({
      ...FIXTURE,
      // Stamped at save time, not round-tripped.
      exportedAt: expect.any(String),
      // Not a drop: an effect in the wizard keeps the CDC destination at 40% of the
      // live log volume unless the user types over it (wizard.tsx:1400).
      reportingDestEvents: {
        ...FIXTURE.reportingDestEvents,
        ecosystem: Math.round(calcEstimator(FIXTURE.estimatorAnswers).monthlyLogVolume * 0.4),
      },
    });

    cleanup();
    fetchMock.mockClear();

    await renderReport([fileFrom(savedJson, 'reloaded.json')]);
    const reloaded = await clickExport(fetchMock);

    expect(withoutClock(reloaded)).toEqual(withoutClock(original));
  });

  it('carries both providers through a two-file merge', async () => {
    const fetchMock = mockBackend();

    const awsFile = fileFrom(
      {
        ...FIXTURE,
        selectedProviders: ['aws'],
        findings: FIXTURE.findings.filter((f) => f.provider === 'aws'),
        niosServerMetrics: [],
        adServerMetrics: [],
        niosMigrationMap: {},
        adMigrationMap: {},
        niosMicrosoftServers: undefined,
        niosMigrationFlags: undefined,
        variantOverrides: {},
        errors: FIXTURE.errors!.filter((e) => e.provider === 'aws'),
        serverMetricOverrides: {},
      },
      'aws.json'
    );
    const niosFile = fileFrom(
      {
        ...FIXTURE,
        selectedProviders: ['nios'],
        findings: FIXTURE.findings.filter((f) => f.provider === 'nios'),
        adServerMetrics: [],
        adMigrationMap: {},
        countOverrides: {},
        errors: [],
      },
      'nios.json'
    );

    await renderReport([awsFile, niosFile]);
    const payload = await clickExport(fetchMock);

    // Both files' findings survive the merge, under the backend namespace.
    expect(new Set(payload.findings.map((f) => f.provider))).toEqual(new Set(['aws', 'nios']));
    expect(payload.selectedProviders).toEqual(['aws', 'nios']);
    expect(payload.providerTotals.aws).toBeGreaterThan(0);
    expect(payload.providerTotals.nios).toBeGreaterThan(0);
    // Provider-scoped data comes whole from the file that owns the provider.
    expect(payload.niosServerMetrics?.map((m) => m.memberName)).toEqual(['member-1']);
    expect(payload.niosMigrationMap).toEqual({ 'member-1': 'nios-x' });
    expect(payload.niosMicrosoftServers?.managedZones).toBe(12);
    expect(payload.niosMigrationFlags?.hostRoutes).toHaveLength(1);
    expect(payload.variantOverrides).toEqual({ m1: 2 });
    // The aws-only error is provider-scoped too, and no AD data leaked in.
    expect(payload.errors.map((e) => e.provider)).toEqual(['aws']);
    expect(payload.adServerMetrics).toBeUndefined();
  });

  it('restores the switch selection made through the UI after a save-and-reload round trip', async () => {
    const fetchMock = mockBackend();

    // No selectedMSScenario yet, as if a scan just completed and nobody has
    // touched the switches -- the selection below happens live in the UI.
    const { selectedMSScenario: _unused, ...withoutSelection } = FIXTURE;
    await renderReport([fileFrom(withoutSelection)]);

    fireEvent.click(document.getElementById('ms-allocation-dhcp')!);

    const original = await clickExport(fetchMock);
    expect(original.microsoftAllocation).toEqual(FIXTURE.microsoftAllocation);
    expect(original.selectedMSScenario).toBe('dhcp-only');

    const savedJson = await clickSaveSession(blobs);
    const parsed = JSON.parse(savedJson) as SessionSnapshot;
    expect(parsed.microsoftAllocation).toEqual(FIXTURE.microsoftAllocation);
    expect(parsed.selectedMSScenario).toBe('dhcp-only');

    cleanup();
    fetchMock.mockClear();

    await renderReport([fileFrom(savedJson, 'reloaded.json')]);
    const reloaded = await clickExport(fetchMock);

    expect(withoutClock(reloaded)).toEqual(withoutClock(original));
  });

  it('loads a legacy v2 session with both switches off and no message', async () => {
    const fetchMock = mockBackend();

    const v2 = { ...FIXTURE, version: 2 } as Record<string, unknown>;
    delete v2.microsoftAllocation;
    delete v2.selectedMSScenario;

    await renderReport([fileFrom(v2 as unknown as SessionSnapshot, 'legacy.json')]);
    const payload = await clickExport(fetchMock);

    expect(payload.microsoftAllocation).toBeUndefined();
    expect(payload.selectedMSScenario).toBe('none');
    expect(screen.queryByText(/incompatible session version/i)).toBeNull();
  });
});
