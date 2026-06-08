import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Policy, PolicyAssignment } from '../lib/api';
import { Compliance } from './Compliance';

const mocks = vi.hoisted(() => {
  const listPolicies = vi.fn();
  const listPolicyVersions = vi.fn();
  const listPolicyAssignments = vi.fn();
  const deletePolicyAssignment = vi.fn();
  const deletePolicy = vi.fn();
  const showToast = vi.fn();

  return {
    listPolicies,
    listPolicyVersions,
    listPolicyAssignments,
    deletePolicyAssignment,
    deletePolicy,
    showToast,
    apiClient: {
      listPolicies: (params: unknown) => listPolicies(params),
      listPolicyVersions: (id: string) => listPolicyVersions(id),
      listPolicyAssignments: (id: string) => listPolicyAssignments(id),
      deletePolicyAssignment: (policyId: string, assignmentId: string) => deletePolicyAssignment(policyId, assignmentId),
      deletePolicy: (id: string) => deletePolicy(id),
      listComplianceFrameworks: vi.fn().mockResolvedValue({ frameworks: [] }),
      listNodes: vi.fn().mockResolvedValue({ data: [], pagination: { total: 0, count: 0, limit: 500, offset: 0 } }),
      listClusters: vi.fn().mockResolvedValue({ data: [], pagination: { total: 0, count: 0, limit: 500, offset: 0 } }),
      listHypervisorHosts: vi.fn().mockResolvedValue({ items: [], total: 0 }),
      listEnrollmentTokens: vi.fn().mockResolvedValue({ data: [], pagination: { total: 0, count: 0, limit: 500, offset: 0 } }),
    },
  };
});

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

vi.mock('../providers/TenantProvider', () => ({
  useTenant: () => ({ currentTenantId: 'tenant-1' }),
}));

vi.mock('../providers/ToastProvider', () => ({
  useToast: () => ({ showToast: mocks.showToast }),
}));

vi.mock('../hooks/useTenants', () => ({
  useTenants: () => ({
    data: [{ id: 'tenant-1', name: 'Bank Tenant', created_at: '2026-01-01T00:00:00Z' }],
    loading: false,
    error: null,
  }),
}));

vi.mock('../hooks/useNodes', () => ({
  useNodes: () => ({
    data: [{ id: 'node-1', hostname: 'core-db-01', state: 'online' }],
    loading: false,
    error: null,
    pagination: { total: 1, count: 1, limit: 1000, offset: 0 },
  }),
}));

vi.mock('./ComplianceEvidence', () => ({
  ComplianceEvidence: () => <div>Evidence tab</div>,
}));

vi.mock('./Frameworks', () => ({
  Frameworks: () => <div>Frameworks tab</div>,
}));

vi.mock('./AuditReports', () => ({
  AuditReports: () => <div>Reports tab</div>,
}));

const samplePolicy: Policy = {
  id: 'policy-1',
  tenant_id: 'tenant-1',
  name: 'SSH baseline',
  description: 'Ensure SSH posture is continuously evaluated',
  rule_type: 'port_check',
  enabled: true,
  labels: {},
  created_at: '2026-06-08T00:00:00Z',
  updated_at: '2026-06-08T00:00:00Z',
};

const sampleAssignment: PolicyAssignment = {
  id: 'assignment-1',
  policy_id: 'policy-1',
  tenant_id: 'tenant-1',
  scope_type: 'tenant',
  selector: {},
  assigned_at: '2026-06-08T01:00:00Z',
};

function renderPoliciesTab() {
  return render(
    <MemoryRouter initialEntries={['/console/compliance?tab=policies']}>
      <Compliance />
    </MemoryRouter>,
  );
}

