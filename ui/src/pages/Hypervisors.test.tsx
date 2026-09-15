import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Hypervisors } from './Hypervisors';
import type {
  CreateHypervisorHostPayload,
  CreateProviderCredentialPayload,
  HypervisorHost,
  ProviderCredential,
} from '../lib/api';

const mocks = vi.hoisted(() => {
  const listProviderCredentials = vi.fn();
  const listHypervisorHosts = vi.fn();
  const createProviderCredential = vi.fn();
  const createHypervisorHost = vi.fn();
  const verifyHypervisorHost = vi.fn();
  const deleteHypervisorHost = vi.fn();
  const deleteProviderCredential = vi.fn();
  const createJob = vi.fn();
  const showToast = vi.fn();

  return {
    listProviderCredentials,
    listHypervisorHosts,
    createProviderCredential,
    createHypervisorHost,
    verifyHypervisorHost,
    deleteHypervisorHost,
    deleteProviderCredential,
    createJob,
    showToast,
    apiClient: {
      listProviderCredentials: (params: unknown) => listProviderCredentials(params),
      listHypervisorHosts: (params: unknown) => listHypervisorHosts(params),
      createProviderCredential: (payload: CreateProviderCredentialPayload) =>
        createProviderCredential(payload),
      createHypervisorHost: (payload: CreateHypervisorHostPayload) =>
        createHypervisorHost(payload),
      verifyHypervisorHost: (id: string) => verifyHypervisorHost(id),
      deleteHypervisorHost: (id: string) => deleteHypervisorHost(id),
      deleteProviderCredential: (id: string) => deleteProviderCredential(id),
      createJob: (payload: unknown) => createJob(payload),
    },
  };
});

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

vi.mock('../hooks/useTenants', () => ({
  useTenants: () => ({
    data: [{ id: 't-1', name: 'Tenant One', created_at: '2026-01-01T00:00:00Z' }],
    loading: false,
    error: null,
    pagination: { total: 1, count: 1, limit: 10, offset: 0, nextOffset: null, prevOffset: null },
    reload: () => undefined,
    refresh: () => undefined,
  }),
}));

vi.mock('../providers/TenantProvider', () => ({
  useTenant: () => ({ currentTenantId: 't-1' }),
}));

vi.mock('../providers/ToastProvider', () => ({
  useToast: () => ({ showToast: mocks.showToast }),
}));

const sampleCredential: ProviderCredential = {
  id: 'cred-1',
  tenant_id: 't-1',
  provider: 'libvirt',
  name: 'kvm-root',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
};

const sampleHost: HypervisorHost = {
  id: 'host-1',
  tenant_id: 't-1',
  provider: 'libvirt',
  name: 'lon-kvm-01',
  endpoint_url: 'qemu+ssh://root@kvm-01/system',
  labels: {},
  health_status: 'unknown',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
};

