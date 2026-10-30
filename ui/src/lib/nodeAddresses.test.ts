import { describe, expect, it } from 'vitest';
import { currentAgentAddresses } from './nodeAddresses';

describe('Current agent interface addresses', () => {
  it('separates current interface reports from enrollment, expired and future evidence', () => {
    const now = Date.parse('2026-10-29T10:00:00Z');
    const row = { kind: 'private_ip', value: '172.19.0.2', source: 'agent_interface_snapshot', confidence: 80, last_seen_at: '2026-10-29T09:59:00Z' };
    expect(currentAgentAddresses({ network_observations: [row, row, { ...row, value: '172.19.0.3', last_seen_at: '2026-10-28T09:59:00Z' }, { ...row, value: '10.0.0.1', last_seen_at: '2026-10-30T10:00:00Z' }, { ...row, value: '10.0.0.2', source: 'enrollment' }] }, now)).toEqual(['172.19.0.2']);
    expect(currentAgentAddresses({ network_observations: [row] }, now + 6 * 60_000)).toEqual([]);
  });
});