describe('Compliance policies', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listPolicies.mockResolvedValue({ data: [], pagination: { total: 0, count: 0, limit: 100, offset: 0 } });
    mocks.listPolicyVersions.mockResolvedValue({ data: [], pagination: { total: 0, count: 0, limit: 100, offset: 0 } });
    mocks.listPolicyAssignments.mockResolvedValue({ items: [], total: 0 });
    mocks.deletePolicyAssignment.mockResolvedValue(undefined);
    mocks.deletePolicy.mockResolvedValue(undefined);
    mocks.apiClient.listNodes.mockResolvedValue({ data: [], pagination: { total: 0, count: 0, limit: 500, offset: 0 } });
    mocks.apiClient.listClusters.mockResolvedValue({ data: [], pagination: { total: 0, count: 0, limit: 500, offset: 0 } });
    mocks.apiClient.listHypervisorHosts.mockResolvedValue({ items: [], total: 0 });
    mocks.apiClient.listEnrollmentTokens.mockResolvedValue({ data: [], pagination: { total: 0, count: 0, limit: 500, offset: 0 } });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('shows explicit unavailable state when policy loading fails', async () => {
    const user = userEvent.setup();
    mocks.listPolicies.mockRejectedValueOnce(new Error('policy store offline'));

    renderPoliciesTab();

    await user.click(screen.getByRole('button', { name: 'Load policies' }));

    expect(await screen.findAllByText('Compliance policies unavailable')).toHaveLength(2);
    expect(screen.getByRole('alert')).toHaveTextContent('policy store offline');
    expect(screen.queryByText('No policies')).not.toBeInTheDocument();
    expect(mocks.showToast).toHaveBeenCalledWith('policy store offline', 'error');
  });

  it('requires modal confirmation before deleting a policy and keeps failed deletion visible', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    mocks.listPolicies.mockResolvedValue({ data: [samplePolicy], pagination: { total: 1, count: 1, limit: 100, offset: 0 } });
    mocks.deletePolicy.mockRejectedValueOnce(new Error('policy is assigned'));

    renderPoliciesTab();

    await user.click(screen.getByRole('button', { name: 'Load policies' }));
    await screen.findByText('SSH baseline');
    await user.click(screen.getByRole('button', { name: 'Delete compliance policy SSH baseline' }));

    expect(confirmSpy).not.toHaveBeenCalled();
    expect(mocks.deletePolicy).not.toHaveBeenCalled();

    const dialog = screen.getByRole('dialog', { name: /delete compliance policy ssh baseline/i });
    await user.click(within(dialog).getByRole('button', { name: 'Delete policy' }));

    await waitFor(() => expect(mocks.deletePolicy).toHaveBeenCalledWith('policy-1'));
    expect(screen.getByRole('dialog', { name: /delete compliance policy ssh baseline/i })).toBeInTheDocument();
    expect(screen.getByText('Policy deletion failed')).toBeInTheDocument();
    expect(screen.getAllByText('Failed to delete policy SSH baseline: policy is assigned').length).toBeGreaterThanOrEqual(2);
  });

  it('surfaces assignment load failures instead of showing a false empty state', async () => {
    const user = userEvent.setup();
    mocks.listPolicies.mockResolvedValue({ data: [samplePolicy], pagination: { total: 1, count: 1, limit: 100, offset: 0 } });
    mocks.listPolicyAssignments.mockRejectedValueOnce(new Error('assignment store offline'));

    renderPoliciesTab();

    await user.click(screen.getByRole('button', { name: 'Load policies' }));
    await user.click(await screen.findByRole('button', { name: /expand compliance policy ssh baseline/i }));

    expect(await screen.findByText(/policy assignments unavailable: assignment store offline/i)).toBeInTheDocument();
    expect(screen.queryByText('No assignments.')).not.toBeInTheDocument();
  });

  it('surfaces version load failures instead of showing a false empty state', async () => {
    const user = userEvent.setup();
    mocks.listPolicies.mockResolvedValue({ data: [samplePolicy], pagination: { total: 1, count: 1, limit: 100, offset: 0 } });
    mocks.listPolicyVersions.mockRejectedValueOnce(new Error('version store offline'));

    renderPoliciesTab();

    await user.click(screen.getByRole('button', { name: 'Load policies' }));
    await user.click(await screen.findByRole('button', { name: /expand compliance policy ssh baseline/i }));

    expect(await screen.findByText(/policy versions unavailable: version store offline/i)).toBeInTheDocument();
    expect(screen.queryByText('No versions yet. Create a version to activate this policy.')).not.toBeInTheDocument();
  });

  it('keeps failed assignment removal inside the confirmation modal', async () => {
    const user = userEvent.setup();
    mocks.listPolicies.mockResolvedValue({ data: [samplePolicy], pagination: { total: 1, count: 1, limit: 100, offset: 0 } });
    mocks.listPolicyAssignments.mockResolvedValueOnce({ items: [sampleAssignment], total: 1 });
    mocks.deletePolicyAssignment.mockRejectedValueOnce(new Error('assignment removal denied'));

    renderPoliciesTab();

    await user.click(screen.getByRole('button', { name: 'Load policies' }));
    await user.click(await screen.findByRole('button', { name: /expand compliance policy ssh baseline/i }));
    await waitFor(() => expect(screen.getAllByText('Org-wide').length).toBeGreaterThan(0));
    await user.click(screen.getByRole('button', { name: /remove policy assignment org-wide/i }));

    const dialog = screen.getByRole('dialog', { name: /remove policy assignment/i });
    await user.click(within(dialog).getByRole('button', { name: /remove assignment/i }));

    await waitFor(() => expect(mocks.deletePolicyAssignment).toHaveBeenCalledWith('policy-1', 'assignment-1'));
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('assignment removal denied');
    expect(screen.getByRole('dialog', { name: /remove policy assignment/i })).toBeInTheDocument();
    expect(screen.getAllByText('Org-wide').length).toBeGreaterThan(0);
  });
});
