import { render, screen, waitFor } from '@testing-library/react';
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
});
