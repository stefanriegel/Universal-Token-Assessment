/**
 * results-migration-planner.test.tsx — unit tests for `<ResultsMigrationPlanner/>`
 * (Phase 34 Plan 01).
 *
 * Locks the lift-first contract from D-03/D-04/D-05: pure presentation,
 * props in / callbacks out, no Sizer or scan-store coupling. Verifies the
 * 4 behaviors mandated by 34-01-PLAN <behavior>:
 *   1. one row per member when given 3 NiosServerMetrics
 *   2. tier picker change invokes onTierChange with (memberId, newTier)
 *   3. empty members array renders nothing (no card chrome)
 *   4. root container exposes data-testid="results-migration-planner"
 */
import { describe, it, expect, vi } from 'vitest';
import { useState } from 'react';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import {
  ResultsMigrationPlanner,
  type ResultsMigrationPlannerProps,
} from '../results-migration-planner';
import { calcNiosTokens, calcUddiTokensAggregated, type NiosServerMetrics, type ServerFormFactor } from '../../mock-data';
import { LARGE_MEMBER, marginalDeltaFindings } from './fixtures/marginal-delta';

const member = (over: Partial<NiosServerMetrics> = {}): NiosServerMetrics => ({
  memberId: 'm1',
  memberName: 'gm.example.com',
  role: 'DNS/DHCP',
  qps: 100,
  lps: 10,
  objectCount: 200,
  activeIPCount: 50,
  model: 'IB-2225',
  platform: 'physical',
  managedIPCount: 0,
  staticHosts: 0,
  dynamicHosts: 0,
  dhcpUtilization: 0,
  ...over,
});

const baseProps = (over: Partial<ResultsMigrationPlannerProps> = {}): ResultsMigrationPlannerProps => ({
  mode: 'scan',
  members: [],
  findings: [],
  effectiveFindings: [],
  selectedProviders: ['nios'],
  growthBufferPct: 0,
  fleetSavings: {
    memberCount: 0,
    totalOldVCPU: 0,
    totalOldRamGB: 0,
    totalNewVCPU: 0,
    totalNewRamGB: 0,
    totalDeltaVCPU: 0,
    totalDeltaRamGB: 0,
    niosXSavings: { vCPU: 0, ramGB: 0, memberCount: 0 },
    xaasSavings: { vCPU: 0, ramGB: 0, memberCount: 0 },
    physicalUnitsRetired: 0,
    unknownModels: [],
    invalidCombinations: [],
  },
  memberSavings: [],
  niosMigrationMap: new Map(),
  setNiosMigrationMap: vi.fn(),
  setVariantOverrides: vi.fn(),
  serverMetricOverrides: {},
  setServerMetricOverrides: vi.fn(),
  editingServerMetric: null,
  setEditingServerMetric: vi.fn(),
  editingServerValue: '',
  setEditingServerValue: vi.fn(),
  memberSearchFilter: '',
  setMemberSearchFilter: vi.fn(),
  showGridMemberDetails: false,
  setShowGridMemberDetails: vi.fn(),
  gridMemberDetailSearch: '',
  setGridMemberDetailSearch: vi.fn(),
  niosGridFeatures: null,
  niosGridLicenses: null,
  niosMigrationFlags: null,
  gridFeaturesOpen: false,
  setGridFeaturesOpen: vi.fn(),
  migrationFlagsOpen: false,
  setMigrationFlagsOpen: vi.fn(),
  niosMicrosoftServers: null,
  microsoftServersOpen: false,
  setMicrosoftServersOpen: vi.fn(),
  onTierChange: vi.fn(),
  ...over,
});

