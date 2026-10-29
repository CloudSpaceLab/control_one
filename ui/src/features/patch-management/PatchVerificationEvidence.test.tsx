import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { PatchVerification } from '@/lib/api';
import { PatchVerificationEvidence } from './PatchVerificationEvidence';

const record: PatchVerification = { job_id: 'execution-1', deployment_id: 'deployment-1', node_id: 'node-1', state: 'verification_pending', current_state: 'verification_pending', promotion_eligible: false, updated_at: '2026-10-28T10:00:00Z', evidence: { reason: 'later_authoritative_inventory_required' }, baseline: [{ name: 'demo', installed_version: '1', fixed_version: '2', cve_id: 'CVE-2026-FIXTURE' }] };

describe('Post-install verification evidence', () => {
  it('keeps execution separate from fix verification and shows the package baseline', () => {
    render(<PatchVerificationEvidence records={[record]} />);
    expect(screen.getByText('verification pending · promotion blocked')).toBeInTheDocument();
    expect(screen.getByText(/installed at request 1/)).toHaveTextContent('recorded fix boundary 2');
    expect(screen.getByText(/Execution execution-1/)).toBeInTheDocument();
  });
  it('does not present saved verification as currently valid when evidence expired', () => {
    render(<PatchVerificationEvidence records={[{ ...record, state: 'verified_fixed', current_state: 'verification_unknown' }]} />);
    expect(screen.getByText('verification unknown · promotion blocked')).toBeInTheDocument();
  });
  it('does not certify historical installation results without verification records', () => {
    render(<PatchVerificationEvidence records={[]} />);
    expect(screen.getByText(/Historical installation results do not establish a verified fix/)).toBeInTheDocument();
  });
  it('does not display expired health observations as passing gates', () => {
    render(<PatchVerificationEvidence records={[]} health={{ current: false, evidence: { observed_at: '2026-10-28T09:00:00Z', boot_id: 'boot-1', reboot_required: false, package_audit: 'healthy', service_health: 'healthy', services_complete: true, consumers_complete: true } }} />);
    expect(screen.getByText(/Host health report: expired/)).toBeInTheDocument();
    expect(screen.getByText(/Package audit: unknown · services: unknown · reboot required: unknown/)).toBeInTheDocument();
  });
});
