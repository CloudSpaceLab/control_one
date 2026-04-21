import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { ReactNode } from 'react';
import { Remediation } from './Remediation';
import * as useApiClientModule from '../hooks/useApiClient';
import * as useToastModule from '../providers/ToastProvider';
import type {
  APIClient,
  PaginatedResponse,
  RemediationApproval,
  RemediationFailures,
  RemediationStats,
  RemediationVerificationStats,
  Tenant,
} from '../lib/api';

function Wrapper({ children }: { children: ReactNode }): JSX.Element {
  return <MemoryRouter>{children}</MemoryRouter>;
}

const sampleStats: RemediationStats = {
  window_start: '2026-04-14T00:00:00Z',
  window_end: '2026-04-21T00:00:00Z',
  totals: {
    total: 12,
    succeeded: 9,
    failed: 2,
    running: 1,
    queued: 0,
    cancelled: 0,
    success_rate: 81.82,
  },
  per_rule: [
    {
      rule_id: 'cis-ssh-disable-root',
      total: 6,
      succeeded: 6,
      failed: 0,
      running: 0,
      queued: 0,
      cancelled: 0,
      success_rate: 100,
      last_run_at: '2026-04-20T12:00:00Z',
    },
    {
      rule_id: 'cis-ufw-enable',
      total: 5,
      succeeded: 3,
      failed: 2,
      running: 0,
      queued: 0,
      cancelled: 0,
      success_rate: 60,
      last_run_at: '2026-04-20T13:30:00Z',
    },
  ],
};

const sampleFailures: RemediationFailures = {
  window_start: '2026-04-14T00:00:00Z',
  window_end: '2026-04-21T00:00:00Z',
  points: [
    { date: '2026-04-20T00:00:00Z', failed: 2, total: 5 },
    { date: '2026-04-21T00:00:00Z', failed: 0, total: 3 },
  ],
};

const sampleVerification: RemediationVerificationStats = {
  window_start: '2026-04-14T00:00:00Z',
  window_end: '2026-04-21T00:00:00Z',
  verified: 5,
  not_verified: 1,
  rolled_back: 0,
  pending_verify: 2,
  total_attempted: 8,
};

const sampleApprovals: PaginatedResponse<RemediationApproval> = {
  data: [
    {
      id: 'a1',
      tenant_id: 'tenant-1',
      node_id: 'node-1',
      rule_id: 'cis-pwd-age',
      script_id: 'script-1',
      severity: 'critical',
      status: 'pending',
      created_at: '2026-04-20T10:00:00Z',
      expires_at: '2026-04-21T10:00:00Z',
    },
  ],
  pagination: { total: 1, count: 1, limit: 50, offset: 0, nextOffset: null, prevOffset: null },
};

const sampleTenants: PaginatedResponse<Tenant> = {
  data: [{ id: 'tenant-1', name: 'Acme Bank', created_at: '2026-01-01T00:00:00Z' }],
  pagination: { total: 1, count: 1, limit: 50, offset: 0, nextOffset: null, prevOffset: null },
};

function makeApiClientStub(): Pick<
  APIClient,
  | 'getRemediationStats'
  | 'getRemediationFailures'
  | 'getRemediationVerificationStats'
  | 'listRemediationApprovals'
  | 'approveRemediationApproval'
  | 'denyRemediationApproval'
  | 'listTenants'
> {
  return {
    getRemediationStats: vi.fn().mockResolvedValue(sampleStats),
    getRemediationFailures: vi.fn().mockResolvedValue(sampleFailures),
    getRemediationVerificationStats: vi.fn().mockResolvedValue(sampleVerification),
    listRemediationApprovals: vi.fn().mockResolvedValue(sampleApprovals),
    approveRemediationApproval: vi.fn().mockResolvedValue(sampleApprovals.data[0]),
    denyRemediationApproval: vi.fn().mockResolvedValue(sampleApprovals.data[0]),
    listTenants: vi.fn().mockResolvedValue(sampleTenants),
  };
}

describe('Remediation dashboard', () => {
  let confirmSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    vi.spyOn(useToastModule, 'useToast').mockReturnValue({
      toasts: [],
      showToast: vi.fn(),
      dismissToast: vi.fn(),
    });
    const api = makeApiClientStub();
    vi.spyOn(useApiClientModule, 'useApiClient').mockReturnValue(api as unknown as APIClient);
    confirmSpy = vi.spyOn(globalThis, 'confirm').mockReturnValue(true);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    confirmSpy.mockRestore();
  });

  it('renders per-rule stats, failures, verification and approvals', async () => {
    render(<Remediation />, { wrapper: Wrapper });

    // Stats load first. Wait for the overall success rate to surface.
    await waitFor(() => {
      expect(screen.getByText(/Overall success/i)).toBeInTheDocument();
    });
    expect(screen.getByText('82')).toBeInTheDocument(); // rounded total success rate
    expect(screen.getByText('cis-ssh-disable-root')).toBeInTheDocument();
    expect(screen.getByText('cis-ufw-enable')).toBeInTheDocument();

    // Verification card labels render. Scope to the card container to avoid
    // clashing with the other places the word "Verified" might appear.
    const verificationCards = document.querySelectorAll('.verification-card');
    expect(verificationCards.length).toBe(4);
    const verificationLabels = Array.from(verificationCards).map((card) =>
      card.querySelector('.verification-label')?.textContent?.toLowerCase() ?? '',
    );
    expect(verificationLabels).toEqual(
      expect.arrayContaining(['verified', 'pending verify', 'not verified', 'rolled back']),
    );

    // Approval queue renders with the seeded row.
    expect(await screen.findByText('cis-pwd-age')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /approve remediation for cis-pwd-age/i })).toBeEnabled();
  });

  it('calls approve on the API client when the operator confirms', async () => {
    const api = makeApiClientStub();
    const approveMock = api.approveRemediationApproval as ReturnType<typeof vi.fn>;
    vi.spyOn(useApiClientModule, 'useApiClient').mockReturnValue(api as unknown as APIClient);

    render(<Remediation />, { wrapper: Wrapper });

    const approveButton = await screen.findByRole('button', {
      name: /approve remediation for cis-pwd-age/i,
    });

    const user = userEvent.setup();
    await user.click(approveButton);

    await waitFor(() => {
      expect(approveMock).toHaveBeenCalledWith('a1');
    });
  });

  it('skips the approval API call when the confirm dialog is cancelled', async () => {
    confirmSpy.mockReturnValue(false);

    const api = makeApiClientStub();
    const approveMock = api.approveRemediationApproval as ReturnType<typeof vi.fn>;
    vi.spyOn(useApiClientModule, 'useApiClient').mockReturnValue(api as unknown as APIClient);

    render(<Remediation />, { wrapper: Wrapper });

    const approveButton = await screen.findByRole('button', {
      name: /approve remediation for cis-pwd-age/i,
    });

    const user = userEvent.setup();
    await user.click(approveButton);

    expect(approveMock).not.toHaveBeenCalled();
  });

  it('renders empty state when there are no pending approvals', async () => {
    const api = makeApiClientStub();
    (api.listRemediationApprovals as ReturnType<typeof vi.fn>).mockResolvedValue({
      data: [],
      pagination: { total: 0, count: 0, limit: 50, offset: 0, nextOffset: null, prevOffset: null },
    });
    vi.spyOn(useApiClientModule, 'useApiClient').mockReturnValue(api as unknown as APIClient);

    render(<Remediation />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(
        screen.getByText(/No high-severity remediations are waiting for approval/i),
      ).toBeInTheDocument();
    });
  });
});
