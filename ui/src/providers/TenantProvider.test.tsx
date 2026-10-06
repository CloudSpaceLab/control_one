import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { TenantProvider, useTenant } from './TenantProvider';

const mocks = vi.hoisted(() => ({
  listTenants: vi.fn(),
}));

vi.mock('./AuthProvider', () => ({
  useAuth: () => ({
    apiClient: { listTenants: mocks.listTenants },
    isAuthenticated: true,
  }),
}));

function Consumer() {
  const { currentTenantId, currentTenant, tenants, setCurrentTenantId } = useTenant();
  return (
    <div>
      <span data-testid="scope">{currentTenantId ?? 'all'}</span>
      <span data-testid="tenant-name">{currentTenant?.name ?? 'none'}</span>
      <span data-testid="tenant-count">{tenants.length}</span>
      <button type="button" onClick={() => setCurrentTenantId(null)}>All tenants</button>
    </div>
  );
}

function renderProvider() {
  return render(
    <TenantProvider>
      <Consumer />
    </TenantProvider>,
  );
}

describe('TenantProvider scope persistence', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    window.localStorage.clear();
    mocks.listTenants.mockResolvedValue({
      data: [
        { id: 'tenant-1', name: 'Bank A', created_at: '2026-01-01T00:00:00Z' },
        { id: 'tenant-2', name: 'Bank B', created_at: '2026-01-01T00:00:00Z' },
      ],
      pagination: { total: 2, count: 2, limit: 200, offset: 0, nextOffset: null, prevOffset: null },
    });
  });

  it('restores an explicit All tenants selection instead of auto-selecting the first tenant', async () => {
    window.localStorage.setItem('co.tenant.id', '__all__');

    renderProvider();

    await waitFor(() => expect(screen.getByTestId('tenant-count')).toHaveTextContent('2'));
    expect(screen.getByTestId('scope')).toHaveTextContent('all');
    expect(screen.getByTestId('tenant-name')).toHaveTextContent('none');
    expect(window.localStorage.getItem('co.tenant.id')).toBe('__all__');
  });

  it('keeps the existing first-tenant default when no scope was previously stored', async () => {
    renderProvider();

    await waitFor(() => expect(screen.getByTestId('scope')).toHaveTextContent('tenant-1'));
    expect(screen.getByTestId('tenant-name')).toHaveTextContent('Bank A');
    expect(window.localStorage.getItem('co.tenant.id')).toBe('tenant-1');
  });

  it('persists All tenants when the user selects it', async () => {
    const user = userEvent.setup();
    renderProvider();

    await waitFor(() => expect(screen.getByTestId('scope')).toHaveTextContent('tenant-1'));
    await user.click(screen.getByRole('button', { name: 'All tenants' }));

    expect(screen.getByTestId('scope')).toHaveTextContent('all');
    expect(window.localStorage.getItem('co.tenant.id')).toBe('__all__');
  });
});
