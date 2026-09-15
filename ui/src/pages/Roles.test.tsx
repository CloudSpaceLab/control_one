import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Roles } from './Roles';

const mocks = vi.hoisted(() => {
  const listPermissions = vi.fn();
  const listRolesWithPermissions = vi.fn();
  const setRolePermissions = vi.fn();
  const createCustomRole = vi.fn();
  const deleteRole = vi.fn();
  return {
    apiClient: {
      listPermissions,
      listRolesWithPermissions,
      setRolePermissions,
      createCustomRole,
      deleteRole,
    },
    listPermissions,
    listRolesWithPermissions,
    setRolePermissions,
    createCustomRole,
    deleteRole,
  };
});

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

describe('Roles', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listPermissions.mockResolvedValue([
      { name: 'roles.read', description: 'View roles', category: 'rbac' },
      { name: 'roles.write', description: 'Edit roles', category: 'rbac' },
    ]);
    mocks.listRolesWithPermissions.mockResolvedValue([
      { id: 'role-admin-live', name: 'admin', description: 'Admin', permissions: ['roles.read'], built_in: true },
      { id: 'role-custom-live', name: 'soc-reviewer', description: 'SOC reviewer', permissions: ['roles.read'] },
    ]);
    mocks.setRolePermissions.mockResolvedValue(undefined);
    mocks.createCustomRole.mockResolvedValue({
      id: 'role-created',
      name: 'risk-analyst',
      description: 'Risk analyst',
      permissions: [],
    });
    mocks.deleteRole.mockResolvedValue(undefined);
  });

  it('treats canonical built-in role names as protected even when IDs vary', async () => {
    mocks.listRolesWithPermissions.mockResolvedValue([
      { id: 'role-admin-live', name: 'admin', description: 'Admin', permissions: ['roles.read'], built_in: true },
      { id: 'role-ciso-live', name: 'ciso', description: 'CISO', permissions: ['roles.read'], built_in: true },
      { id: 'role-investigator-live', name: 'investigator', description: 'Investigator', permissions: [], built_in: true },
      { id: 'role-operator-live', name: 'operator', description: 'Operator', permissions: [], built_in: true },
      { id: 'role-viewer-live', name: 'viewer', description: 'Viewer', permissions: ['roles.read'], built_in: true },
      { id: 'role-custom-live', name: 'soc-reviewer', description: 'SOC reviewer', permissions: [] },
    ]);

    render(<Roles />);

    await waitFor(() => expect(screen.getByRole('heading', { name: /roles & permissions/i })).toBeInTheDocument());

    expect(screen.getAllByText('built-in')).toHaveLength(5);
    expect(screen.getAllByRole('button', { name: /delete/i })).toHaveLength(1);
    expect(screen.getAllByRole('button', { name: /delete/i })[0]).toHaveTextContent('delete');
  });

  it('keeps built-in name fallback for older role API payloads', async () => {
    mocks.listRolesWithPermissions.mockResolvedValue([
      { id: 'random-admin-id', name: 'admin', description: 'Admin', permissions: ['roles.read'] },
      { id: 'random-viewer-id', name: 'viewer', description: 'Viewer', permissions: ['roles.read'] },
    ]);

    render(<Roles />);

    await waitFor(() => expect(screen.getByRole('heading', { name: /roles & permissions/i })).toBeInTheDocument());

    expect(screen.getAllByText('built-in')).toHaveLength(2);
    expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument();
  });

  it('renders built-in role permissions as read-only baselines', async () => {
    const user = userEvent.setup();

    render(<Roles />);

    const included = await screen.findByRole('checkbox', {
      name: 'Built-in role admin includes roles.read',
    });
    const missing = screen.getByRole('checkbox', {
      name: 'Built-in role admin does not include roles.write',
    });
    const custom = screen.getByRole('checkbox', { name: /grant roles.write for soc-reviewer/i });

    expect(included).toBeChecked();
    expect(included).toBeDisabled();
    expect(missing).not.toBeChecked();
    expect(missing).toBeDisabled();
    expect(custom).toBeEnabled();

    await user.click(included);
    await user.click(missing);

    expect(mocks.setRolePermissions).not.toHaveBeenCalled();
  });

  it('does not show a false empty state when role data fails to load', async () => {
    mocks.listRolesWithPermissions.mockRejectedValueOnce(new Error('role catalog unavailable'));

    render(<Roles />);

    expect(await screen.findByRole('alert')).toHaveTextContent('role catalog unavailable');
    expect(screen.getByText('Roles could not be loaded')).toBeInTheDocument();
    expect(screen.queryByText('No roles yet')).not.toBeInTheDocument();
  });

  it('rolls back failed permission changes and keeps the error visible', async () => {
    const user = userEvent.setup();
    mocks.setRolePermissions.mockRejectedValueOnce(new Error('permission denied'));

    render(<Roles />);

    const checkbox = await screen.findByRole('checkbox', { name: /grant roles.write for soc-reviewer/i });
    expect(checkbox).not.toBeChecked();
    await user.click(checkbox);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Permission update failed for soc-reviewer: permission denied',
    );
    expect(screen.getByRole('checkbox', { name: /grant roles.write for soc-reviewer/i })).not.toBeChecked();
    expect(mocks.setRolePermissions).toHaveBeenCalledWith('role-custom-live', ['roles.read', 'roles.write']);
  });

  it('keeps failed custom role deletes in the confirmation modal', async () => {
    const user = userEvent.setup();
    mocks.deleteRole.mockRejectedValueOnce(new Error('role still assigned'));

    render(<Roles />);

    await user.click(await screen.findByRole('button', { name: /delete custom role soc-reviewer/i }));
    const dialog = screen.getByRole('dialog', { name: /delete custom role/i });
    expect(dialog).toHaveTextContent('soc-reviewer');
    await user.click(within(dialog).getByRole('button', { name: /delete role/i }));

    expect(await within(dialog).findByRole('alert')).toHaveTextContent(
      'Role delete failed: role still assigned',
    );
    expect(screen.getByRole('dialog', { name: /delete custom role/i })).toBeInTheDocument();
  });

  it('surfaces custom role create failures without using browser alerts', async () => {
    const user = userEvent.setup();
    const alertSpy = vi.spyOn(window, 'alert').mockImplementation(() => undefined);
    mocks.createCustomRole.mockRejectedValueOnce(new Error('duplicate role name'));

    render(<Roles />);

    await user.click(screen.getByRole('button', { name: /new custom role/i }));
    await user.type(screen.getByLabelText(/role name/i), 'soc-reviewer');
    await user.click(screen.getByRole('button', { name: /create role/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent('duplicate role name');
    expect(alertSpy).not.toHaveBeenCalled();
    alertSpy.mockRestore();
  });
});
