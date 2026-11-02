import type { OfflinePatchContent, NodeSummary } from './api';

export function offlinePlanPreparation(node: NodeSummary | undefined, plan: OfflinePatchContent | null): {jobId: string; completedAt: string} | null {
  const raw = node?.labels?.['patch.offline_prepared_plan'] as {job_id?: unknown; manifest_sha256?: unknown; completed_at?: unknown} | undefined;
  if (!plan || raw?.manifest_sha256 !== plan.manifest_sha256 || typeof raw.job_id !== 'string' || !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(raw.job_id) || typeof raw.completed_at !== 'string' || !Number.isFinite(Date.parse(raw.completed_at))) return null;
  return {jobId: raw.job_id, completedAt: raw.completed_at};
}

export function agentPublishedOfflinePlan(node: NodeSummary, now = Date.now()): OfflinePatchContent | null {
  const raw = node.labels?.['patch.offline_plans'] as { observed_at?: string; report?: { version?: number; status?: string; plans?: unknown[] } } | undefined;
  const observed = Date.parse(raw?.observed_at ?? '');
  if (node.state !== 'active' || !Number.isFinite(observed) || observed > now || now - observed > 300000 || raw?.report?.version !== 1 || raw.report.status !== 'ready' || raw.report.plans?.length !== 1) return null;
  try { const plan = parseOfflinePatchContent(JSON.stringify(raw.report.plans[0])); requireCurrentOfflinePlan(plan, now); return plan; } catch { return null; }
}

export function parseOfflinePatchContent(text: string): OfflinePatchContent {
  if (text.length > 131072) throw new Error('The offline content plan is too large.');
  let value: unknown;
  try { value = JSON.parse(text); } catch { throw new Error('Choose a valid offline content-plan JSON file.'); }
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Choose the public content plan produced by offline-stage.');
  const plan = value as Record<string, unknown>;
  const keys = ['format', 'manifest_sha256', 'installed_status_sha256', 'keyring_sha256', 'suite', 'component', 'architecture', 'requested', 'resolved', 'review'];
  if (Object.keys(plan).some(key => !keys.includes(key)) || (plan.format !== 2 && plan.format !== 3)) throw new Error('Choose the public format-2 or format-3 content plan, not the agent configuration.');
  for (const key of ['manifest_sha256', 'installed_status_sha256', 'keyring_sha256']) {
    if (typeof plan[key] !== 'string' || !/^[a-f0-9]{64}$/.test(plan[key] as string)) throw new Error('The content plan has an invalid integrity hash.');
  }
  for (const key of ['suite', 'component', 'architecture']) {
    if (typeof plan[key] !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9_.-]*$/.test(plan[key] as string)) throw new Error('The content plan has an invalid repository selector.');
  }
  for (const key of ['requested', 'resolved']) {
    const packages = plan[key];
    if (!Array.isArray(packages) || !packages.length || packages.length > 100) throw new Error('The content plan requires 1–100 exact package versions.');
    let previous = '';
    for (const pkg of packages) {
      if (!pkg || typeof pkg.name !== 'string' || typeof pkg.version !== 'string' || !/^[a-z0-9][a-z0-9+.-]*(?::[a-z0-9_-]+)?$/.test(pkg.name) || !/^[0-9][A-Za-z0-9.+:~-]*$/.test(pkg.version) || pkg.version.length > 256 || pkg.name <= previous) throw new Error('The content plan has invalid or duplicate package versions.');
      previous = pkg.name;
	  if (Object.keys(pkg).some(key => key !== 'name' && key !== 'version')) throw new Error('Package entries must contain only a name and version.');
    }
  }
  const typed = plan as unknown as OfflinePatchContent;
  for (const pkg of typed.requested) {
    if (!typed.resolved.some(candidate => candidate.name === pkg.name && candidate.version === pkg.version)) throw new Error('A requested version is missing from the resolved dependency plan.');
  }
  if (typed.format === 3) {
    const r = typed.review;
    if (!r || typeof r !== 'object' || Array.isArray(r) || Object.keys(r).some(key=>!['prepared_at','expires_at','changes','reboot','service_impact'].includes(key)) || typeof r.prepared_at !== 'string' || typeof r.expires_at !== 'string' || !Number.isFinite(Date.parse(r.prepared_at)) || !Number.isFinite(Date.parse(r.expires_at)) || Date.parse(r.expires_at)<=Date.parse(r.prepared_at) || Date.parse(r.expires_at)-Date.parse(r.prepared_at)>3600000 || r.reboot!=='unknown' || r.service_impact!=='requires_operator_review' || !Array.isArray(r.changes) || r.changes.length!==typed.resolved.length) throw new Error('The time-bounded offline review is invalid.');
    r.changes.forEach((change,i)=>{
      const pkg=typed.resolved[i]; const role=typed.requested.some(p=>p.name===pkg.name)?'requested':'dependency';
      if (!change || typeof change !== 'object' || Array.isArray(change) || Object.keys(change).some(key=>!['name','installed_version','proposed_version','role'].includes(key)) || change.name!==pkg.name || change.proposed_version!==pkg.version || change.role!==role || typeof change.installed_version!=='string' || change.installed_version.length>256 || (change.installed_version!=='' && !/^[0-9][A-Za-z0-9.+:~-]*$/.test(change.installed_version))) throw new Error('The before/after review differs from the resolved plan.');
    });
  } else if (typed.review!==undefined) throw new Error('Before/after review requires a format-3 plan.');
  return typed;
}

export function requireCurrentOfflinePlan(plan: OfflinePatchContent, now=Date.now()): void {
  if (plan.format===3 && (!plan.review || !Number.isFinite(Date.parse(plan.review.prepared_at)) || !Number.isFinite(Date.parse(plan.review.expires_at)) || Date.parse(plan.review.prepared_at)>now || Date.parse(plan.review.expires_at)<=now)) throw new Error('The offline plan expired or is not yet valid. Prepare and review a fresh plan.');
}
