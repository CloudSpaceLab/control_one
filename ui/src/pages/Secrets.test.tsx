import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Secrets } from './Secrets';

const mocks = vi.hoisted(() => {
  const deleteSecretGroup = vi.fn();
  const createSecretGroup = vi.fn();
  const syncSecretGroup = vi.fn();
  const reloadGroups = vi.fn();
  const reloadSyncs = vi.fn();
  const showToast = vi.fn();
  return {
    apiClient: {
      createSecretGroup,
      deleteSecretGroup,
      syncSecretGroup,
    },
    deleteSecretGroup,
    reloadGroups,
    reloadSyncs,
    showToast,
  };
});

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

vi.mock('../hooks/useTenants', () => ({
  useTenants: () => ({ data: [], loading: false, error: null }),
}));

vi.mock('../providers/TenantProvider', () => ({
  useTenant: () => ({ currentTenantId: 'tenant-a' }),
}));

vi.mock('../providers/ToastProvider', () => ({
  useToast: () => ({
    toasts: [],
    showToast: mocks.showToast,
    dismissToast: vi.fn(),
  }),
}));

vi.mock('../hooks/useSecrets', () => ({
  useSecretGroups: () => ({
    data: [
      {
        id: 'group-1',
        tenant_id: 'tenant-a',
        name: 'prod-vault',
        backend: 'vault',
        sync_status: 'synced',
        created_at: '2026-06-07T00:00:00Z',
        updated_at: '2026-06-07T00:00:00Z',
      },
    ],
    loading: false,
    error: null,
    pagination: { total: 1, count: 1, limit: 50, offset: 0 },
    reload: mocks.reloadGroups,
  }),
  useSecretSyncs: () => ({
    data: [],
    loading: false,
    error: null,
    pagination: { total: 0, count: 0, limit: 20, offset: 0 },
    reload: mocks.reloadSyncs,
  }),
}));

describe('Secrets', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.deleteSecretGroup.mockResolvedValue(undefined);
  });

  it('renders ASCII-safe operational copy', () => {
    const { container } = render(<Secrets />);
    const forbiddenGlyphs = [String.fromCharCode(183), String.fromCharCode(8212), String.fromCharCode(215)];

    expect(screen.getByText('POSTURE / SECRETS')).toBeInTheDocument();
    expect(screen.getByText('SECRET GROUPS / 1 of 1')).toBeInTheDocument();
    for (const glyph of forbiddenGlyphs) {
      expect(container.textContent).not.toContain(glyph);
    }
  });

  it('calls the secret group delete endpoint and refreshes the list', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    const user = userEvent.setup();

    render(<Secrets />);

    await user.click(screen.getByRole('button', { name: /delete/i }));

    await waitFor(() => {
      expect(mocks.deleteSecretGroup).toHaveBeenCalledWith('group-1');
    });
    expect(mocks.reloadGroups).toHaveBeenCalled();
    expect(mocks.showToast).toHaveBeenCalledWith('Secret group deleted successfully', 'success');

    confirmSpy.mockRestore();
  });
});
