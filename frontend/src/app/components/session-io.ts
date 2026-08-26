// session-io.ts -- Session export serialization
// Zero React/DOM imports; pure TypeScript so this module is testable with vitest without jsdom.

import type { ProviderType, FindingRow, NiosServerMetrics, ServerFormFactor } from './mock-data';
import type { EstimatorInputs } from './estimator-calc';
import { EstimatorDefaults } from './estimator-calc';
import type { ADServerMetricAPI, ProviderErrorAPI, NiosMicrosoftServersAPI, NiosMigrationFlagsAPI, MicrosoftAllocationAPI } from './api-client';
import { toFrontendProvider } from './mock-data';

/**
 * v2 added errors, niosMicrosoftServers, niosMigrationFlags and variantOverrides
 * so a re-imported session can reproduce its own xlsx workbook. v1 files still
 * load; their absent fields simply omit the sheets they would have fed. v3
 * added microsoftAllocation and selectedMSScenario so a re-imported session
 * reproduces its own Microsoft Allocation sheet; v1 and v2 files still load
 * with those fields simply absent.
 */
export const SESSION_FORMAT_VERSION = 3;

/**
 * The serialized shape written to disk.
 * Both migration maps are stored as plain objects because JSON.stringify
 * on a Map produces "{}" -- callers must pass Maps; exportSession converts them.
 */
export interface SessionSnapshot {
  version: number;
  exportedAt: string;
  toolVersion: string;
  selectedProviders: ProviderType[];
  findings: FindingRow[];
  countOverrides: Record<string, number>;
  niosMigrationMap: Record<string, ServerFormFactor>;
  adMigrationMap: Record<string, ServerFormFactor>;
  niosServerMetrics: NiosServerMetrics[];
  adServerMetrics: ADServerMetricAPI[];
  estimatorAnswers: EstimatorInputs;
  growthBufferPct: number;
  /**
   * Optional so v1 files written before this field existed stay valid.
   * restoreSession defaults it to 0.20 when absent (wizard.tsx:2001).
   */
  serverGrowthBufferPct?: number;
  reportingDestEnabled: Record<string, boolean>;
  reportingDestEvents: Record<string, number>;
  serverMetricOverrides?: Record<string, { qps?: number; lps?: number; objects?: number }>;
  /**
   * v2 fields. All optional so v1 files stay valid — an absent field simply
   * omits the workbook sheet it would have fed.
   */
  errors?: ProviderErrorAPI[];
  niosMicrosoftServers?: NiosMicrosoftServersAPI;
  niosMigrationFlags?: NiosMigrationFlagsAPI;
  /** Per-member appliance variant index, keyed by NIOS member ID. */
  variantOverrides?: Record<string, number>;
  /**
   * v3 fields. Both optional so v1 and v2 files stay valid — an absent
   * microsoftAllocation simply omits the Microsoft Allocation sheet, and an
   * absent selectedMSScenario defaults to 'none' on restore (D-09).
   */
  microsoftAllocation?: MicrosoftAllocationAPI;
  selectedMSScenario?: string;
}

/**
 * What callers (wizard.tsx) pass in.
 * Uses Map for the two migration maps (matching wizard state) so callers
 * pass their Maps directly without manual conversion.
 */
export interface SessionExportInput {
  selectedProviders: ProviderType[];
  findings: FindingRow[];
  countOverrides: Record<string, number>;
  niosMigrationMap: Map<string, ServerFormFactor>;
  adMigrationMap: Map<string, ServerFormFactor>;
  niosServerMetrics: NiosServerMetrics[];
  adServerMetrics: ADServerMetricAPI[];
  estimatorAnswers: EstimatorInputs;
  growthBufferPct: number;
  serverGrowthBufferPct: number;
  reportingDestEnabled: Record<string, boolean>;
  reportingDestEvents: Record<string, number>;
  serverMetricOverrides?: Record<string, { qps?: number; lps?: number; objects?: number }>;
  errors?: ProviderErrorAPI[];
  niosMicrosoftServers?: NiosMicrosoftServersAPI;
  niosMigrationFlags?: NiosMigrationFlagsAPI;
  variantOverrides?: Record<string, number>;
  microsoftAllocation?: MicrosoftAllocationAPI;
  selectedMSScenario?: string;
}

/**
 * Assemble a SessionSnapshot and return it as a pretty-printed JSON string.
 *
 * Maps are converted with Object.fromEntries so they round-trip correctly.
 */
