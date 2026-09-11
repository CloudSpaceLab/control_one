import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Tenant, TenantRemediationConfig } from '../lib/api';
import { Tenants } from './Tenants';

const sampleTenant: Tenant = {
  id: 'tenant-1',
  name: 'Bank Tenant',
  created_at: '2026-06-01T00:00:00Z',
};

const sampleRemediation: TenantRemediationConfig = {
  TenantID: 'tenant-1',
  MinApprovalSeverity: 'high',
  ChangeWindows: [],
  CriticalOverride: true,
  CircuitBreakerWindowMin: 15,
  CircuitBreakerFailPct: 30,
  CircuitBreakerMinSamples: 5,
};

const emptyPagination = {
  total: 0,
  count: 0,
  limit: 20,
  offset: 0,
  nextOffset: null,
  prevOffset: null,
};

const loadedPagination = {
  total: 1,
  count: 1,
  limit: 20,
  offset: 0,
  nextOffset: null,
  prevOffset: null,
};

const mocks = vi.hoisted(() => {
  const reload = vi.fn();
  const showToast = vi.fn();
  const createTenant = vi.fn();
  const updateTenant = vi.fn();
  const deleteTenant = vi.fn();
  const getTenantRemediationConfig = vi.fn();
  const upsertTenantRemediationConfig = vi.fn();
  const useTenantsResult = {
    data: [] as Tenant[],
    pagination: {
      total: 0,
      count: 0,
      limit: 20,
      offset: 0,
      nextOffset: null as number | null,
      prevOffset: null as number | null,
    },
    loading: false,
    error: null as string | null,
    reload,
  };

  return {
    reload,
    showToast,
    createTenant,
    updateTenant,
    deleteTenant,
    getTenantRemediationConfig,
    upsertTenantRemediationConfig,
    useTenantsResult,
    apiClient: {
      createTenant: (payload: unknown) => createTenant(payload),
      updateTenant: (tenantId: string, payload: unknown) => updateTenant(tenantId, payload),
      deleteTenant: (tenantId: string) => deleteTenant(tenantId),
      getTenantRemediationConfig: (tenantId: string) => getTenantRemediationConfig(tenantId),
      upsertTenantRemediationConfig: (tenantId: string, payload: unknown) =>
        upsertTenantRemediationConfig(tenantId, payload),
    },
  };
});

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

vi.mock('../hooks/useTenants', () => ({
  useTenants: () => mocks.useTenantsResult,
}));

vi.mock('../providers/ToastProvider', () => ({
  useToast: () => ({ showToast: mocks.showToast }),
}));

function setTenantsState({
  data = [sampleTenant],
  error = null,
}: {
  data?: Tenant[];
  error?: string | null;
} = {}) {
  mocks.useTenantsResult.data = data;
  mocks.useTenantsResult.pagination = data.length > 0 ? loadedPagination : emptyPagination;
  mocks.useTenantsResult.loading = false;
  mocks.useTenantsResult.error = error;
  mocks.useTenantsResult.reload = mocks.reload;
}

async function openDeleteDialog(user = userEvent.setup()) {
  renderTenants();

  await user.click(await screen.findByRole('button', { name: 'View' }));
  await user.click(screen.getByRole('button', { name: 'Delete tenant Bank Tenant' }));

  return {
    user,
    dialog: screen.getByRole('dialog', { name: /delete tenant bank tenant/i }),
  };
}

function renderTenants() {
  return render(
    <MemoryRouter>
      <Tenants />
    </MemoryRouter>,
  );
}

describe('Tenants', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setTenantsState();
    mocks.createTenant.mockResolvedValue(sampleTenant);
    mocks.updateTenant.mockResolvedValue(sampleTenant);
    mocks.deleteTenant.mockResolvedValue(undefined);
    mocks.getTenantRemediationConfig.mockResolvedValue(sampleRemediation);
    mocks.upsertTenantRemediationConfig.mockResolvedValue(sampleRemediation);
  });

  it('shows tenant loading failures without also rendering a false empty state', () => {
    setTenantsState({ data: [], error: 'tenant catalog offline' });

    renderTenants();

    expect(screen.getByRole('alert')).toHaveTextContent('Tenants unavailable');
    expect(screen.getByRole('alert')).toHaveTextContent('tenant catalog offline');
    expect(screen.queryByText('No tenants')).not.toBeInTheDocument();
  });

  it('requires app-modal and exact tenant-name confirmation before deleting', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);

    const { dialog } = await openDeleteDialog(user);

    expect(confirmSpy).not.toHaveBeenCalled();
    expect(mocks.deleteTenant).not.toHaveBeenCalled();

    const confirmButton = within(dialog).getByRole('button', { name: 'Delete tenant' });
    expect(confirmButton).toBeDisabled();

    await user.type(within(dialog).getByLabelText('Type tenant name to confirm'), 'Bank');
    expect(confirmButton).toBeDisabled();

    await user.clear(within(dialog).getByLabelText('Type tenant name to confirm'));
    await user.type(within(dialog).getByLabelText('Type tenant name to confirm'), 'Bank Tenant');
    expect(confirmButton).toBeEnabled();

    await user.click(confirmButton);

    await waitFor(() => expect(mocks.deleteTenant).toHaveBeenCalledWith('tenant-1'));
    expect(mocks.showToast).toHaveBeenCalledWith('Tenant "Bank Tenant" deleted.', 'success');
  });

  it('keeps failed tenant deletes visible in the confirmation modal', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    mocks.deleteTenant.mockRejectedValueOnce(new Error('tenant has active nodes'));

    const { dialog } = await openDeleteDialog(user);

    await user.type(within(dialog).getByLabelText('Type tenant name to confirm'), 'Bank Tenant');
    await user.click(within(dialog).getByRole('button', { name: 'Delete tenant' }));

    await waitFor(() => expect(mocks.deleteTenant).toHaveBeenCalledWith('tenant-1'));
    expect(confirmSpy).not.toHaveBeenCalled();
    expect(screen.getByRole('dialog', { name: /delete tenant bank tenant/i })).toBeInTheDocument();
    expect(await within(dialog).findByText('Tenant deletion failed')).toBeInTheDocument();
    expect(within(dialog).getByRole('alert')).toHaveTextContent(
      'Failed to delete tenant Bank Tenant: tenant has active nodes',
    );
    expect(mocks.showToast).toHaveBeenCalledWith(
      'Failed to delete tenant Bank Tenant: tenant has active nodes',
      'error',
    );
  });
});