describe('<ResultsMigrationPlanner/>', () => {
  it('Test 4: root container has data-testid="results-migration-planner"', () => {
    const members = [member({ memberId: 'm1', memberName: 'a.example.com' })];
    const { container } = render(<ResultsMigrationPlanner {...baseProps({ members })} />);
    expect(container.querySelector('[data-testid="results-migration-planner"]')).not.toBeNull();
  });

  it('Test 1: renders one row per member when given 3 NiosServerMetrics', () => {
    const members = [
      member({ memberId: 'm1', memberName: 'a.example.com' }),
      member({ memberId: 'm2', memberName: 'b.example.com' }),
      member({ memberId: 'm3', memberName: 'c.example.com' }),
    ];
    render(<ResultsMigrationPlanner {...baseProps({ members })} />);
    // Each member name should be rendered at least once in the planner
    expect(screen.getAllByText('a.example.com').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('b.example.com').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('c.example.com').length).toBeGreaterThanOrEqual(1);
  });

  it('Test 2: tier picker change invokes onTierChange with (memberId, newTier)', async () => {
    const onTierChange = vi.fn();
    // Pre-mark the member as migrating so the NIOS-X / XaaS picker is visible
    const members = [member({ memberId: 'm1', memberName: 'edge.example.com' })];
    const niosMigrationMap = new Map<string, any>([['edge.example.com', 'nios-x']]);
    const user = userEvent.setup();
    render(
      <ResultsMigrationPlanner
        {...baseProps({ members, niosMigrationMap, onTierChange })}
      />,
    );

    // The XaaS picker button — pick the first one in the migration-planner section
    const planner = screen.getByTestId('results-migration-planner');
    const xaasButton = within(planner).getAllByRole('button', { name: /xaas/i })[0];
    await user.click(xaasButton);
    expect(onTierChange).toHaveBeenCalledWith('edge.example.com', 'nios-xaas');
  });

  it('Test 3: empty members array renders nothing (no planner card)', () => {
    const { container } = render(<ResultsMigrationPlanner {...baseProps({ members: [] })} />);
    expect(container.querySelector('[data-testid="results-migration-planner"]')).toBeNull();
  });

  it('renders Microsoft-managed servers when provided and open', () => {
    const members = [member({ memberId: 'm1', memberName: 'gm.example.com' })];
    render(<ResultsMigrationPlanner {...baseProps({
      members,
      microsoftServersOpen: true,
      niosMicrosoftServers: {
        servers: [{
          fqdn: 'dc01.example.local', address: '10.0.0.11', os: 'Windows Server 2019',
          adDomain: 'example.local', dnsManaged: true, dhcpManaged: false, dhcpHosts: 0, readOnly: true,
        }],
        managedZones: 5,
      },
    })} />);
    expect(screen.getByText('dc01.example.local')).toBeTruthy();
    expect(screen.getByText('Microsoft-Managed Servers')).toBeTruthy();
  });

  it('omits Microsoft-managed section when servers empty', () => {
    const members = [member({ memberId: 'm1', memberName: 'gm.example.com' })];
    render(<ResultsMigrationPlanner {...baseProps({
      members,
      niosMicrosoftServers: { servers: [], managedZones: 0 },
    })} />);
    expect(screen.queryByText('Microsoft-Managed Servers')).toBeNull();
  });

  it('Marginal Delta: toggling a large synthetic member shows a delta equal to the actual bucket total change', async () => {
    // Wrapper wires niosMigrationMap through real React state so the toggle
    // handler's setNiosMigrationMap(next) call is actually reflected — a
    // plain vi.fn() in baseProps would no-op and hide the bug this guards.
    function Wrapper() {
      const [map, setMap] = useState<Map<string, ServerFormFactor>>(new Map());
      return (
        <ResultsMigrationPlanner
          {...baseProps({
            findings: marginalDeltaFindings,
            effectiveFindings: marginalDeltaFindings,
            niosMigrationMap: map,
            setNiosMigrationMap: setMap,
          })}
        />
      );
    }

    const user = userEvent.setup();
    render(<Wrapper />);

    const stayingBefore = calcNiosTokens(marginalDeltaFindings);
    const migratingBefore = calcUddiTokensAggregated([]);

    const nameCell = screen.getByText(LARGE_MEMBER).closest('.flex-1')!;
    const rowContent = nameCell.parentElement!; // sibling of the toggle <button>
    const toggleButton = within(rowContent as HTMLElement).getAllByRole('button')[0];
    await user.click(toggleButton);

    const largeMemberFindings = marginalDeltaFindings.filter((f) => f.source === LARGE_MEMBER);
    const remainingFindings = marginalDeltaFindings.filter((f) => f.source !== LARGE_MEMBER);
    const stayingAfter = calcNiosTokens(remainingFindings);
    const migratingAfter = calcUddiTokensAggregated(largeMemberFindings);
    const expectedStayingDelta = stayingAfter - stayingBefore;
    const expectedMigratingDelta = migratingAfter - migratingBefore;

    const deltaEl = await screen.findByTestId('migration-toggle-delta');
    if (expectedStayingDelta !== 0) {
      expect(deltaEl.textContent).toContain(`NIOS ${expectedStayingDelta > 0 ? '+' : ''}${expectedStayingDelta.toLocaleString()}`);
    }
    if (expectedMigratingDelta !== 0) {
      expect(deltaEl.textContent).toContain(`UDDI +${expectedMigratingDelta.toLocaleString()}`);
    }
  });

  describe('usability: tier rollup, filtering, sorting', () => {
    const members = [
      member({ memberId: 'm1', memberName: 'tiny-01.example.com', qps: 0, lps: 0, objectCount: 0, activeIPCount: 5 }), // 2XS
      member({ memberId: 'm2', memberName: 'tiny-02.example.com', qps: 0, lps: 0, objectCount: 0, activeIPCount: 10 }), // 2XS
      member({ memberId: 'm3', memberName: 'medium-01.example.com', qps: 40000, lps: 300, objectCount: 110000, activeIPCount: 500 }), // M
    ];

    it('renders a Server Tier distribution rollup counting ALL members regardless of Migration Bucket', () => {
      render(<ResultsMigrationPlanner {...baseProps({ members })} />);
      const rollup = screen.getByTestId('server-tier-rollup');
      expect(within(rollup).getByText(/2XS/).closest('span')?.textContent).toContain('2');
      expect(within(rollup).getByText(/^M\b/).closest('span')?.textContent ?? within(rollup).getByText(/M:/).textContent).toContain('1');
    });

    it('combines name search + Migration Bucket + Server Tier filters with AND logic', async () => {
      const user = userEvent.setup();
      const niosMigrationMap = new Map<string, ServerFormFactor>([['tiny-02.example.com', 'nios-x']]);
      function Wrapper() {
        const [search, setSearch] = useState('');
        return (
          <ResultsMigrationPlanner
            {...baseProps({
              members,
              niosMigrationMap,
              memberSearchFilter: search,
              setMemberSearchFilter: setSearch,
            })}
          />
        );
      }
      render(<Wrapper />);

      const planner = screen.getByTestId('migration-member-selector');

      // Name search narrows to the two "tiny" members
      await user.type(screen.getByPlaceholderText(/filter members/i), 'tiny');
      expect(within(planner).getAllByText('tiny-01.example.com').length).toBeGreaterThan(0);
      expect(within(planner).getAllByText('tiny-02.example.com').length).toBeGreaterThan(0);
      expect(within(planner).queryByText('medium-01.example.com')).toBeNull();

      // Migration Bucket = migrating narrows further to just tiny-02
      await user.click(screen.getByRole('button', { name: /migrating/i }));
      expect(within(planner).queryByText('tiny-01.example.com')).toBeNull();
      expect(within(planner).getAllByText('tiny-02.example.com').length).toBeGreaterThan(0);
    });

    it('sorts the member list by object count', async () => {
      const user = userEvent.setup();
      render(<ResultsMigrationPlanner {...baseProps({ members })} />);
      const planner = screen.getByTestId('migration-member-selector');
      await user.click(within(planner).getByRole('button', { name: /object count/i }));
      const names = within(planner).getAllByText(/\.example\.com/).map((el) => el.textContent ?? '');
      // Descending by objectCount: medium-01 (110000) before either tiny (0)
      const medIdx = names.findIndex((t) => t.startsWith('medium-01.example.com'));
      const tinyIdx = names.findIndex((t) => t.startsWith('tiny-01.example.com'));
      expect(medIdx).toBeLessThan(tinyIdx);
    });

    it('does not expose a token-based sort control', () => {
      render(<ResultsMigrationPlanner {...baseProps({ members })} />);
      const planner = screen.getByTestId('migration-member-selector');
      expect(within(planner).queryByRole('button', { name: /token/i })).toBeNull();
    });
  });
});
