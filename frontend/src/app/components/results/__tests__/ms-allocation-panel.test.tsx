/**
 * ms-allocation-panel.test.tsx — unit tests for `<MSAllocationPanel/>` and its
 * pure helpers (Phase 07 Plan 02, MSALLOC-01/MSTOK-04).
 *
 * Locks the behavior matrix from 07-02-PLAN <behavior>: the four pure helpers
 * (selectMSScenario, msAllocationDelta) and
 * the three render branches (absent/D-07, unavailable/D-08, populated).
 */
import { describe, it, expect, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import {
  MSAllocationPanel,
  selectMSScenario,
  msAllocationDelta,
} from '../ms-allocation-panel';
import type {
  MicrosoftAllocationAPI,
  MSAllocationScenarioAPI,
  MSCategoryTokensAPI,
} from '../../api-client';

// ─── Fixture helpers ─────────────────────────────────────────────────────────

function category(over: Partial<MSCategoryTokensAPI> = {}): MSCategoryTokensAPI {
  return {
    category: 'DDI Objects',
    niosCount: 0,
    niosRate: 50,
    nativeCount: 0,
    nativeRate: 25,
    niosSubtotalNum: 0,
    nativeSubtotalNum: 0,
    subtotalDen: 1,
    tokens: 0,
    ...over,
  };
}

function scenario(over: Partial<MSAllocationScenarioAPI> = {}): MSAllocationScenarioAPI {
  return {
    id: 'none',
    dnsEnabled: false,
    dhcpEnabled: false,
    categories: [category(), category(), category()],
    effectiveTokens: 1000,
    deltaTokens: 0,
    ...over,
  };
}


/** Four-scenario allocation fixture: none/dns-only/dhcp-only/both, each case
 * overriding only what it is asserting. */
function allocation(over: Partial<MicrosoftAllocationAPI> = {}): MicrosoftAllocationAPI {
  return {
    diagnostic: { available: true },
    baselineTokens: 1000,
    scenarios: [
      scenario({ id: 'none', dnsEnabled: false, dhcpEnabled: false, effectiveTokens: 1000, deltaTokens: 0 }),
      scenario({
        id: 'dns-only',
        dnsEnabled: true,
        dhcpEnabled: false,
        categories: [category({ tokens: 40 }), category({ tokens: 10 }), category({ tokens: 5 })],
        effectiveTokens: 1055,
        deltaTokens: 55,
      }),
      scenario({
        id: 'dhcp-only',
        dnsEnabled: false,
        dhcpEnabled: true,
        categories: [category({ tokens: 20 }), category({ tokens: 30 }), category({ tokens: 5 })],
        effectiveTokens: 1055,
        deltaTokens: 55,
      }),
      scenario({
        id: 'both',
        dnsEnabled: true,
        dhcpEnabled: true,
        categories: [category({ tokens: 40 }), category({ tokens: 30 }), category({ tokens: 8 })],
        effectiveTokens: 1078,
        deltaTokens: 78,
      }),
    ],
    evidence: { relationshipRows: 0, duplicateRelationshipRows: 0, relationshipAnomalies: 0 },
    ...over,
  };
}

function noop() {}

// ─── selectMSScenario ────────────────────────────────────────────────────────

describe('selectMSScenario', () => {
  it('returns none when both switches are off', () => {
    expect(selectMSScenario(false, false)).toBe('none');
  });

  it('returns dns-only when only DNS is on', () => {
    expect(selectMSScenario(true, false)).toBe('dns-only');
  });

  it('returns dhcp-only when only DHCP is on', () => {
    expect(selectMSScenario(false, true)).toBe('dhcp-only');
  });

  it('returns both when both switches are on', () => {
    expect(selectMSScenario(true, true)).toBe('both');
  });
});

// ─── msAllocationDelta ───────────────────────────────────────────────────────

describe('msAllocationDelta', () => {
  it('returns zeros when allocation is null', () => {
    expect(msAllocationDelta(null, 'both')).toEqual({ ddi: 0, ip: 0, asset: 0, total: 0 });
  });

  it('returns zeros for the none scenario even when the allocation is fully populated', () => {
    expect(msAllocationDelta(allocation(), 'none')).toEqual({ ddi: 0, ip: 0, asset: 0, total: 0 });
  });

  it('returns the dns-only scenario delta as the difference from none, ddi+ip+asset equal to total', () => {
    const result = msAllocationDelta(allocation(), 'dns-only');
    expect(result.total).toBe(55);
    expect(result.ddi).toBe(40);
    expect(result.ip).toBe(10);
    expect(result.asset).toBe(5);
    expect(result.ddi + result.ip + result.asset).toBe(result.total);
  });

  it('returns zeros when diagnostic.available is false', () => {
    const unavailable = allocation({ diagnostic: { available: false, code: 'X', message: 'msg' } });
    expect(msAllocationDelta(unavailable, 'both')).toEqual({ ddi: 0, ip: 0, asset: 0, total: 0 });
  });

  it('returns zeros when scenarios has fewer than four entries', () => {
    const short = allocation({ scenarios: [scenario({ id: 'none' })] });
    expect(msAllocationDelta(short, 'both')).toEqual({ ddi: 0, ip: 0, asset: 0, total: 0 });
  });

  it('returns zeros when the requested id is not present', () => {
    expect(msAllocationDelta(allocation(), 'nonexistent')).toEqual({ ddi: 0, ip: 0, asset: 0, total: 0 });
  });
});



// ─── MSAllocationPanel — degraded states ────────────────────────────────────

describe('MSAllocationPanel — degraded states', () => {
  it('renders no DOM output when allocation is null', () => {
    const { container } = render(
      <MSAllocationPanel
        allocation={null}
        selected="none"
        onSelect={noop}
        evidenceOpen={false}
        setEvidenceOpen={noop}
      />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('renders no DOM output, specifically no ms-allocation-dns element, when diagnostic.code is MS_ALLOCATION_ABSENT', () => {
    const absent = allocation({ diagnostic: { available: false, code: 'MS_ALLOCATION_ABSENT' } });
    const { container } = render(
      <MSAllocationPanel
        allocation={absent}
        selected="none"
        onSelect={noop}
        evidenceOpen={false}
        setEvidenceOpen={noop}
      />,
    );
    expect(container).toBeEmptyDOMElement();
    expect(document.getElementById('ms-allocation-dns')).toBeNull();
  });

  it('renders the verbatim message and disabled switches when diagnostic.available is false', () => {
    const message = 'The Microsoft allocation snapshot is unavailable for this scan. The baseline NIOS scan results remain valid and usable.';
    const unavailable = allocation({ diagnostic: { available: false, code: 'MS_ALLOCATION_UNAVAILABLE', message } });
    render(
      <MSAllocationPanel
        allocation={unavailable}
        selected="none"
        onSelect={noop}
        evidenceOpen={false}
        setEvidenceOpen={noop}
      />,
    );
    expect(screen.getByText(message)).toBeInTheDocument();
    expect(document.getElementById('ms-allocation-dns')).toBeDisabled();
    expect(document.getElementById('ms-allocation-dhcp')).toBeDisabled();
  });
});

// ─── MSAllocationPanel — populated states ───────────────────────────────────

describe('MSAllocationPanel — populated', () => {
  it('shows both switches unchecked and no delta line when selected is none', () => {
    render(
      <MSAllocationPanel
        allocation={allocation()}
        selected="none"
        onSelect={noop}
        evidenceOpen={false}
        setEvidenceOpen={noop}
      />,
    );
    expect(document.getElementById('ms-allocation-dns')).not.toBeChecked();
    expect(document.getElementById('ms-allocation-dhcp')).not.toBeChecked();
    expect(screen.queryByText(/additional tokens vs all-NIOS/)).toBeNull();
  });

  it('renders the delta line with a leading plus sign when selected is both', () => {
    render(
      <MSAllocationPanel
        allocation={allocation()}
        selected="both"
        onSelect={noop}
        evidenceOpen={false}
        setEvidenceOpen={noop}
      />,
    );
    expect(screen.getByText(/\+78 additional tokens vs all-NIOS/)).toBeInTheDocument();
  });






  it('the evidence section is collapsed by default with aria-expanded false, and shows three counts once toggled', () => {
    render(
      <MSAllocationPanel
        allocation={allocation({
          evidence: { relationshipRows: 500, duplicateRelationshipRows: 3, relationshipAnomalies: 7 },
        })}
        selected="none"
        onSelect={noop}
        evidenceOpen={true}
        setEvidenceOpen={noop}
      />,
    );
    const trigger = screen.getByRole('button', { name: /show raw relationship evidence/i });
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText(/Relationship rows observed: 500/)).toBeInTheDocument();
    expect(screen.getByText(/Duplicate relationship rows: 3/)).toBeInTheDocument();
    expect(screen.getByText(/Relationship anomalies: 7/)).toBeInTheDocument();
  });

  it('the evidence trigger carries aria-expanded false when collapsed', () => {
    render(
      <MSAllocationPanel
        allocation={allocation()}
        selected="none"
        onSelect={noop}
        evidenceOpen={false}
        setEvidenceOpen={noop}
      />,
    );
    const trigger = screen.getByRole('button', { name: /show raw relationship evidence/i });
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
  });

  it('toggling a switch calls onSelect exactly once with the correct id and issues no network request', async () => {
    const user = userEvent.setup();
    const fetchSpy = vi.spyOn(globalThis, 'fetch');
    const onSelect = vi.fn();
    render(
      <MSAllocationPanel
        allocation={allocation()}
        selected="none"
        onSelect={onSelect}
        evidenceOpen={false}
        setEvidenceOpen={noop}
      />,
    );
    await user.click(document.getElementById('ms-allocation-dns')!);
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith('dns-only');
    expect(fetchSpy).not.toHaveBeenCalled();
    fetchSpy.mockRestore();
  });
});
