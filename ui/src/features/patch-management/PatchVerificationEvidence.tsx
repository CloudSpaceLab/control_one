import { Panel } from '@/components/kit';
import type { PatchVerification, PatchHealthEvidence } from '@/lib/api';
import { formatTs } from '@/lib/format';

export function PatchVerificationEvidence({ records, health }: { records: PatchVerification[]; health?: PatchHealthEvidence | null }) {
  return <Panel padding="md" eyebrow="POST-INSTALL VERIFICATION" title="Execution and verified outcome">
    <p className="text-sm text-text-secondary">Installation success is recorded separately from a verified security fix. Promotion requires later authoritative inventory, advisory, service-health and reboot evidence.</p>
    {health ? <div className="mt-2 text-sm">
      <p>Host health report: {health.current ? 'current' : 'expired'} · observed {formatTs(health.evidence.observed_at)}</p>
      <p>Package audit: {health.current ? health.evidence.package_audit : 'unknown'} · services: {health.current ? health.evidence.service_health : 'unknown'} · reboot required: {health.current && health.evidence.reboot_required !== null ? (health.evidence.reboot_required ? 'yes' : 'no') : 'unknown'}</p>
      <p className="text-xs text-text-secondary">Service inventory: {health.current && health.evidence.services_complete ? 'complete' : 'unavailable'} · running-consumer check: {health.current && health.evidence.consumers_complete ? 'complete' : 'unavailable'}. A required reboot also needs a later changed boot identity.</p>
    </div> : <p className="mt-2 text-sm">Host health evidence unavailable. Service and reboot checks cannot be assumed to have passed.</p>}
    {records.length === 0 ? <p className="mt-2 text-sm">No verification record is available. Historical installation results do not establish a verified fix.</p> : <details className="mt-3 text-sm"><summary className="cursor-pointer">Execution verification records ({records.length})</summary><ul className="mt-3 space-y-3">{records.map(record => <li key={record.job_id} className="rounded border border-border p-3 text-sm">
      <p>{record.current_state.replaceAll('_', ' ')} · promotion {record.promotion_eligible ? 'eligible' : 'blocked'}</p>
      <p className="break-all font-mono text-xs">Execution {record.job_id} · deployment {record.deployment_id}</p>
      <p>{record.execution_completed_at ? `Execution completed ${formatTs(record.execution_completed_at)}` : 'Execution pending'}</p>
      <p>{record.evidence.reason?.replaceAll('_', ' ') ?? 'Verification evidence pending'}</p>
      {record.current_reasons?.map(reason => <p key={reason}>{reason.replaceAll('_', ' ')}</p>)}
      {record.evidence.assessment_id && <p className="break-all text-xs">Assessment {record.evidence.assessment_id}</p>}
      <ul className="mt-1 list-disc pl-5">{record.baseline.map((item, index) => <li key={`${item.name}:${item.cve_id}:${index}`}>{item.name} · installed at request {item.installed_version} · recorded fix boundary {item.fixed_version || 'unknown'} · {item.cve_id}</li>)}</ul>
    </li>)}</ul></details>}
  </Panel>;
}