describe('Hypervisors', () => {
  beforeEach(() => {
    mocks.listProviderCredentials.mockReset();
    mocks.listProviderCredentials.mockResolvedValue({ items: [], pagination: { total: 0 } });
    mocks.listHypervisorHosts.mockReset();
    mocks.listHypervisorHosts.mockResolvedValue({ items: [], pagination: { total: 0 } });
    mocks.createProviderCredential.mockReset();
    mocks.createProviderCredential.mockResolvedValue(sampleCredential);
    mocks.createHypervisorHost.mockReset();
    mocks.createHypervisorHost.mockResolvedValue(sampleHost);
    mocks.verifyHypervisorHost.mockReset();
    mocks.deleteHypervisorHost.mockReset();
    mocks.deleteHypervisorHost.mockResolvedValue(undefined);
    mocks.deleteProviderCredential.mockReset();
    mocks.deleteProviderCredential.mockResolvedValue(undefined);
    mocks.createJob.mockReset();
    mocks.showToast.mockReset();
  });

  it('defaults both enrollment forms to the active tenant', async () => {
    render(<Hypervisors />);

    await waitFor(() => {
      expect(mocks.listProviderCredentials).toHaveBeenCalledWith({ tenantId: 't-1', limit: 200 });
    });

    const tenantSelectors = screen.getAllByRole('combobox', { name: /^Tenant$/i });
    expect(tenantSelectors).toHaveLength(2);
    for (const selector of tenantSelectors) {
      expect(selector).toHaveValue('t-1');
    }
  });

  it('saves provider credentials from the visual provider form', async () => {
    const user = userEvent.setup();
    render(<Hypervisors />);

    await waitFor(() => {
      expect(mocks.listProviderCredentials).toHaveBeenCalledWith({ tenantId: 't-1', limit: 200 });
    });

    const panel = screen.getByRole('heading', { name: /^Provider credential$/i }).closest('section');
    expect(panel).toBeTruthy();
    const credentialPanel = within(panel as HTMLElement);

    await user.selectOptions(credentialPanel.getByLabelText(/provider/i), 'aws');
    await user.type(credentialPanel.getByLabelText(/^Name$/i), 'prod-aws-readonly');
    await user.type(credentialPanel.getByLabelText(/aws access key id/i), 'AKIA_TEST');
    await user.type(credentialPanel.getByLabelText(/aws secret access key/i), 'secret');
    await user.type(credentialPanel.getByLabelText(/default region/i), 'eu-west-2');

    await user.click(credentialPanel.getByRole('button', { name: /save credential/i }));

    await waitFor(() => expect(mocks.createProviderCredential).toHaveBeenCalledTimes(1));
    expect(mocks.createProviderCredential).toHaveBeenCalledWith({
      tenant_id: 't-1',
      provider: 'aws',
      name: 'prod-aws-readonly',
      config: {
        access_key_id: 'AKIA_TEST',
        secret_access_key: 'secret',
        region: 'eu-west-2',
      },
    });
  });

  it('registers a hypervisor host with the active tenant and labels', async () => {
    const user = userEvent.setup();
    render(<Hypervisors />);

    await waitFor(() => {
      expect(mocks.listHypervisorHosts).toHaveBeenCalledWith({ tenantId: 't-1', limit: 200 });
    });

    const panel = screen.getByRole('heading', { name: /register hypervisor host/i }).closest('section');
    expect(panel).toBeTruthy();
    const hostPanel = within(panel as HTMLElement);

    await user.type(hostPanel.getByLabelText(/^Name$/i), 'lon-kvm-01');
    await user.type(hostPanel.getByLabelText(/endpoint url/i), 'qemu+ssh://root@kvm-01/system');
    fireEvent.change(hostPanel.getByLabelText(/labels/i), {
      target: { value: '{"environment":"prod","site":"lon"}' },
    });

    await user.click(hostPanel.getByRole('button', { name: /add host/i }));

    await waitFor(() => expect(mocks.createHypervisorHost).toHaveBeenCalledTimes(1));
    expect(mocks.createHypervisorHost).toHaveBeenCalledWith({
      tenant_id: 't-1',
      provider: 'libvirt',
      name: 'lon-kvm-01',
      endpoint_url: 'qemu+ssh://root@kvm-01/system',
      labels: { environment: 'prod', site: 'lon' },
    });
  });

  it('shows explicit unavailable states when inventory loading fails', async () => {
    mocks.listProviderCredentials.mockRejectedValueOnce(new Error('provider store offline'));

    render(<Hypervisors />);

    expect(await screen.findByText('Hypervisor inventory unavailable')).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('provider store offline');
    expect(screen.getByText('Hypervisor hosts unavailable')).toBeInTheDocument();
    expect(screen.getByText('Provider credentials unavailable')).toBeInTheDocument();
    expect(screen.queryByText('No hypervisor hosts')).not.toBeInTheDocument();
    expect(screen.queryByText('No credentials')).not.toBeInTheDocument();
  });

  it('requires modal confirmation before removing a host and keeps failed removal visible', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    mocks.listProviderCredentials.mockResolvedValue({ items: [sampleCredential], pagination: { total: 1 } });
    mocks.listHypervisorHosts.mockResolvedValue({ items: [sampleHost], pagination: { total: 1 } });
    mocks.deleteHypervisorHost.mockRejectedValueOnce(new Error('delete denied'));

    render(<Hypervisors />);

    await screen.findByText('lon-kvm-01');
    await user.click(screen.getByRole('button', { name: 'Remove hypervisor host lon-kvm-01' }));

    expect(confirmSpy).not.toHaveBeenCalled();
    expect(mocks.deleteHypervisorHost).not.toHaveBeenCalled();

    const dialog = screen.getByRole('dialog', { name: /remove hypervisor host lon-kvm-01/i });
    await user.click(within(dialog).getByRole('button', { name: 'Remove host' }));

    await waitFor(() => expect(mocks.deleteHypervisorHost).toHaveBeenCalledWith('host-1'));
    expect(screen.getByRole('dialog', { name: /remove hypervisor host lon-kvm-01/i })).toBeInTheDocument();
    expect(screen.getByText('Removal failed')).toBeInTheDocument();
    expect(screen.getAllByText('Failed to remove host lon-kvm-01: delete denied').length).toBeGreaterThanOrEqual(2);

    confirmSpy.mockRestore();
  });

  it('requires modal confirmation before deleting a provider credential and keeps failed deletion visible', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    mocks.listProviderCredentials.mockResolvedValue({ items: [sampleCredential], pagination: { total: 1 } });
    mocks.listHypervisorHosts.mockResolvedValue({ items: [sampleHost], pagination: { total: 1 } });
    mocks.deleteProviderCredential.mockRejectedValueOnce(new Error('credential in use'));

    render(<Hypervisors />);

    const deleteCredentialButton = await screen.findByRole('button', { name: 'Delete provider credential kvm-root' });
    await user.click(deleteCredentialButton);

    expect(confirmSpy).not.toHaveBeenCalled();
    expect(mocks.deleteProviderCredential).not.toHaveBeenCalled();

    const dialog = screen.getByRole('dialog', { name: /delete provider credential kvm-root/i });
    await user.click(within(dialog).getByRole('button', { name: 'Delete credential' }));

    await waitFor(() => expect(mocks.deleteProviderCredential).toHaveBeenCalledWith('cred-1'));
    expect(screen.getByRole('dialog', { name: /delete provider credential kvm-root/i })).toBeInTheDocument();
    expect(screen.getByText('Removal failed')).toBeInTheDocument();
    expect(screen.getAllByText('Failed to remove credential kvm-root: credential in use').length).toBeGreaterThanOrEqual(2);

    confirmSpy.mockRestore();
  });
});
