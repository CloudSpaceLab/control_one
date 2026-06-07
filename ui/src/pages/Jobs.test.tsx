import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Jobs } from './Jobs';

const mocks = vi.hoisted(() => {
  const createComplianceScan = vi.fn();
  const createJob = vi.fn();
  const getJob = vi.fn();
  const showToast = vi.fn();

  return {
    createComplianceScan,
    createJob,
    getJob,
    showToast,
    apiClient: {
      createComplianceScan,
      createJob,
      getJob,
    },
  };
});

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

vi.mock('../hooks/useJobs', () => ({
  useJobs: () => ({
    data: [],
    loading: false,
    error: null,
    pagination: { total: 0, count: 0, limit: 20, offset: 0, nextOffset: null, prevOffset: null },
    refresh: vi.fn(),
  }),
}));

vi.mock('../hooks/useTenants', () => ({
  useTenants: () => ({
    data: [{ id: 'tenant-1', name: 'Bank Tenant', created_at: '2026-01-01T00:00:00Z' }],
    loading: false,
    error: null,
    pagination: { total: 1, count: 1, limit: 10, offset: 0, nextOffset: null, prevOffset: null },
    reload: vi.fn(),
    refresh: vi.fn(),
  }),
}));

vi.mock('../providers/TenantProvider', () => ({
  useTenant: () => ({ currentTenantId: 'tenant-1' }),
}));

vi.mock('../providers/ToastProvider', () => ({
  useToast: () => ({ showToast: mocks.showToast }),
}));

vi.mock('../hooks/useCancelJob', () => ({
  useCancelJob: () => ({ cancelJob: vi.fn() }),
}));

vi.mock('../hooks/useWorkerStatus', () => ({
  useWorkerStatus: () => ({
    status: {
      backend: 'memory',
      started: true,
      queue_depth: 0,
      active: 0,
      job_integrations: {
        'provision.apply': {
          mode: 'simulated',
          label: 'Simulated provisioning',
          detail: 'No external provisioning API is configured; jobs record workflow evidence without changing infrastructure.',
          external: false,
          simulated: true,
          mutates_infrastructure: false,
        },
        'compliance.scan': {
          mode: 'local_policy',
          label: 'Local policy evaluator',
          detail: 'No external scanner is configured; jobs evaluate Control One policies against stored node evidence and return no fabricated results.',
          external: false,
          simulated: false,
          mutates_infrastructure: false,
        },
      },
    },
    loading: false,
    error: null,
    refresh: vi.fn(),
  }),
}));

describe('Jobs', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.createJob.mockResolvedValue({});
    mocks.createComplianceScan.mockResolvedValue({ job_ids: ['job-1', 'job-2'], count: 2 });
  });

  it('surfaces provisioning and compliance execution modes', () => {
    render(<Jobs />);

    expect(screen.getAllByText('Simulated provisioning').length).toBeGreaterThan(0);
    expect(screen.getByText(/without changing infrastructure/i)).toBeInTheDocument();
    expect(screen.getByText('Local policy evaluator')).toBeInTheDocument();
  });

  it('routes blank-node compliance scans through the batch endpoint with policy facts', async () => {
    const user = userEvent.setup();
    render(<Jobs />);

    await user.selectOptions(screen.getByLabelText(/job type/i), 'compliance.scan');
    fireEvent.change(screen.getByLabelText(/rule set/i), { target: { value: 'cis-level-1' } });
    await user.click(screen.getByRole('button', { name: /submit job/i }));

    await waitFor(() => expect(mocks.createComplianceScan).toHaveBeenCalledTimes(1));
    expect(mocks.createComplianceScan).toHaveBeenCalledWith({
      tenant_id: 'tenant-1',
      node_ids: [],
      policies: { rule_set: 'cis-level-1' },
    });
    expect(mocks.createJob).not.toHaveBeenCalled();
  });
});
