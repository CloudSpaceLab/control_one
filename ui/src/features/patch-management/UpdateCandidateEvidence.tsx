import { Panel } from '@/components/kit';
import type { UpdateCandidate } from '@/lib/api';
import { formatTs } from '@/lib/format';

export function UpdateCandidateEvidence({ records, query = '' }: { records: UpdateCandidate[]; query?: string }) {
  const filtered = records.filter(row => row.name.toLowerCase().includes(query.toLowerCase()));
  const shown = filtered.slice(0, 25);
  return <Panel padding="md" eyebrow="UPDATE CANDIDATES" title="Repository availability and security applicability">
    <p className="text-sm text-text-secondary">{records.filter(row => row.repository_verified).length} packages have current verified repository evidence · {records.filter(row => row.security_fix_applicable).length} candidates meet known fix boundaries in selected advisory coverage.</p>
    <p className="mt-1 text-sm text-text-secondary">Newer does not mean a verified security fix. Repository availability, advisory boundaries and post-install verification are separate evidence. These records do not establish comprehensive advisory coverage or enable automatic installation.</p>
    {records.length === 0 ? <p className="mt-2 text-sm">Candidate evidence unavailable. A fresh supported native snapshot and repository observation are required.</p> : <details className="mt-3 text-sm"><summary className="cursor-pointer">Candidate versions and provenance ({filtered.length})</summary>
      <p className="mt-2 text-xs">Showing up to 25 matches. The package filter below also filters these records.</p>
      <ul className="mt-2 space-y-3">{shown.map(row => <li key={`${row.name}:${row.arch}`} className="rounded border border-border p-3">
        <p>{row.name} · installed {row.installed_version} · repository candidate {row.repository_observation?.candidate_version || 'unknown'} ({row.version_relation})</p>
        <p>Repository {row.repository_verified ? 'verified' : 'unverified'} · scoped security fix {row.security_fix_applicable ? 'applicable' : 'not established'}</p>
        <p className="text-xs text-text-secondary">{row.reason.replaceAll('_', ' ')}</p>
        {row.fix_boundaries.map(item => <p key={`${item.cve_id}:${item.fixed_version}`} className="text-xs">{item.cve_id} · {item.severity} · recorded fix boundary {item.fixed_version || 'unknown'} · {item.currently_affected === false ? 'installed version meets boundary' : 'affected installed version'} · {item.advisory_url}</p>)}
        {row.repository_observation && <details className="mt-1 text-xs"><summary className="cursor-pointer">Repository provenance</summary>
          <p>{row.repository_observation.vendor || 'Vendor unknown'} · {row.repository_observation.repository || 'Repository unknown'} · {row.repository_observation.suite}/{row.repository_observation.component}</p>
          <p>Observed {formatTs(row.repository_observation.observed_at)} · expires {row.repository_observation.expires_at && !row.repository_observation.expires_at.startsWith('0001-') ? formatTs(row.repository_observation.expires_at) : 'unknown'}</p>
          <p className="break-all">Signed Release digest: {row.repository_observation.release_sha256 || 'unknown'}</p>
          <p className="break-all">Package index digest: {row.repository_observation.index_sha256 || 'unknown'}</p>
          <p className="break-all">Candidate artifact digest: {row.repository_observation.artifact_sha256 || 'unknown'}</p>
        </details>}
      </li>)}</ul>
    </details>}
  </Panel>;
}
