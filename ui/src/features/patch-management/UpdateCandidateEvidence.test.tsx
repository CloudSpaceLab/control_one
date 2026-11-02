import { render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { UpdateCandidate } from '@/lib/api';
import { UpdateCandidateEvidence } from './UpdateCandidateEvidence';

const candidate: UpdateCandidate = { name: 'demo', arch: 'amd64', installed_version: '1.0', inventory_generation: 'inventory-1', coverage: 'selected_advisories_only', security_fix_applicable: false, repository_verified: true, version_relation: 'newer', reason: 'repository_candidate_not_proven_to_fix_selected_advisories', fix_boundaries: [{ cve_id: 'CVE-2026-FIXTURE', severity: 'high', fixed_version: '2.0', advisory_url: 'https://vendor.example/advisory' }], repository_observation: { candidate_version: '1.5', observed_at: '2026-10-28T10:00:00Z', state: 'verified_repository_candidate', reason: 'signed_release_and_index_verified', repository: 'https://archive.ubuntu.com/ubuntu', vendor: 'Ubuntu', suite: 'noble-updates', component: 'main', expires_at: '2026-10-29T10:00:00Z', release_sha256: 'release-digest', index_sha256: 'index-digest', artifact_sha256: 'artifact-digest' } };

describe('Update-candidate evidence', () => {
  beforeEach(()=>{vi.useFakeTimers();vi.setSystemTime(new Date('2026-10-28T10:10:00Z'));});
  afterEach(()=>vi.useRealTimers());
  it('separates a newer repository version from the advisory fix boundary', () => {
    render(<UpdateCandidateEvidence records={[candidate]} />);
    expect(screen.getByText(/repository candidate 1.5 \(newer\)/)).toHaveTextContent('installed 1.0');
    expect(screen.getByText(/scoped security fix not established/)).toBeInTheDocument();
    expect(screen.getByText(/recorded fix boundary 2.0/)).toBeInTheDocument();
    expect(screen.getByText(/Package index digest/)).toHaveTextContent('index-digest');
  });
  it('identifies a scoped applicable fix without claiming automatic installation', () => {
    render(<UpdateCandidateEvidence records={[{ ...candidate, security_fix_applicable: true }]} />);
    expect(screen.getByText(/scoped security fix applicable/)).toBeInTheDocument();
    expect(screen.getByText(/do not establish comprehensive advisory coverage or enable automatic installation/)).toBeInTheDocument();
  });
  it('shows unavailable coverage instead of inventing candidate versions', () => {
    render(<UpdateCandidateEvidence records={[]} />);
    expect(screen.getByText(/Candidate evidence unavailable/)).toBeInTheDocument();
  });
  it('applies the installed-package filter to candidate records', () => {
    render(<UpdateCandidateEvidence records={[candidate]} query="other" />);
    expect(screen.getByText('Candidate versions and provenance (0)')).toBeInTheDocument();
    expect(screen.queryByText(/repository candidate 1.5/)).not.toBeInTheDocument();
  });
  it('withdraws expired positive repository and fix counts',()=>{
    vi.setSystemTime(new Date('2026-10-30T10:10:00Z'));
    render(<UpdateCandidateEvidence records={[{...candidate,security_fix_applicable:true}]}/>);
    expect(screen.getByText(/0 packages have current verified repository evidence/)).toBeInTheDocument();
    expect(screen.getByText(/Repository unverified or expired · scoped security fix not established/)).toBeInTheDocument();
  });
});
