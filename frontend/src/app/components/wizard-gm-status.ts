import type { NiosServerMetrics, ServerFormFactor } from './nios-calc';

export interface GmStatus {
  label: string;
  serverTokens: 'none' | 'sized';
  formFactor: ServerFormFactor | null;
}

/**
 * Mode-aware GM/GMC status resolution (2026-07-08).
 *
 * Replaces the old `isInfraOnlyMember` all-zero-workload heuristic with the
 * migration-mode matrix:
 *   - Retained (not in migrationMap)           -> "Retained on NIOS", 0 tokens (retained always wins)
 *   - Migrated + management-only (no DNS/DHCP) -> "Replaced by Infoblox Portal", 0 tokens
 *   - Migrated + runs DNS/DHCP                 -> sized normally as a NIOS-X form factor
 * Non-GM/GMC members pass through unaffected.
 */
export function resolveGmStatus(
  m: NiosServerMetrics,
  migrationMap: Map<string, ServerFormFactor>,
): GmStatus {
  const isGm = m.role === 'GM' || m.role === 'GMC';
  const formFactor = migrationMap.get(m.memberName) ?? null;

  if (!isGm) {
    return { label: '', serverTokens: 'sized', formFactor };
  }

  const isMigrated = migrationMap.has(m.memberName);
  if (!isMigrated) {
    return { label: 'Retained on NIOS', serverTokens: 'none', formFactor: null };
  }
  if (!m.runsDnsDhcp) {
    return { label: 'Replaced by Infoblox Portal', serverTokens: 'none', formFactor: null };
  }
  return { label: '', serverTokens: 'sized', formFactor };
}
