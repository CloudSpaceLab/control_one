import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Users } from './Users';

const mocks = vi.hoisted(() => {
  const reloadUsers = vi.fn();
  const reloadRoles = vi.fn();
  const updateUserRoles = vi.fn();
  const showToast = vi.fn();
  const roles = [
    { id: 'role-viewer', name: 'viewer', description: 'View only', built_in: true, created_at: '2026-01-01T00:00:00Z' },
    { id: 'role-operator', name: 'operator', description: 'Operate fleet', built_in: true, created_at: '2026-01-01T00:00:00Z' },
  ];
  const users = [
    {
      id: '11111111-1111-1111-1111-111111111111',
      external_id: 'admin@local',
      display_name: 'Ada Admin',
      email: 'admin@local',
      roles: ['viewer'],
      created_at: '2026-01-01T00:00:00Z',
    },
  ];
  return {
    apiClient: { updateUserRoles },
    reloadUsers,
    reloadRoles,
    updateUserRoles,
    showToast,
    roles,
    users,
    usersError: null as string | null,
    rolesError: null as string | null,
  };
});

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

vi.mock('../hooks/useUsers', () => ({
  useUsers: () => ({
    data: mocks.users,
    loading: false,
    error: mocks.usersError,
    pagination: { total: mocks.users.length, count: mocks.users.length, limit: 50, offset: 0, nextOffset: null, prevOffset: null },
    reload: mocks.reloadUsers,
  }),
}));

vi.mock('../hooks/useRoles', () => ({
  useRoles: () => ({
    data: mocks.roles,
    loading: false,
    error: mocks.rolesError,
    reload: mocks.reloadRoles,
  }),
}));

vi.mock('../providers/ToastProvider', () => ({
  useToast: () => ({ showToast: mocks.showToast }),
}));

describe('Users', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.users.splice(0, mocks.users.length, {
      id: '11111111-1111-1111-1111-111111111111',
      external_id: 'admin@local',
      display_name: 'Ada Admin',
      email: 'admin@local',
      roles: ['viewer'],
      created_at: '2026-01-01T00:00:00Z',
    });
    mocks.usersError = null;
    mocks.rolesError = null;
    mocks.updateUserRoles.mockResolvedValue({ ...mocks.users[0], roles: ['operator'] });
  });

  it('deduplicates repeated role assignments in the directory', async () => {
    mocks.users[0].roles = ['viewer', 'viewer', 'operator', 'operator'];

    render(<Users />);

    const row = (await screen.findByText('Ada Admin')).closest('tr');
    expect(row).toBeTruthy();
    expect(within(row as HTMLElement).getAllByText('viewer')).toHaveLength(1);
    expect(within(row as HTMLElement).getAllByText('operator')).toHaveLength(1);
  });

  it('does not show a false empty state when users fail to load', async () => {
    mocks.users.splice(0, mocks.users.length);
    mocks.usersError = 'directory unavailable';

    render(<Users />);

    expect(await screen.findByRole('alert')).toHaveTextContent('directory unavailable');
    expect(screen.getByText('Users could not be loaded')).toBeInTheDocument();
    expect(screen.queryByText('No users found')).not.toBeInTheDocument();
  });

  it('prevents saving an empty single-user role set', async () => {
    const user = userEvent.setup();
    render(<Users />);

    await user.click(await screen.findByRole('button', { name: /edit roles/i }));
    const dialog = screen.getByRole('dialog', { name: /edit roles for ada admin/i });

    const viewerCheckbox = within(dialog as HTMLElement).getByRole('checkbox', { name: /viewer/i });
    expect(viewerCheckbox).toBeChecked();
    await user.click(viewerCheckbox);

    expect(
      within(dialog as HTMLElement).getByText(/at least one role is required for console access/i),
    ).toBeInTheDocument();
    expect(within(dialog as HTMLElement).getByRole('button', { name: /save changes/i })).toBeDisabled();
    expect(mocks.updateUserRoles).not.toHaveBeenCalled();
  });

  it('labels bulk updates as role replacement and calls the replacement API once per selected user', async () => {
    const user = userEvent.setup();
    render(<Users />);

    await user.click(await screen.findByLabelText(/select ada admin/i));
    await user.click(screen.getByRole('button', { name: /bulk replace roles/i }));

    const dialog = screen.getByRole('dialog', { name: /bulk replace roles/i });
    expect(
      within(dialog as HTMLElement).getByText(/this replaces existing roles for 1 selected user/i),
    ).toBeInTheDocument();

    await user.click(within(dialog as HTMLElement).getByRole('checkbox', { name: /operator/i }));
    await user.click(within(dialog as HTMLElement).getByRole('button', { name: /replace roles for 1 user/i }));

    await waitFor(() => expect(mocks.updateUserRoles).toHaveBeenCalledTimes(1));
    expect(mocks.updateUserRoles).toHaveBeenCalledWith(
      '11111111-1111-1111-1111-111111111111',
      { roles: ['operator'] },
    );
    await waitFor(() => expect(screen.getByText(/successfully replaced roles for 1 user/i)).toBeInTheDocument());
  });

  it('keeps failed bulk role updates visible in the modal', async () => {
    const user = userEvent.setup();
    mocks.updateUserRoles.mockRejectedValueOnce(new Error('rbac write denied'));

    render(<Users />);

    await user.click(await screen.findByLabelText(/select ada admin/i));
    await user.click(screen.getByRole('button', { name: /bulk replace roles/i }));
    const dialog = screen.getByRole('dialog', { name: /bulk replace roles/i });
    await user.click(within(dialog).getByRole('checkbox', { name: /operator/i }));
    await user.click(within(dialog).getByRole('button', { name: /replace roles for 1 user/i }));

    expect(await within(dialog).findByRole('alert')).toHaveTextContent(
      'Bulk role update failed: rbac write denied',
    );
    expect(screen.getByRole('dialog', { name: /bulk replace roles/i })).toBeInTheDocument();
  });
});
