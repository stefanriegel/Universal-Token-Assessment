// ms-allocation-panel.tsx — the Microsoft allocation contract surfaced on the
// scan-results screen (MSALLOC-01, MSTOK-04). Renders two switches that
// re-index into four backend-precomputed scenarios (D-01/D-02), a collapsed
// raw-evidence section (D-06), and the two degraded states
// (D-07 absent, D-08 unavailable). Every helper below is pure/React-free so it
// can be exercised directly in tests without rendering.
import { ChevronDown, ChevronRight } from 'lucide-react';

import { Switch } from '../ui/switch';
import { Label } from '../ui/label';
import type { MicrosoftAllocationAPI } from '../api-client';

/** Selects one of the four precomputed scenario ids from the two switch states. */
export function selectMSScenario(dnsEnabled: boolean, dhcpEnabled: boolean): string {
  if (dnsEnabled && dhcpEnabled) return 'both';
  if (dnsEnabled) return 'dns-only';
  if (dhcpEnabled) return 'dhcp-only';
  return 'none';
}

/**
 * Returns the backend's own pre-computed delta between the selected scenario
 * and the 'none' baseline. This is a difference of two integers the backend
 * already produced — no token rate, no ceiling division and no re-derivation
 * of token math happens here (D-02, T-07-08).
 */
export function msAllocationDelta(
  allocation: MicrosoftAllocationAPI | null,
  selected: string,
): { ddi: number; ip: number; asset: number; total: number } {
  const zero = { ddi: 0, ip: 0, asset: 0, total: 0 };
  if (!allocation) return zero;
  if (!allocation.diagnostic.available) return zero;
  if (selected === 'none') return zero;
  if (allocation.scenarios.length !== 4) return zero;

  const selectedScenario = allocation.scenarios.find((s) => s.id === selected);
  const noneScenario = allocation.scenarios.find((s) => s.id === 'none');
  if (!selectedScenario || !noneScenario) return zero;

  return {
    ddi: (selectedScenario.categories[0]?.tokens ?? 0) - (noneScenario.categories[0]?.tokens ?? 0),
    ip: (selectedScenario.categories[1]?.tokens ?? 0) - (noneScenario.categories[1]?.tokens ?? 0),
    asset: (selectedScenario.categories[2]?.tokens ?? 0) - (noneScenario.categories[2]?.tokens ?? 0),
    total: selectedScenario.deltaTokens,
  };
}

export interface MSAllocationPanelProps {
  allocation: MicrosoftAllocationAPI | null;
  selected: string;
  onSelect: (id: string) => void;
  /** Disable only scenario selection while preserving access to raw evidence. */
  selectionDisabled?: boolean;
  /** Element describing why scenario selection is disabled. */
  selectionDescriptionId?: string;
  evidenceOpen: boolean;
  setEvidenceOpen: (v: boolean | ((prev: boolean) => boolean)) => void;
}

export function MSAllocationPanel({
  allocation,
  selected,
  onSelect,
  selectionDisabled = false,
  selectionDescriptionId,
  evidenceOpen,
  setEvidenceOpen,
}: MSAllocationPanelProps) {
  if (!allocation || allocation.diagnostic.code === 'MS_ALLOCATION_ABSENT') {
    return null;
  }

  const dnsEnabled = selected === 'dns-only' || selected === 'both';
  const dhcpEnabled = selected === 'dhcp-only' || selected === 'both';

  const handleChange = (nextDns: boolean, nextDhcp: boolean) => {
    onSelect(selectMSScenario(nextDns, nextDhcp));
  };

  const switchRow = (
    <div className="flex flex-wrap items-center gap-6">
      <div className="flex items-center gap-2">
        <Switch
          id="ms-allocation-dns"
          checked={dnsEnabled}
          disabled={selectionDisabled || !allocation.diagnostic.available}
          aria-describedby={selectionDisabled ? selectionDescriptionId : undefined}
          onCheckedChange={(checked) => handleChange(checked, dhcpEnabled)}
        />
        <Label htmlFor="ms-allocation-dns" className="text-[13px]">
          Manage Microsoft DNS with Universal DDI
        </Label>
      </div>
      <div className="flex items-center gap-2">
        <Switch
          id="ms-allocation-dhcp"
          checked={dhcpEnabled}
          disabled={selectionDisabled || !allocation.diagnostic.available}
          aria-describedby={selectionDisabled ? selectionDescriptionId : undefined}
          onCheckedChange={(checked) => handleChange(dnsEnabled, checked)}
        />
        <Label htmlFor="ms-allocation-dhcp" className="text-[13px]">
          Manage Microsoft DHCP with Universal DDI
        </Label>
      </div>
    </div>
  );

  if (!allocation.diagnostic.available) {
    return (
      <div className="mt-2 pt-2 border-t border-[var(--border)] space-y-3">
        {switchRow}
        <div className="bg-blue-50/20 border border-blue-100 rounded px-3 py-2 text-[12px] text-[var(--muted-foreground)]">
          {allocation.diagnostic.message}
        </div>
      </div>
    );
  }

  const delta = msAllocationDelta(allocation, selected);
  const evidence = allocation.evidence;

  return (
    <div className="mt-2 pt-2 border-t border-[var(--border)] space-y-3">
      {switchRow}

      {selected !== 'none' && (
        <div className="text-[13px] text-[var(--muted-foreground)]">
          +{delta.total.toLocaleString()} additional tokens vs all-NIOS
        </div>
      )}

      <div>
        <button
          type="button"
          onClick={() => setEvidenceOpen((v) => !v)}
          aria-expanded={evidenceOpen}
          className="flex items-center gap-2 text-[13px] text-[var(--muted-foreground)] hover:text-foreground transition-colors"
        >
          {evidenceOpen ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
          Show raw relationship evidence
        </button>
        {evidenceOpen && (
          <div className="mt-2 pl-5 text-[12px] text-[var(--muted-foreground)] space-y-1">
            <div>Relationship rows observed: {evidence.relationshipRows.toLocaleString()}</div>
            <div>Duplicate relationship rows: {evidence.duplicateRelationshipRows.toLocaleString()}</div>
            <div>Relationship anomalies: {evidence.relationshipAnomalies.toLocaleString()}</div>
          </div>
        )}
      </div>
    </div>
  );
}
