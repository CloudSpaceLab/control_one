import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Access } from './Access';

const mocks = vi.hoisted(() => {
  const listAccessRequests = vi.fn();
  const createAccessRequest = vi.fn();
  const approveAccessRequest = vi.fn();
  const denyAccessRequest = vi.fn();
  const listCommandACLs = vi.fn();
  const createCommandACL = vi.fn();
  const deleteCommandACL = vi.fn();
  const roles = [
    { id: 'role-admin', name: 'admin', description: 'Admin', built_in: true, created_at: '2026-01-01T00:00:00Z' },
    { id: 'role-ciso', name: 'ciso', description: 'CISO', built_in: true, created_at: '2026-01-01T00:00:00Z' },
    { id: 'role-investigator', name: 'investigator', description: 'Investigator', built_in: true, created_at: '2026-01-01T00:00:00Z' },
    { id: 'role-operator', name: 'operator', description: 'Operator', built_in: true, created_at: '2026-01-01T00:00:00Z' },
    { id: 'role-viewer', name: 'viewer', description: 'Viewer', built_in: true, created_at: '2026-01-01T00:00:00Z' },
    { id: 'role-custom', name: 'soc-reviewer', description: 'SOC reviewer', built_in: false, created_at: '2026-01-01T00:00:00Z' },
  ];
  return {
    apiClient: {
      listAccessRequests,
      createAccessRequest,
      approveAccessRequest,
      denyAccessRequest,
      listCommandACLs,
      createCommandACL,
      deleteCommandACL,
    },
    listAccessRequests,
    createAccessRequest,
    listCommandACLs,
    createCommandACL,
    roles,
  };
});

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

vi.mock('../hooks/useTenants', () => ({
  useTenants: () => ({
    data: [{ id: 'tenant-1', name: 'Bank Tenant', created_at: '2026-01-01T00:00:00Z' }],
    loading: false,
    error: null,
    reload: vi.fn(),
  }),
}));

vi.mock('../hooks/useRoles', () => ({
  useRoles: () => ({
    data: mocks.roles,
    loading: false,
    error: null,
    reload: vi.fn(),
  }),
}));

const pagination = { total: 0, count: 0, limit: 100, offset: 0, nextOffset: null, prevOffset: null };

describe('Access', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listAccessRequests.mockResolvedValue({ data: [], pagination });
    mocks.createAccessRequest.mockResolvedValue({});
    mocks.listCommandACLs.mockResolvedValue({
      data: [
        {
          id: 'acl-1',
          tenant_id: 'tenant-1',
          name: 'Block dangerous command',
          roles: ['operator'],
          pattern: '^rm\\s+-rf',
          action: 'deny',
          created_at: '2026-06-06T12:00:00Z',
          updated_at: '2026-06-06T12:00:00Z',
        },
      ],
      pagination,
    });
  });

  it('requires explicit access before submitting a JIT request', async () => {
    render(<Access />);

    const requestButton = await screen.findByRole('button', { name: /request access/i });
    expect(screen.getByLabelText(/requested access/i)).toHaveValue('');
    expect(requestButton).toBeDisabled();

    fireEvent.change(screen.getByLabelText(/requested access/i), {
      target: { value: '  root@prod-db-01  ' },
    });
    fireEvent.change(screen.getByLabelText(/justification/i), {
      target: { value: '  emergency rotation  ' },
    });

    expect(requestButton).toBeEnabled();
    fireEvent.click(requestButton);

    await waitFor(() => expect(mocks.createAccessRequest).toHaveBeenCalledTimes(1));
    expect(mocks.createAccessRequest).toHaveBeenCalledWith(
      expect.objectContaining({
        tenant_id: 'tenant-1',
        requested_access: 'root@prod-db-01',
        justification: 'emergency rotation',
        target_resource_type: 'ssh',
        ttl_seconds: 1800,
      }),
    );
    await waitFor(() => expect(screen.getByLabelText(/requested access/i)).toHaveValue(''));
  });

  it('names command policy delete buttons for assistive technology', async () => {
    const user = userEvent.setup();
    render(<Access />);

    await user.click(await screen.findByRole('tab', { name: /command policy/i }));

    expect(
      await screen.findByRole('button', { name: /delete command policy rule block dangerous command/i }),
    ).toBeInTheDocument();
  });

  it('uses the canonical role API list when creating command policy rules', async () => {
    const user = userEvent.setup();
    mocks.createCommandACL.mockResolvedValue({});
    render(<Access />);

    await user.click(await screen.findByRole('tab', { name: /command policy/i }));
    await user.click(await screen.findByRole('button', { name: /new rule/i }));

    const roleSelect = screen.getByLabelText(/role/i) as HTMLSelectElement;
    expect(Array.from(roleSelect.options).map((option) => option.value)).toEqual([
      'admin',
      'ciso',
      'investigator',
      'operator',
      'viewer',
      'soc-reviewer',
    ]);

    await user.type(screen.getByLabelText(/name/i), 'SOC shell review');
    await user.selectOptions(roleSelect, 'soc-reviewer');
    await user.type(screen.getByLabelText(/regex pattern/i), '^journalctl');
    await user.click(screen.getByRole('button', { name: /^create$/i }));

    await waitFor(() => expect(mocks.createCommandACL).toHaveBeenCalledTimes(1));
    expect(mocks.createCommandACL).toHaveBeenCalledWith(
      expect.objectContaining({
        tenant_id: 'tenant-1',
        name: 'SOC shell review',
        pattern: '^journalctl',
        action: 'deny',
        roles: ['soc-reviewer'],
      }),
    );
  });
});
