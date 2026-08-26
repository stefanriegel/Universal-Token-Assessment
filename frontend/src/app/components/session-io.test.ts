import { describe, it, expect } from 'vitest';
import {
  exportSession,
  importSession,
  validateSessionSchema,
  mergeFindings,
  SESSION_FORMAT_VERSION,
  type SessionExportInput,
  type SessionSnapshot,
} from './session-io';
import { EstimatorDefaults } from './estimator-calc';
import type { ProviderType, FindingRow } from './mock-data';
import type { MicrosoftAllocationAPI, MSCategoryTokensAPI } from './api-client';

/** Minimal valid MicrosoftAllocationAPI fixture — shape only, values are arbitrary. */
function msAllocationFixture(): MicrosoftAllocationAPI {
  // Always three categories (DDI / Active IP / Managed Asset) — the wire type is a
  // 3-tuple mirroring Go's [3]MSCategoryTokens, so an empty array is not a legal payload.
  const cat = (category: string): MSCategoryTokensAPI => ({
    category,
    niosCount: 0,
    niosRate: 0,
    nativeCount: 0,
    nativeRate: 0,
    niosSubtotalNum: 0,
    nativeSubtotalNum: 0,
    subtotalDen: 1,
    tokens: 0,
  });
  const cats = (): [MSCategoryTokensAPI, MSCategoryTokensAPI, MSCategoryTokensAPI] => [
    cat('DDI Object'),
    cat('Active IP'),
    cat('Managed Asset'),
  ];
  return {
    diagnostic: { available: true },
    baselineTokens: 1000,
    scenarios: [
      { id: 'none', dnsEnabled: false, dhcpEnabled: false, categories: cats(), effectiveTokens: 1000, deltaTokens: 0 },
      { id: 'dhcp-only', dnsEnabled: false, dhcpEnabled: true, categories: cats(), effectiveTokens: 1055, deltaTokens: 55 },
    ],
    evidence: { relationshipRows: 10, duplicateRelationshipRows: 1, relationshipAnomalies: 0 },
  };
}