export function exportSession(input: SessionExportInput, toolVersion: string): string {
  const snapshot: SessionSnapshot = {
    version: SESSION_FORMAT_VERSION,
    exportedAt: new Date().toISOString(),
    toolVersion,
    selectedProviders: input.selectedProviders,
    findings: input.findings,
    countOverrides: input.countOverrides,
    niosMigrationMap: Object.fromEntries(input.niosMigrationMap),
    adMigrationMap: Object.fromEntries(input.adMigrationMap),
    niosServerMetrics: input.niosServerMetrics,
    adServerMetrics: input.adServerMetrics,
    estimatorAnswers: input.estimatorAnswers,
    growthBufferPct: input.growthBufferPct,
    serverGrowthBufferPct: input.serverGrowthBufferPct,
    reportingDestEnabled: input.reportingDestEnabled,
    reportingDestEvents: input.reportingDestEvents,
    serverMetricOverrides: input.serverMetricOverrides,
    errors: input.errors,
    niosMicrosoftServers: input.niosMicrosoftServers,
    niosMigrationFlags: input.niosMigrationFlags,
    variantOverrides: input.variantOverrides,
    microsoftAllocation: input.microsoftAllocation,
    selectedMSScenario: input.selectedMSScenario,
  };
  return JSON.stringify(snapshot, null, 2);
}

// ---------------------------------------------------------------------------
// Import-side: validation and deserialization
// ---------------------------------------------------------------------------

/**
 * Result of a schema validation check.
 * `valid: true` means the data can safely be cast to SessionSnapshot.
 * `valid: false` carries a user-readable `error` string suitable for display in the UI.
 */
export interface ValidationResult {
  valid: boolean;
  error?: string;
}

/**
 * Validate that `data` conforms to the SessionSnapshot schema.
 *
 * Checks are ordered from coarse (type) to fine (field presence/type) so the
 * first failing check produces the most actionable error message.
 */
export function validateSessionSchema(data: unknown): ValidationResult {
  if (typeof data !== 'object' || data === null || Array.isArray(data)) {
    return { valid: false, error: 'Not a valid session file.' };
  }
  const d = data as Record<string, unknown>;
  if (typeof d.version !== 'number') {
    return { valid: false, error: 'Session file is missing a version field.' };
  }
  if (d.version > SESSION_FORMAT_VERSION) {
    return { valid: false, error: 'Incompatible session version. Please export a new session from the current tool version.' };
  }
  if (!Array.isArray(d.findings)) {
    return { valid: false, error: 'Session file is missing findings data.' };
  }
  if (!Array.isArray(d.selectedProviders)) {
    return { valid: false, error: 'Session file is missing provider list.' };
  }
  if (typeof d.estimatorAnswers !== 'object' || d.estimatorAnswers === null || Array.isArray(d.estimatorAnswers)) {
    return { valid: false, error: 'Session file is missing estimator configuration.' };
  }
  return { valid: true };
}

/**
 * Read a `.json` File, parse it, validate its schema, and return the
 * deserialized SessionSnapshot — or reject with a user-readable error.
 *
 * Uses the modern `file.text()` API (Node 20+ / all current browsers).
 * Does NOT use FileReader.
 */
export async function importSession(file: File): Promise<SessionSnapshot> {
  let parsed: unknown;
  try {
    const text = await file.text();
    parsed = JSON.parse(text);
  } catch {
    throw new Error('File is not valid JSON.');
  }
  const result = validateSessionSchema(parsed);
  if (!result.valid) {
    throw new Error(result.error);
  }
  return parsed as SessionSnapshot;
}

/**
 * Merge imported findings with live scan findings using a live-wins-per-provider strategy.
 *
 * Retained rows: imported findings whose provider is in `importedProviders` AND
 * whose provider is NOT in `liveProviders` (i.e., the provider was not re-scanned live).
 * All live findings are always included.
 *
 * @param importedFindings - All findings from the previously imported session.
 * @param importedProviders - Set of provider IDs that came from the imported session.
 * @param liveFindings - Findings produced by the most recent live scan.
 * @param liveProviders - Provider IDs that participated in the most recent live scan.
 * @returns Merged findings: retained imported rows first, then all live rows.
 */
export function mergeFindings(
  importedFindings: FindingRow[],
  importedProviders: Set<ProviderType>,
  liveFindings: FindingRow[],
  liveProviders: ProviderType[]
): FindingRow[] {
  const liveSet = new Set(liveProviders);
  const retained = importedFindings.filter(
    (f) => importedProviders.has(f.provider) && !liveSet.has(f.provider)
  );
  return [...retained, ...liveFindings];
}

