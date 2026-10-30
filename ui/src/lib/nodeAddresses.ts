import type { NodeSummary } from './api';

// Interface addresses are agent reports, not proof of public reachability.
export function currentAgentAddresses(node: Pick<NodeSummary, 'network_observations'>, now = Date.now()): string[] {
  return [...new Set((node.network_observations ?? []).filter(row => {
    const observed = Date.parse(row.last_seen_at ?? '');
    return row.source === 'agent_interface_snapshot' && ['private_ip', 'public_ip'].includes(row.kind)
      && row.value && Number.isFinite(observed) && observed <= now && now - observed <= 5 * 60_000;
  }).map(row => row.value))].sort();
}
