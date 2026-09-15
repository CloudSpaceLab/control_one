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
  const defaultSecretGroupsResult = () => ({
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
    error: null as string | null,
    pagination: { total: 1, count: 1, limit: 50, offset: 0 },
    reload: reloadGroups,
  });
  const defaultSecretSyncsResult = () => ({
    data: [],
    loading: false,
    error: null as string | null,
    pagination: { total: 0, count: 0, limit: 20, offset: 0 },
    reload: reloadSyncs,
  });
  return {
    apiClient: {
      createSecretGroup,
      deleteSecretGroup,
      syncSecretGroup,
    },
    createSecretGroup,
    deleteSecretGroup,
    syncSecretGroup,
    reloadGroups,
    reloadSyncs,
    showToast,
    defaultSecretGroupsResult,
    defaultSecretSyncsResult,
    secretGroupsResult: undefined as ReturnType<typeof defaultSecretGroupsResult> | undefined,
    secretSyncsResult: undefined as ReturnType<typeof defaultSecretSyncsResult> | undefined,
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
  useSecretGroups: () => mocks.secretGroupsResult ?? mocks.defaultSecretGroupsResult(),
  useSecretSyncs: () => mocks.secretSyncsResult ?? mocks.defaultSecretSyncsResult(),
}));

describe('Secrets', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.restoreAllMocks();
    mocks.secretGroupsResult = mocks.defaultSecretGroupsResult();
    mocks.secretSyncsResult = mocks.defaultSecretSyncsResult();
    mocks.deleteSecretGroup.mockResolvedValue(undefined);
    mocks.createSecretGroup.mockResolvedValue({});
    mocks.syncSecretGroup.mockResolvedValue(undefined);
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

  it('confirms secret group deletion in-app and refreshes the list', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm');
    const user = userEvent.setup();

    render(<Secrets />);

    await user.click(screen.getByRole('button', { name: /delete secret group prod-vault/i }));
    expect(screen.getByRole('dialog', { name: /delete secret group/i })).toBeInTheDocument();
    expect(screen.getByText(/delete "prod-vault" from vault/i)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /^delete$/i }));

    await waitFor(() => {
      expect(mocks.deleteSecretGroup).toHaveBeenCalledWith('group-1');
    });
    expect(mocks.reloadGroups).toHaveBeenCalled();
    expect(mocks.showToast).toHaveBeenCalledWith('Secret group deleted successfully', 'success');
    expect(confirmSpy).not.toHaveBeenCalled();
  });

  it('keeps a failed delete visible in the confirmation modal', async () => {
    const user = userEvent.setup();
    mocks.deleteSecretGroup.mockRejectedValueOnce(new Error('vault delete unavailable'));

    render(<Secrets />);

    await user.click(screen.getByRole('button', { name: /delete secret group prod-vault/i }));
    await user.click(screen.getByRole('button', { name: /^delete$/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Secret group delete failed: vault delete unavailable',
    );
    expect(screen.getByRole('dialog', { name: /delete secret group/i })).toBeInTheDocument();
    expect(mocks.showToast).toHaveBeenCalledWith('vault delete unavailable', 'error');
  });

  it('names row actions for assistive technology', () => {
    render(<Secrets />);

    expect(screen.getByRole('button', { name: /sync secret group prod-vault/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /delete secret group prod-vault/i })).toBeInTheDocument();
  });

  it('does not show the empty state when secret group loading fails', () => {
    mocks.secretGroupsResult = {
      ...mocks.defaultSecretGroupsResult(),
      data: [],
      error: 'vault list unavailable',
      pagination: { total: 0, count: 0, limit: 50, offset: 0 },
    };

    render(<Secrets />);

    expect(screen.getByText('Failed to load secret groups')).toBeInTheDocument();
    expect(screen.getByText('vault list unavailable')).toBeInTheDocument();
    expect(screen.queryByText('No secret groups found')).not.toBeInTheDocument();
  });

  it('surfaces failed sync requests after the toast disappears', async () => {
    const user = userEvent.setup();
    mocks.syncSecretGroup.mockRejectedValueOnce(new Error('vault queue unavailable'));

    render(<Secrets />);

    await user.click(screen.getByRole('button', { name: /sync secret group prod-vault/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Sync failed for prod-vault: vault queue unavailable',
    );
    expect(mocks.showToast).toHaveBeenCalledWith('vault queue unavailable', 'error');
  });

  it('blocks invalid sync intervals before creating a secret group', async () => {
    const user = userEvent.setup();
    render(<Secrets />);

    await user.click(screen.getByRole('button', { name: /create secret group/i }));
    await user.type(screen.getByLabelText(/name/i), 'prod-secrets');
    const interval = screen.getByLabelText(/sync interval/i);
    await user.clear(interval);
    await user.type(interval, '30');

    expect(screen.getByText(/sync interval must be at least 60 seconds/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^create$/i })).toBeDisabled();
    expect(mocks.createSecretGroup).not.toHaveBeenCalled();
  });
});