// ---------------------------------------------------------------------------
// Session merge: fold N session files into one snapshot
// ---------------------------------------------------------------------------

/** A session file the user has loaded, paired with its file name for conflict messages. */
export interface LoadedSessionFile {
  fileName: string;
  snapshot: SessionSnapshot;
}

/** `provider` = two files claimed the same provider. `setting` = session-wide values disagreed. */
export type MergeConflictKind = 'provider' | 'setting';

/** A non-blocking record that two files disagreed. Rendered to the user as-is. */
export interface MergeConflict {
  kind: MergeConflictKind;
  /** Provider id for `provider` conflicts, snapshot field name for `setting` conflicts. */
  field: string;
  winnerFile: string;
  loserFile: string;
  /** User-readable sentence, rendered directly in the UI. */
  detail: string;
}

export interface MergeResult {
  snapshot: SessionSnapshot;
  conflicts: MergeConflict[];
}

/**
 * A valid snapshot with no data, using the same scalar defaults restoreSession
 * applies for an absent field. Returned for an empty file list so mergeSnapshots
 * is total and callers need no null branch.
 */
function emptySnapshot(): SessionSnapshot {
  return {
    version: SESSION_FORMAT_VERSION,
    exportedAt: new Date().toISOString(),
    toolVersion: '',
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
  };
}

/**
 * Fold N session files into one snapshot for unified cross-source sizing.
 *
 * Three rules, applied in order:
 *  1. Provider ownership — the first file listing a provider owns all data
 *     scoped to it.
 *  2. Keyed maps (countOverrides, serverMetricOverrides) merge shallow, first-wins.
 *  3. Session-wide scalars take the first file's value; divergence is reported.
 *
 * Merging a single file returns that file unchanged apart from `exportedAt`,
 * which is what keeps the single-session flow byte-identical to today.
 */