// Minimal base input shared across tests.
const baseInput: SessionExportInput = {
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

describe('exportSession', () => {
  it('returns a string that parses as valid JSON', () => {
    const result = exportSession({ ...baseInput }, '1.0.0');
    expect(() => JSON.parse(result)).not.toThrow();
  });

  it('parsed output has version === SESSION_FORMAT_VERSION', () => {
    const result = exportSession({ ...baseInput }, '1.0.0');
    const parsed = JSON.parse(result);
    expect(parsed.version).toBe(SESSION_FORMAT_VERSION);
  });

  it('niosMigrationMap serializes as a plain object with the correct value', () => {
    const map = new Map<string, 'nios-x' | 'nios-xaas'>();
    map.set('server-01', 'nios-x');
    const result = exportSession({ ...baseInput, niosMigrationMap: map }, '1.0.0');
    const parsed = JSON.parse(result);
    expect(typeof parsed.niosMigrationMap).toBe('object');
    expect(parsed.niosMigrationMap['server-01']).toBe('nios-x');
  });

  it('exportedAt is a non-empty ISO string that constructs a valid Date', () => {
    const result = exportSession({ ...baseInput }, '1.0.0');
    const parsed = JSON.parse(result);
    expect(typeof parsed.exportedAt).toBe('string');
    expect(parsed.exportedAt.length).toBeGreaterThan(0);
    expect(() => new Date(parsed.exportedAt)).not.toThrow();
    expect(new Date(parsed.exportedAt).toString()).not.toBe('Invalid Date');
  });

  it('toolVersion field equals the value passed in', () => {
    const result = exportSession({ ...baseInput }, 'v2.5.0');
    const parsed = JSON.parse(result);
    expect(parsed.toolVersion).toBe('v2.5.0');
  });

  it('empty state (empty arrays, empty Maps, empty objects) serializes without throwing', () => {
    expect(() => {
      const result = exportSession({ ...baseInput }, 'dev');
      JSON.parse(result);
    }).not.toThrow();
  });

  it('serverGrowthBufferPct survives the export round-trip', () => {
    const result = exportSession({ ...baseInput, serverGrowthBufferPct: 0.35 }, '1.0.0');
    const parsed = JSON.parse(result) as SessionSnapshot;
    expect(parsed.serverGrowthBufferPct).toBe(0.35);
  });

  it('round-trips the v2 fields that the workbook needs', () => {
    const input: SessionExportInput = {
      ...baseInput,
      errors: [{ provider: 'aws', resource: 'vpc', message: 'access denied' }],
      niosMicrosoftServers: {
        servers: [{ fqdn: 'dc1.example.com', address: '10.0.0.1', os: 'Windows', adDomain: 'example.com', dnsManaged: true, dhcpManaged: false, dhcpHosts: 0, readOnly: false }],
        managedZones: 12,
      },
      niosMigrationFlags: {
        dhcpOptions: [{ network: '10.0.0.0/24', optionNumber: 43, optionName: 'vendor', optionType: 'string', flag: 'CHECK_GUARDRAILS', member: 'gm1' }],
        hostRoutes: [],
      },
      variantOverrides: { 'member-1': 2 },
    };

    const json = exportSession(input, 'v3.10.0');
    const parsed = validateSessionSchema(JSON.parse(json));
    expect(parsed.valid).toBe(true);

    const snapshot = JSON.parse(json) as SessionSnapshot;
    expect(snapshot.errors).toEqual(input.errors);
    expect(snapshot.niosMicrosoftServers).toEqual(input.niosMicrosoftServers);
    expect(snapshot.niosMigrationFlags).toEqual(input.niosMigrationFlags);
    expect(snapshot.variantOverrides).toEqual({ 'member-1': 2 });
  });

  it('defaults the v2 fields when loading a v1 file', () => {
    const v1 = { ...snap(), version: 1 } as Record<string, unknown>;
    delete v1.errors;
    delete v1.variantOverrides;
    const result = validateSessionSchema(v1);
    expect(result.valid).toBe(true);
    const snapshot = v1 as unknown as SessionSnapshot;
    expect(snapshot.errors ?? []).toEqual([]);
    expect(snapshot.variantOverrides ?? {}).toEqual({});
  });

  it('round-trips the v3 microsoftAllocation and selectedMSScenario fields', () => {
    const allocation = msAllocationFixture();
    const input: SessionExportInput = {
      ...baseInput,
      microsoftAllocation: allocation,
      selectedMSScenario: 'dhcp-only',
    };

    const json = exportSession(input, 'v3.11.0');
    const parsed = validateSessionSchema(JSON.parse(json));
    expect(parsed.valid).toBe(true);

    const snapshot = JSON.parse(json) as SessionSnapshot;
    expect(snapshot.microsoftAllocation).toEqual(allocation);
    expect(snapshot.selectedMSScenario).toBe('dhcp-only');
  });

  it('omits microsoftAllocation and selectedMSScenario rather than writing them null', () => {
    const json = exportSession({ ...baseInput }, '1.0.0');
    const parsed = JSON.parse(json) as Record<string, unknown>;
    expect('microsoftAllocation' in parsed).toBe(false);
    expect('selectedMSScenario' in parsed).toBe(false);
  });

  it('defaults the v3 fields when loading a v2 file', () => {
    const v2 = { ...snap(), version: 2 } as Record<string, unknown>;
    delete v2.microsoftAllocation;
    delete v2.selectedMSScenario;
    const result = validateSessionSchema(v2);
    expect(result.valid).toBe(true);
    const snapshot = v2 as unknown as SessionSnapshot;
    expect(snapshot.microsoftAllocation).toBeUndefined();
    expect(snapshot.selectedMSScenario ?? 'none').toBe('none');
  });
});

// ---------------------------------------------------------------------------
// validateSessionSchema
// ---------------------------------------------------------------------------

describe('validateSessionSchema', () => {
  // Helper: build a valid snapshot object from exportSession output.
  const validSnapshot = (): SessionSnapshot =>
    JSON.parse(exportSession({ ...baseInput }, '1.0.0')) as SessionSnapshot;

  it('returns { valid: true } for a well-formed snapshot', () => {
    const result = validateSessionSchema(validSnapshot());
    expect(result).toEqual({ valid: true });
  });

  it('returns { valid: false } with "Not a valid session file." for null', () => {
    const result = validateSessionSchema(null);
    expect(result.valid).toBe(false);
    expect(result.error).toBe('Not a valid session file.');
  });

  it('returns { valid: false } with "Not a valid session file." for an array', () => {
    const result = validateSessionSchema([1, 2, 3]);
    expect(result.valid).toBe(false);
    expect(result.error).toBe('Not a valid session file.');
  });

  it('returns { valid: false } with missing-version error when version field is absent', () => {
    const snapshot = validSnapshot();
    const { version: _removed, ...withoutVersion } = snapshot;
    const result = validateSessionSchema(withoutVersion);
    expect(result.valid).toBe(false);
    expect(result.error).toBe('Session file is missing a version field.');
  });

  it('returns { valid: false } with incompatibility message for a wrong version number', () => {
    const snapshot = { ...validSnapshot(), version: SESSION_FORMAT_VERSION + 1 };
    const result = validateSessionSchema(snapshot);
    expect(result.valid).toBe(false);
    expect(result.error).toContain('Incompatible session version');
  });

  it('returns { valid: false } with missing-findings error when findings is not an array', () => {
    const snapshot = { ...validSnapshot(), findings: 'oops' };
    const result = validateSessionSchema(snapshot);
    expect(result.valid).toBe(false);
    expect(result.error).toBe('Session file is missing findings data.');
  });

  it('returns { valid: false } with missing-providers error when selectedProviders is not an array', () => {
    const snapshot = { ...validSnapshot(), selectedProviders: null };
    const result = validateSessionSchema(snapshot);
    expect(result.valid).toBe(false);
    expect(result.error).toBe('Session file is missing provider list.');
  });

  it('returns { valid: false } with missing-estimator error when estimatorAnswers is not an object', () => {
    const snapshot = { ...validSnapshot(), estimatorAnswers: 'bad' };
    const result = validateSessionSchema(snapshot);
    expect(result.valid).toBe(false);
    expect(result.error).toBe('Session file is missing estimator configuration.');
  });
});

// ---------------------------------------------------------------------------
// importSession
// ---------------------------------------------------------------------------

describe('importSession', () => {
  // Node 20+ ships File globally; no jsdom needed.
  const makeFile = (content: string) =>
    new File([content], 'test.json', { type: 'application/json' });

  it('resolves with a valid SessionSnapshot for a round-tripped exportSession output', async () => {
    const jsonString = exportSession({ ...baseInput }, '1.0.0');
    const file = makeFile(jsonString);
    const result = await importSession(file);
    expect(result.version).toBe(SESSION_FORMAT_VERSION);
    expect(Array.isArray(result.findings)).toBe(true);
    expect(Array.isArray(result.selectedProviders)).toBe(true);
  });

  it('rejects with "File is not valid JSON." for non-JSON content', async () => {
    const file = makeFile('this is not JSON {{ broken');
    await expect(importSession(file)).rejects.toThrow('File is not valid JSON.');
  });

  it('round-trips microsoftAllocation and selectedMSScenario through import', async () => {
    const allocation = msAllocationFixture();
    const jsonString = exportSession(
      { ...baseInput, microsoftAllocation: allocation, selectedMSScenario: 'dhcp-only' },
      '1.0.0'
    );
    const file = makeFile(jsonString);
    const result = await importSession(file);
    expect(result.microsoftAllocation).toEqual(allocation);
    expect(result.selectedMSScenario).toBe('dhcp-only');
  });
});

// ---------------------------------------------------------------------------
// mergeFindings
// ---------------------------------------------------------------------------

describe('mergeFindings', () => {
  // Minimal fixture factory — only provider and item matter for merge logic.
  const row = (provider: ProviderType, item = 'test-item'): FindingRow => ({
    provider,
    source: 'test-source',
    region: '',
    category: 'DDI Object',
    item,
    count: 1,
    tokensPerUnit: 1,
    managementTokens: 1,
  });

  it('retains imported provider rows and appends live rows from a different provider', () => {
    const imported = [row('nios', 'nios-item')];
    const live = [row('aws', 'aws-item')];
    const result = mergeFindings(imported, new Set(['nios']), live, ['aws']);
    expect(result).toHaveLength(2);
    expect(result.some(r => r.provider === 'nios' && r.item === 'nios-item')).toBe(true);
    expect(result.some(r => r.provider === 'aws' && r.item === 'aws-item')).toBe(true);
  });

  it('replaces imported findings when the same provider is live-scanned', () => {
    const imported = [row('nios', 'old-item')];
    const live = [row('nios', 'new-item')];
    const result = mergeFindings(imported, new Set(['nios']), live, ['nios']);
    expect(result).toHaveLength(1);
    expect(result[0].item).toBe('new-item');
  });

  it('returns liveFindings unchanged when importedProviders is empty', () => {
    const live = [row('aws', 'aws-item'), row('gcp', 'gcp-item')];
    const result = mergeFindings([], new Set(), live, ['aws', 'gcp']);
    expect(result).toEqual(live);
  });

  it('retains non-scanned imported providers when only one imported provider is re-scanned', () => {
    const imported = [row('nios', 'nios-item'), row('azure', 'azure-item')];
    const live = [row('nios', 'nios-fresh')];
    const result = mergeFindings(imported, new Set(['nios', 'azure']), live, ['nios']);
    expect(result).toHaveLength(2);
    expect(result.some(r => r.provider === 'azure' && r.item === 'azure-item')).toBe(true);
    expect(result.some(r => r.provider === 'nios' && r.item === 'nios-fresh')).toBe(true);
    expect(result.every(r => r.item !== 'nios-item')).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// mergeSnapshots
// ---------------------------------------------------------------------------

import { mergeSnapshots, type LoadedSessionFile } from './session-io';

// Helper: a full valid snapshot, overridable per test.
function snap(over: Partial<SessionSnapshot> = {}): SessionSnapshot {
  return {
    version: SESSION_FORMAT_VERSION,
    exportedAt: '2026-01-01T00:00:00.000Z',
    toolVersion: '1.0.0',
    selectedProviders: [],
    findings: [],
    countOverrides: {},
    niosMigrationMap: {},
    adMigrationMap: {},
    niosServerMetrics: [],
    adServerMetrics: [],
    estimatorAnswers: { ...EstimatorDefaults },
    growthBufferPct: 0.2,
    serverGrowthBufferPct: 0.2,
    reportingDestEnabled: {},
    reportingDestEvents: {},
    serverMetricOverrides: {},
    ...over,
  };
}

function loaded(fileName: string, over: Partial<SessionSnapshot> = {}): LoadedSessionFile {
  return { fileName, snapshot: snap(over) };
}

describe('mergeSnapshots', () => {
  it('returns a valid empty snapshot and no conflicts for an empty list', () => {
    const { snapshot, conflicts } = mergeSnapshots([]);
    expect(conflicts).toEqual([]);
    expect(snapshot.version).toBe(SESSION_FORMAT_VERSION);
    expect(snapshot.selectedProviders).toEqual([]);
    expect(snapshot.findings).toEqual([]);
    expect(snapshot.serverGrowthBufferPct).toBe(0.2);
  });

  it('a single file merges to itself apart from exportedAt', () => {
    const one = loaded('file-a.json', {
      selectedProviders: ['aws'],
      findings: [{ provider: 'aws', source: 'acct-1', region: 'eu-central-1',
        category: 'DDI Object', item: 'Hosted Zones', count: 4,
        tokensPerUnit: 25, managementTokens: 1 }],
      growthBufferPct: 0.45,
    });
    const { snapshot, conflicts } = mergeSnapshots([one]);
    expect(conflicts).toEqual([]);
    const { exportedAt: _a, ...mergedRest } = snapshot;
    const { exportedAt: _b, ...inputRest } = one.snapshot;
    expect(mergedRest).toEqual(inputRest);
  });

  const awsRow: FindingRow = { provider: 'aws', source: 'acct-1', region: 'eu-central-1',
    category: 'DDI Object', item: 'Hosted Zones', count: 4, tokensPerUnit: 25, managementTokens: 1 };
  const azureRow: FindingRow = { provider: 'azure', source: 'sub-1', region: 'westeurope',
    category: 'Active IP', item: 'Private IPs', count: 130, tokensPerUnit: 13, managementTokens: 10 };

  it('unions selectedProviders in first-seen order', () => {
    const { snapshot } = mergeSnapshots([
      loaded('file-a.json', { selectedProviders: ['aws'] }),
      loaded('file-b.json', { selectedProviders: ['azure', 'nios'] }),
    ]);
    expect(snapshot.selectedProviders).toEqual(['aws', 'azure', 'nios']);
  });

  it('concatenates findings across files owning different providers', () => {
    const { snapshot, conflicts } = mergeSnapshots([
      loaded('file-a.json', { selectedProviders: ['aws'], findings: [awsRow] }),
      loaded('file-b.json', { selectedProviders: ['azure'], findings: [azureRow] }),
    ]);
    expect(conflicts).toEqual([]);
    expect(snapshot.findings).toEqual([awsRow, azureRow]);
  });

  it('first file wins a provider collision and emits one provider conflict', () => {
    const otherAwsRow: FindingRow = { ...awsRow, source: 'acct-2', count: 99 };
    const { snapshot, conflicts } = mergeSnapshots([
      loaded('file-a.json', { selectedProviders: ['aws'], findings: [awsRow] }),
      loaded('file-b.json', { selectedProviders: ['aws'], findings: [otherAwsRow] }),
    ]);
    expect(snapshot.findings).toEqual([awsRow]);
    expect(conflicts).toHaveLength(1);
    expect(conflicts[0].kind).toBe('provider');
    expect(conflicts[0].field).toBe('aws');
    expect(conflicts[0].winnerFile).toBe('file-a.json');
    expect(conflicts[0].loserFile).toBe('file-b.json');
  });

  it('takes nios-scoped data from the nios owner only', () => {
    const { snapshot } = mergeSnapshots([
      loaded('file-a.json', { selectedProviders: ['aws'], niosMigrationMap: { 'ignored-01': 'nios-x' } }),
      loaded('file-b.json', {
        selectedProviders: ['nios'],
        niosMigrationMap: { 'server-01': 'nios-x' },
        niosServerMetrics: [{ memberId: 'm1', memberName: 'server-01' } as never],
      }),
    ]);
    expect(snapshot.niosMigrationMap).toEqual({ 'server-01': 'nios-x' });
    expect(snapshot.niosServerMetrics).toHaveLength(1);
  });

  it('takes selectedMSScenario from the nios owner even when it is not the first file', () => {
    const { snapshot } = mergeSnapshots([
      loaded('file-a.json', { selectedProviders: ['aws'], selectedMSScenario: 'both' }),
      loaded('file-b.json', {
        selectedProviders: ['nios'],
        selectedMSScenario: 'dhcp-only',
        microsoftAllocation: { scenarios: [] } as never,
      }),
    ]);
    // Must pair with the scenario set it was made on, not the first file's value.
    expect(snapshot.selectedMSScenario).toBe('dhcp-only');
  });

  it('drops a non-nios file\'s selectedMSScenario when the nios owner has none', () => {
    const { snapshot } = mergeSnapshots([
      loaded('file-a.json', { selectedProviders: ['aws'], selectedMSScenario: 'both' }),
      loaded('file-b.json', { selectedProviders: ['nios'] }),
    ]);
    // Left undefined, not 'none' — restore applies that default (D-09), and
    // defaulting here would break the single-file merge-to-itself invariant.
    expect(snapshot.selectedMSScenario).toBeUndefined();
  });

  it('takes AD-scoped data from the microsoft owner, not a file that merely has it', () => {
    const { snapshot } = mergeSnapshots([
      loaded('file-a.json', { selectedProviders: ['aws'], adMigrationMap: { 'ignored-dc': 'nios-x' } }),
      loaded('file-b.json', {
        selectedProviders: ['microsoft'],
        adMigrationMap: { 'dc-01': 'nios-x' },
        adServerMetrics: [{ hostname: 'dc-01' } as never],
      }),
    ]);
    expect(snapshot.adMigrationMap).toEqual({ 'dc-01': 'nios-x' });
    expect(snapshot.adServerMetrics).toHaveLength(1);
  });

  it('takes estimatorAnswers from the estimator owner even when it is not the first file', () => {
    const answers = { ...EstimatorDefaults, activeIPs: 4242 };
    const { snapshot } = mergeSnapshots([
      loaded('file-a.json', { selectedProviders: ['aws'] }),
      loaded('file-b.json', { selectedProviders: ['estimator'], estimatorAnswers: answers }),
    ]);
    expect(snapshot.estimatorAnswers.activeIPs).toBe(4242);
  });

  it('keeps findings whose provider no file listed, from the first file containing them', () => {
    const { snapshot } = mergeSnapshots([
      loaded('file-a.json', { selectedProviders: [], findings: [awsRow] }),
      loaded('file-b.json', { selectedProviders: [], findings: [{ ...awsRow, count: 77 }] }),
    ]);
    expect(snapshot.findings).toEqual([awsRow]);
  });

  it('shallow-merges keyed maps with first-wins and no conflict', () => {
    const { snapshot, conflicts } = mergeSnapshots([
      loaded('file-a.json', {
        selectedProviders: ['aws'],
        countOverrides: { 'aws::acct-1::Hosted Zones': 5 },
        serverMetricOverrides: { 'm1': { qps: 100 } },
      }),
      loaded('file-b.json', {
        selectedProviders: ['azure'],
        countOverrides: { 'azure::sub-1::Private IPs': 9, 'aws::acct-1::Hosted Zones': 999 },
        serverMetricOverrides: { 'm2': { lps: 50 }, 'm1': { qps: 999 } },
      }),
    ]);
    expect(snapshot.countOverrides).toEqual({
      'aws::acct-1::Hosted Zones': 5,
      'azure::sub-1::Private IPs': 9,
    });
    expect(snapshot.serverMetricOverrides).toEqual({ 'm1': { qps: 100 }, 'm2': { lps: 50 } });
    expect(conflicts).toEqual([]);
  });

  it('keeps the first file growth buffers and reports the divergence', () => {
    const { snapshot, conflicts } = mergeSnapshots([
      loaded('file-a.json', { selectedProviders: ['aws'], growthBufferPct: 0.2, serverGrowthBufferPct: 0.2 }),
      loaded('file-b.json', { selectedProviders: ['azure'], growthBufferPct: 0.5, serverGrowthBufferPct: 0.6 }),
    ]);
    expect(snapshot.growthBufferPct).toBe(0.2);
    expect(snapshot.serverGrowthBufferPct).toBe(0.2);
    const fields = conflicts.filter((c) => c.kind === 'setting').map((c) => c.field);
    expect(fields).toContain('growthBufferPct');
    expect(fields).toContain('serverGrowthBufferPct');
  });

  it('reports diverging reporting destination config but keeps the first', () => {
    const { snapshot, conflicts } = mergeSnapshots([
      loaded('file-a.json', { selectedProviders: ['aws'], reportingDestEvents: { syslog: 10 } }),
      loaded('file-b.json', { selectedProviders: ['azure'], reportingDestEvents: { syslog: 20 } }),
    ]);
    expect(snapshot.reportingDestEvents).toEqual({ syslog: 10 });
    expect(conflicts.some((c) => c.kind === 'setting' && c.field === 'reportingDestEvents')).toBe(true);
  });

  it('does not report a setting conflict for differing toolVersion, and keeps the first file value', () => {
    const { snapshot, conflicts } = mergeSnapshots([
      loaded('file-a.json', { selectedProviders: ['aws'], toolVersion: '1.0.0' }),
      loaded('file-b.json', { selectedProviders: ['azure'], toolVersion: '2.0.0' }),
    ]);
    expect(snapshot.toolVersion).toBe('1.0.0');
    expect(conflicts.some((c) => c.kind === 'setting' && c.field === 'toolVersion')).toBe(false);
  });

  it('treats an absent serverGrowthBufferPct as the 0.20 default, so an old file and a new 0.20 file agree', () => {
    const oldFile = loaded('old.json', { selectedProviders: ['aws'] });
    delete (oldFile.snapshot as Partial<SessionSnapshot>).serverGrowthBufferPct;
    const { snapshot, conflicts } = mergeSnapshots([
      oldFile,
      loaded('new.json', { selectedProviders: ['azure'], serverGrowthBufferPct: 0.2 }),
    ]);
    expect(snapshot.serverGrowthBufferPct).toBeUndefined();
    expect(conflicts.some((c) => c.kind === 'setting' && c.field === 'serverGrowthBufferPct')).toBe(false);
  });

  it('emits no setting conflict when session-wide values agree', () => {
    const { conflicts } = mergeSnapshots([
      loaded('file-a.json', { selectedProviders: ['aws'], growthBufferPct: 0.2 }),
      loaded('file-b.json', { selectedProviders: ['azure'], growthBufferPct: 0.2 }),
    ]);
    expect(conflicts.filter((c) => c.kind === 'setting')).toEqual([]);
  });

  // -------------------------------------------------------------------------
  // v2 fields
  // -------------------------------------------------------------------------

  it('takes provider-scoped v2 fields from the owning file (Rule 1)', () => {
    const awsFile = loaded('aws.json', {
      selectedProviders: ['aws'],
      errors: [{ provider: 'aws', resource: 'vpc', message: 'aws failed' }],
    });
    const niosFile = loaded('nios.json', {
      selectedProviders: ['nios'],
      errors: [{ provider: 'nios', resource: 'grid', message: 'nios failed' }],
      niosMicrosoftServers: { servers: [], managedZones: 5 },
      niosMigrationFlags: { dhcpOptions: [], hostRoutes: [{ network: '10.0.0.0/24', member: 'gm1' }] },
    });

    const { snapshot, conflicts } = mergeSnapshots([awsFile, niosFile]);

    expect(conflicts).toEqual([]);
    expect(snapshot.errors).toHaveLength(2);
    expect(snapshot.errors?.map((e) => e.provider).sort()).toEqual(['aws', 'nios']);
    expect(snapshot.niosMicrosoftServers?.managedZones).toBe(5);
    expect(snapshot.niosMigrationFlags?.hostRoutes).toHaveLength(1);
  });

  // Regression: ProviderErrorAPI.provider carries the BACKEND id ('ad'), not the
  // frontend ProviderType ('microsoft'). ownedBy() is keyed by ProviderType, so an
  // error must be converted with toFrontendProvider before the ownership check —
  // comparing the raw backend id against the 'microsoft' owner would silently drop it.
  it('keeps an AD error (backend provider id "ad") owned by the microsoft file', () => {
    const adFile = loaded('ad.json', {
      selectedProviders: ['microsoft'],
      errors: [{ provider: 'ad', resource: 'dc-01', message: 'ad failed' }],
    });

    const { snapshot } = mergeSnapshots([adFile]);

    expect(snapshot.errors).toEqual([{ provider: 'ad', resource: 'dc-01', message: 'ad failed' }]);
  });

  it('drops errors belonging to a provider this file does not own', () => {
    const first = loaded('a.json', {
      selectedProviders: ['aws'],
      errors: [{ provider: 'aws', resource: 'vpc', message: 'from a' }],
    });
    const second = loaded('b.json', {
      selectedProviders: ['aws'],
      errors: [{ provider: 'aws', resource: 'vpc', message: 'from b' }],
    });

    const { snapshot } = mergeSnapshots([first, second]);
    expect(snapshot.errors).toEqual([{ provider: 'aws', resource: 'vpc', message: 'from a' }]);
  });

  // Pins that collapsing empty errors/variantOverrides to undefined is safe: a live
  // scan's exportSession always writes errors: [] and variantOverrides: {} (never
  // omits them), and the Go exporter treats that identically to undefined — Errors
  // is gated on len() > 0 (internal/exporter/exporter.go:201), and VariantOverrides
  // is only read by key, which is a safe no-op on a nil Go map
  // (internal/exporter/resource_savings.go:85-89). Unlike NiosServerMetrics /
  // ADServerMetrics / NiosMicrosoftServers, where nil vs [] IS load-bearing
  // (exporter.go:170,214,227,240,266 gate sheets on `!= nil`) — this test does not
  // apply to those fields, which mergeSnapshots correctly leaves as-is.
  it('collapses explicit empty errors/variantOverrides the same as omitted ones', () => {
    const explicit = loaded('a.json', { selectedProviders: ['aws'], errors: [], variantOverrides: {} });
    const omitted = loaded('b.json', { selectedProviders: ['aws'] });
    delete (omitted.snapshot as Partial<SessionSnapshot>).errors;
    delete (omitted.snapshot as Partial<SessionSnapshot>).variantOverrides;

    const explicitResult = mergeSnapshots([explicit]);
    const omittedResult = mergeSnapshots([omitted]);

    expect(explicitResult.snapshot.errors).toBeUndefined();
    expect(explicitResult.snapshot.variantOverrides).toBeUndefined();
    expect(explicitResult.snapshot.errors).toEqual(omittedResult.snapshot.errors);
    expect(explicitResult.snapshot.variantOverrides).toEqual(omittedResult.snapshot.variantOverrides);
  });

  it('merges variantOverrides shallow, first-wins (Rule 2)', () => {
    const first = loaded('a.json', { selectedProviders: ['nios'], variantOverrides: { m1: 1 } });
    const second = loaded('b.json', { selectedProviders: ['aws'], variantOverrides: { m1: 9, m2: 2 } });

    const { snapshot } = mergeSnapshots([first, second]);
    expect(snapshot.variantOverrides).toEqual({ m1: 1, m2: 2 });
  });

  // -------------------------------------------------------------------------
  // v3 fields
  // -------------------------------------------------------------------------

  it('returns microsoftAllocation and selectedMSScenario unchanged for a single file', () => {
    const allocation = msAllocationFixture();
    const one = loaded('a.json', {
      selectedProviders: ['nios'],
      microsoftAllocation: allocation,
      selectedMSScenario: 'dhcp-only',
    });
    const { snapshot } = mergeSnapshots([one]);
    expect(snapshot.microsoftAllocation).toEqual(allocation);
    expect(snapshot.selectedMSScenario).toBe('dhcp-only');
  });

  it('takes microsoftAllocation from the file that owns the nios provider (Rule 1)', () => {
    const allocation = msAllocationFixture();
    const { snapshot } = mergeSnapshots([
      loaded('file-a.json', { selectedProviders: ['aws'] }),
      loaded('file-b.json', { selectedProviders: ['nios'], microsoftAllocation: allocation }),
    ]);
    expect(snapshot.microsoftAllocation).toEqual(allocation);
  });

  it('leaves microsoftAllocation undefined when no file carries it', () => {
    const { snapshot } = mergeSnapshots([
      loaded('file-a.json', { selectedProviders: ['aws'] }),
      loaded('file-b.json', { selectedProviders: ['nios'] }),
    ]);
    expect(snapshot.microsoftAllocation).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// Session format version
// ---------------------------------------------------------------------------

describe('session format version', () => {
  it('is 3', () => {
    expect(SESSION_FORMAT_VERSION).toBe(3);
  });

  it('accepts a v1 file so existing sessions keep loading', () => {
    const v1 = { ...snap(), version: 1 };
    expect(validateSessionSchema(v1).valid).toBe(true);
  });

  it('accepts a v2 file so existing sessions keep loading', () => {
    const v2 = { ...snap(), version: 2 };
    expect(validateSessionSchema(v2).valid).toBe(true);
  });

  it('rejects a file from a newer tool version', () => {
    const v4 = { ...snap(), version: 4 };
    const result = validateSessionSchema(v4);
    expect(result.valid).toBe(false);
    expect(result.error).toMatch(/Incompatible session version/);
  });
});
