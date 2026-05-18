import type { Job } from './api';

export type AgentUpdateStatusTone = 'healthy' | 'warning' | 'degraded' | 'critical' | 'unknown';

interface AgentUpdatePayload {
  node_id?: unknown;
  target_version?: unknown;
}

function payload(job?: Job | null): AgentUpdatePayload {
  if (!job || typeof job.payload !== 'object' || job.payload === null || Array.isArray(job.payload)) {
    return {};
  }
  return job.payload as AgentUpdatePayload;
}

export function agentUpdateNodeId(job?: Job | null): string | null {
  const nodeId = payload(job).node_id;
  return typeof nodeId === 'string' && nodeId.trim() ? nodeId : null;
}

export function agentUpdateTargetVersion(job?: Job | null): string | null {
  const targetVersion = payload(job).target_version;
  return typeof targetVersion === 'string' && targetVersion.trim() ? targetVersion : null;
}

function jobTime(job: Job): number {
  const value = job.finished_at ?? job.updated_at ?? job.started_at ?? job.created_at;
  const ms = new Date(value).getTime();
  return Number.isFinite(ms) ? ms : 0;
}

export function latestAgentUpdateByNode(jobs: Job[]): Map<string, Job> {
  const latest = new Map<string, Job>();
  for (const job of jobs) {
    const nodeId = agentUpdateNodeId(job);
    if (!nodeId) continue;
    const current = latest.get(nodeId);
    if (!current || jobTime(job) >= jobTime(current)) {
      latest.set(nodeId, job);
    }
  }
  return latest;
}

export function agentUpdateStatusLabel(status?: string | null): string {
  switch ((status ?? '').toLowerCase()) {
    case 'queued':
      return 'queued';
    case 'running':
      return 'updating';
    case 'succeeded':
      return 'updated';
    case 'failed':
      return 'failed';
    case 'cancelled':
      return 'cancelled';
    default:
      return 'not queued';
  }
}

export function agentUpdateStatusTone(status?: string | null): AgentUpdateStatusTone {
  switch ((status ?? '').toLowerCase()) {
    case 'succeeded':
      return 'healthy';
    case 'queued':
      return 'warning';
    case 'running':
      return 'degraded';
    case 'failed':
    case 'cancelled':
      return 'critical';
    default:
      return 'unknown';
  }
}