export function mergeSnapshots(files: LoadedSessionFile[]): MergeResult {
  const conflicts: MergeConflict[] = [];
  if (files.length === 0) {
    return { snapshot: emptySnapshot(), conflicts };
  }

  // Rule 1: the first file listing a provider owns everything scoped to it.
  const owner = new Map<ProviderType, LoadedSessionFile>();
  for (const file of files) {
    for (const provider of file.snapshot.selectedProviders) {
      const held = owner.get(provider);
      if (!held) {
        owner.set(provider, file);
      } else {
        conflicts.push({
          kind: 'provider',
          field: provider,
          winnerFile: held.fileName,
          loserFile: file.fileName,
          detail: `${provider} is provided by both ${held.fileName} and ${file.fileName}. ` +
            `Using ${held.fileName}; the ${provider} data in ${file.fileName} is ignored.`,
        });
      }
    }
  }

  // A finding for a provider no file listed still belongs to someone: give it to
  // the first file that contains it, so no rows are silently dropped.
  for (const file of files) {
    for (const row of file.snapshot.findings) {
      if (!owner.has(row.provider)) owner.set(row.provider, file);
    }
  }

  const ownedBy = (provider: ProviderType, file: LoadedSessionFile) =>
    owner.get(provider) === file;

  const selectedProviders: ProviderType[] = [];
  for (const file of files) {
    for (const provider of file.snapshot.selectedProviders) {
      if (!selectedProviders.includes(provider)) selectedProviders.push(provider);
    }
  }

  const findings = files.flatMap((file) =>
    file.snapshot.findings.filter((row) => ownedBy(row.provider, file))
  );

  // Rule 1: errors are provider-scoped, exactly like findings. ProviderErrorAPI.provider
  // carries the backend id (e.g. 'ad'), so it must go through toFrontendProvider before
  // the ownership check, which is keyed by the frontend ProviderType (e.g. 'microsoft').
  const errors = files.flatMap((file) =>
    (file.snapshot.errors ?? []).filter((e) => ownedBy(toFrontendProvider(e.provider), file))
  );

  // Provider-scoped collections come whole from their owner, or stay empty.
  const niosOwner = owner.get('nios');
  const adOwner = owner.get('microsoft');
  const estimatorOwner = owner.get('estimator');
  const first = files[0].snapshot;

  // Rule 2: keyed maps merge shallow, first-wins. Under Rule 1 these cannot
  // collide in practice, so a collision is resolved silently rather than reported.
  const countOverrides: Record<string, number> = {};
  const serverMetricOverrides: NonNullable<SessionSnapshot['serverMetricOverrides']> = {};
  const variantOverrides: NonNullable<SessionSnapshot['variantOverrides']> = {};
  for (const file of files) {
    for (const [key, value] of Object.entries(file.snapshot.countOverrides ?? {})) {
      if (!(key in countOverrides)) countOverrides[key] = value;
    }
    for (const [key, value] of Object.entries(file.snapshot.serverMetricOverrides ?? {})) {
      if (!(key in serverMetricOverrides)) serverMetricOverrides[key] = value;
    }
    for (const [key, value] of Object.entries(file.snapshot.variantOverrides ?? {})) {
      if (!(key in variantOverrides)) variantOverrides[key] = value;
    }
  }

  // Rule 3: session-wide values take the first file's, divergence is reported.
  // `toolVersion` is excluded: it's provenance metadata (different teams run
  // different builds essentially always), not a user-editable setting, so a
  // mismatch is noise rather than something actionable. It still comes
  // through via the `...first` spread below.
  const SESSION_WIDE = [
    'growthBufferPct',
    'serverGrowthBufferPct',
    'reportingDestEnabled',
    'reportingDestEvents',
  ] as const;
  // serverGrowthBufferPct is optional -- files saved before the field existed
  // omit it, which restoreSession treats as exactly the default 0.20. Compare
  // under that same default so an old file and a new 0.20 file agree.
  const normalize = (field: (typeof SESSION_WIDE)[number], value: unknown) =>
    field === 'serverGrowthBufferPct' ? value ?? 0.2 : value ?? null;
  for (const field of SESSION_WIDE) {
    const winner = JSON.stringify(normalize(field, files[0].snapshot[field]));
    for (const file of files.slice(1)) {
      if (JSON.stringify(normalize(field, file.snapshot[field])) === winner) continue;
      conflicts.push({
        kind: 'setting',
        field,
        winnerFile: files[0].fileName,
        loserFile: file.fileName,
        detail: `${field} differs between ${files[0].fileName} and ${file.fileName}. ` +
          `Using the value from ${files[0].fileName}; you can change it below.`,
      });
    }
  }

  const snapshot: SessionSnapshot = {
    ...first,
    version: SESSION_FORMAT_VERSION,
    exportedAt: new Date().toISOString(),
    selectedProviders,
    findings,
    // errors and variantOverrides are optional (v1 files omit them entirely); stay
    // undefined rather than [] / {} when no file supplied one, so a single-file merge
    // remains identical to its input.
    //
    // Safe unlike NiosServerMetrics/ADServerMetrics/NiosMicrosoftServers, where nil vs
    // empty is load-bearing (internal/exporter/exporter.go:170,214,227,240,266 gate
    // sheets on `!= nil`, so an empty-but-present slice still renders empty sheets).
    // Errors is gated on length, not nil (internal/exporter/exporter.go:201
    // `len(in.Errors) > 0`), and VariantOverrides is only ever read by key
    // (internal/exporter/resource_savings.go:85-89) — a nil Go map is a legal, safe
    // read target, so undefined and {} behave identically there too.
    errors: errors.length > 0 ? errors : undefined,
    countOverrides,
    serverMetricOverrides,
    variantOverrides: Object.keys(variantOverrides).length > 0 ? variantOverrides : undefined,
    niosMigrationMap: niosOwner ? niosOwner.snapshot.niosMigrationMap : {},
    niosServerMetrics: niosOwner ? niosOwner.snapshot.niosServerMetrics : [],
    niosMicrosoftServers: niosOwner?.snapshot.niosMicrosoftServers,
    niosMigrationFlags: niosOwner?.snapshot.niosMigrationFlags,
    microsoftAllocation: niosOwner?.snapshot.microsoftAllocation,
    // Scoped to the NIOS owner for the same reason microsoftAllocation is: the
    // selection only means anything against the scenario set it was made on. Left
    // to the `...first` spread it would take a non-NIOS file's value whenever NIOS
    // is not first, pairing a stale selection with someone else's scenario set.
    // Deliberately NOT added to SESSION_WIDE — scoping the value is independent of
    // divergence detection, and listing it there would raise a conflict banner on
    // every Microsoft/non-Microsoft merge. Left undefined rather than defaulted to
    // 'none' so a single-file merge stays identical to its input; restore applies
    // the 'none' default for an absent field (D-09).
    selectedMSScenario: niosOwner?.snapshot.selectedMSScenario,
    adMigrationMap: adOwner ? adOwner.snapshot.adMigrationMap : {},
    adServerMetrics: adOwner ? adOwner.snapshot.adServerMetrics : [],
    estimatorAnswers: (estimatorOwner ?? files[0]).snapshot.estimatorAnswers,
  };
  return { snapshot, conflicts };
}
