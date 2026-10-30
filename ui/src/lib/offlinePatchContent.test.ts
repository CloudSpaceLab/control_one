import { describe, expect, it } from 'vitest';
import { agentPublishedOfflinePlan, offlinePlanPreparation, parseOfflinePatchContent } from './offlinePatchContent';
import type { NodeSummary } from './api';

const fixture = () => ({format:2 as const,manifest_sha256:'a'.repeat(64),installed_status_sha256:'b'.repeat(64),keyring_sha256:'c'.repeat(64),suite:'noble-updates',component:'main',architecture:'amd64',requested:[{name:'liblzma5',version:'1.2.3'}],resolved:[{name:'liblzma5',version:'1.2.3'},{name:'zlib1g',version:'1.3'}]});

it('shows preparation identity only for the matching manifest and a valid completion record',()=>{
  const plan=fixture();const job='1ae94861-5449-4b8d-bb6a-ae76718f7b61';
  const record={job_id:job,manifest_sha256:plan.manifest_sha256,completed_at:'2026-10-30T09:10:54Z'};
  const node={labels:{'patch.offline_prepared_plan':record}} as unknown as NodeSummary;
  expect(offlinePlanPreparation(node,plan)).toEqual({jobId:job,completedAt:record.completed_at});
  expect(offlinePlanPreparation(node,{...plan,manifest_sha256:'d'.repeat(64)})).toBeNull();
  expect(offlinePlanPreparation(undefined,plan)).toBeNull();
  record.completed_at='invalid';expect(offlinePlanPreparation(node,plan)).toBeNull();
});

describe('agent published plan discovery', () => {
  it.each(['ready', 'expired', 'future', 'withdrawn', 'malformed', 'inactive', 'missing'])('handles %s evidence without offering stale content', scenario => {
    const now = Date.now();
    const report = { version: 1, status: 'ready', plans: [fixture()] };
    const evidence = { observed_at: new Date(now).toISOString(), report };
    const node = { id: 'lab', state: 'active', labels: { 'patch.offline_plans': evidence } } as unknown as NodeSummary;
    if (scenario === 'expired') evidence.observed_at = new Date(now - 300001).toISOString();
    if (scenario === 'future') evidence.observed_at = new Date(now + 1000).toISOString();
    if (scenario === 'withdrawn') { report.status = 'unavailable'; report.plans = []; }
    if (scenario === 'malformed') report.plans[0].manifest_sha256 = 'invalid';
    if (scenario === 'inactive') node.state = 'retired';
    if (scenario === 'missing') node.labels = {};
    expect(agentPublishedOfflinePlan(node, now)).toEqual(scenario === 'ready' ? fixture() : null);
  });
});

describe('offline content plan import',()=>{
  it('imports an exact plan with reviewed dependencies',()=>{const plan=fixture();expect(parseOfflinePatchContent(JSON.stringify(plan))).toEqual(plan);});
  it.each(['credential','old format','bad hash','duplicate','missing requested','invalid version','oversized'])('rejects %s without forwarding it',scenario=>{
    const plan: Record<string,unknown> = fixture();
    if (scenario==='credential') plan.node_token='secret';
    if (scenario==='old format') plan.format=1;
    if (scenario==='bad hash') plan.manifest_sha256='not-a-hash';
    if (scenario==='duplicate') plan.resolved=[{name:'liblzma5',version:'1'},{name:'liblzma5',version:'2'}];
    if (scenario==='missing requested') plan.resolved=[{name:'zlib1g',version:'1'}];
    if (scenario==='invalid version') plan.requested=[{name:'liblzma5',version:'1;touch'}];
    const text=scenario==='oversized'?' '.repeat(131073):JSON.stringify(plan);
    expect(()=>parseOfflinePatchContent(text)).toThrow();
  });
});
