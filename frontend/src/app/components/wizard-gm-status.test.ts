import { describe, expect, test } from 'vitest';
import { resolveGmStatus } from './wizard-gm-status';
import type { NiosServerMetrics, ServerFormFactor } from './nios-calc';

const baseGm: NiosServerMetrics = {
  memberId: 'gm-1',
  memberName: 'gm.example.com',
  role: 'GM',
  qps: 0,
  lps: 0,
  objectCount: 0,
  activeIPCount: 0,
  model: 'IB-4030',
  platform: 'Physical',
  managedIPCount: 0,
  staticHosts: 0,
  dynamicHosts: 0,
  dhcpUtilization: 0,
  runsDnsDhcp: false,
};

describe('resolveGmStatus', () => {
  test('retained GM, management-only -> Retained on NIOS, no server tokens', () => {
    const status = resolveGmStatus(baseGm, new Map());
    expect(status.label).toBe('Retained on NIOS');
    expect(status.serverTokens).toBe('none');
    expect(status.formFactor).toBeNull();
  });

  test('migrated GM, management-only (runsDnsDhcp false) -> Replaced by Infoblox Portal', () => {
    const map = new Map<string, ServerFormFactor>([['gm.example.com', 'nios-x']]);
    const status = resolveGmStatus(baseGm, map);
    expect(status.label).toBe('Replaced by Infoblox Portal');
    expect(status.serverTokens).toBe('none');
    expect(status.formFactor).toBeNull();
  });

  test('migrated GM running DNS/DHCP -> sized as NIOS-X form factor', () => {
    const gmWithDns = { ...baseGm, runsDnsDhcp: true };
    const map = new Map<string, ServerFormFactor>([['gm.example.com', 'nios-x']]);
    const status = resolveGmStatus(gmWithDns, map);
    expect(status.label).toBe('');
    expect(status.serverTokens).toBe('sized');
    expect(status.formFactor).toBe('nios-x');
  });

  test('retained GM running DNS/DHCP still Retained on NIOS, no server tokens (retained always wins)', () => {
    const gmWithDns = { ...baseGm, runsDnsDhcp: true };
    const status = resolveGmStatus(gmWithDns, new Map());
    expect(status.label).toBe('Retained on NIOS');
    expect(status.serverTokens).toBe('none');
  });

  test('non-GM/GMC member passes through unaffected', () => {
    const member = { ...baseGm, memberName: 'member1.example.com', role: 'DNS/DHCP' as const };
    const map = new Map<string, ServerFormFactor>([['member1.example.com', 'nios-x']]);
    const status = resolveGmStatus(member, map);
    expect(status.label).toBe('');
    expect(status.serverTokens).toBe('sized');
    expect(status.formFactor).toBe('nios-x');
  });
});
