import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { NetworkDevices } from './NetworkDevices';
vi.mock('../components/NetworkTelemetryPanel', () => ({ NetworkTelemetryPanel: () => null }));

const mocks = vi.hoisted(() => ({
  list: vi.fn(), create: vi.fn(), get: vi.fn(), toast: vi.fn(),
  tenant: 'tenant-a' as string | null, permissions: ['targets.read', 'targets.write'],
}));
vi.mock('../hooks/useApiClient', () => ({ useApiClient: () => ({ listNetworkTargets: mocks.list, createNetworkTarget: mocks.create, getNetworkTarget: mocks.get }) }));
vi.mock('../providers/AuthProvider', () => ({ useAuth: () => ({ profile: { roles: ['viewer'], permissions: mocks.permissions } }) }));
vi.mock('../providers/TenantProvider', () => ({ useTenant: () => ({ currentTenantId: mocks.tenant, tenants: [{ id: 'tenant-a', name: 'Lagos' }, { id: 'tenant-b', name: 'Abuja' }] }) }));
vi.mock('../providers/ToastProvider', () => ({ useToast: () => ({ showToast: mocks.toast }) }));
vi.mock('../components/NetworkInventoryPanel', () => ({ NetworkInventoryPanel: () => <div>Inventory panel</div> }));

const device = {
  id: 'switch-1', tenant_id: 'tenant-a', family: 'network_security', type: 'switch',
  display_name: 'Branch switch', site: 'Lagos', group: 'Branch', capabilities: [],
  lifecycle_state: 'discovered', reachability_state: 'unknown', collection_state: 'not_configured',
  addresses: [{ address: '192.0.2.30', purpose: 'management', current: true, source: 'operator', confidence: 100 }],
  classification: { source: 'operator', confidence: 100, evidence: ['Operator-selected type; not protocol verified'] },
};
function CurrentUrl() { const location = useLocation(); return <output data-testid="current-url">{location.search}</output>; }
function mount(initialEntries = ['/network-devices']) {
  return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><MemoryRouter initialEntries={initialEntries}><CurrentUrl /><NetworkDevices /></MemoryRouter></QueryClientProvider>);
}
beforeEach(() => {
  vi.clearAllMocks(); mocks.tenant = 'tenant-a'; mocks.permissions = ['targets.read', 'targets.write'];
  mocks.list.mockResolvedValue({ data: [device], pagination: { total: 1, count: 1, limit: 20, offset: 0, nextOffset: null, prevOffset: null } });
  mocks.create.mockResolvedValue(device); mocks.get.mockResolvedValue(device);
});
describe('Network device workflow', () => {
  it('saves an agentless identity through the form and displays unverified evidence', async () => {
    const user = userEvent.setup(); mount();
    await user.click(screen.getByRole('button', { name: 'Add network device' }));
    await user.type(screen.getByLabelText('Device name'), 'Branch switch');
    await user.type(screen.getByLabelText('Management address'), '192.0.2.30');
    await user.type(screen.getByLabelText('Site (optional)'), 'Lagos');
    await user.click(screen.getByRole('button', { name: 'Save device' }));
    await waitFor(() => expect(mocks.create).toHaveBeenCalledWith({ tenant_id: 'tenant-a', type: 'switch', display_name: 'Branch switch', hostname: '', site: 'Lagos', group: '', management_addresses: ['192.0.2.30'] }));
    expect(await screen.findByText('Connection not verified')).toBeInTheDocument();
    expect(screen.getByText('Agentless')).toBeInTheDocument();
    expect(screen.getByText('Operator-selected type; not protocol verified')).toBeInTheDocument();
  });
  it('retains the form and surfaces a failed save without claiming success', async () => {
    mocks.create.mockRejectedValue(new Error('management address must be an IP address or DNS name'));
    const user = userEvent.setup(); mount();
    await user.click(screen.getByRole('button', { name: 'Add network device' }));
    await user.type(screen.getByLabelText('Device name'), 'Branch switch');
    await user.type(screen.getByLabelText('Management address'), 'https://switch');
    await user.click(screen.getByRole('button', { name: 'Save device' }));
    expect(await screen.findByText('management address must be an IP address or DNS name')).toBeInTheDocument();
    expect(screen.getByLabelText('Device name')).toHaveValue('Branch switch');
    expect(mocks.toast).not.toHaveBeenCalled();
  });
  it('uses exact server pagination and passes All tenants and filters to the API', async () => {
    mocks.tenant = null;
    mocks.list.mockResolvedValue({ data: [device], pagination: { total: 25, count: 1, limit: 20, offset: 0 } });
    const user = userEvent.setup(); mount();
    await waitFor(() => expect(mocks.list).toHaveBeenCalledWith(expect.objectContaining({ tenantId: undefined, offset: 0 })));
    await user.click(screen.getByRole('button', { name: 'Next' }));
    await waitFor(() => expect(mocks.list).toHaveBeenCalledWith(expect.objectContaining({ offset: 20 })));
    await user.selectOptions(screen.getByLabelText('Device type'), 'firewall');
    await waitFor(() => expect(mocks.list).toHaveBeenCalledWith(expect.objectContaining({ offset: 0, type: 'firewall' })));
    await user.type(screen.getByLabelText('Vendor'), 'Cisco');
    await waitFor(() => expect(mocks.list).toHaveBeenCalledWith(expect.objectContaining({ vendor: 'Cisco', tenantId: undefined })));
    await user.selectOptions(screen.getByLabelText('Telemetry / collection state'), 'stale');
    await waitFor(() => expect(mocks.list).toHaveBeenCalledWith(expect.objectContaining({ collectionState: 'stale', offset: 0 })));
    expect(screen.getByTestId('current-url')).toHaveTextContent('vendor=Cisco');
    expect(screen.getByTestId('current-url')).toHaveTextContent('collection_state=stale');
  });
  it('opens related workflows from a target detail deep link', async () => {
    mount(['/network-devices?device=switch-1']);
    expect(await screen.findByText('Agentless')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Search events' })).toHaveAttribute('href', '/search?q=Branch%20switch');
    expect(screen.getByRole('link', { name: 'Open cases' })).toHaveAttribute('href', '/cases');
    expect(screen.getByRole('link', { name: 'Observability' })).toHaveAttribute('href', '/observability');
  });
  it('shows stale and unverified state as actionable inventory attention', async () => {
    mocks.list.mockResolvedValue({ data: [
      { ...device, id: 'stale-switch', reachability_state: 'reachable', collection_state: 'stale' },
      { ...device, id: 'unverified-switch', reachability_state: 'unknown', collection_state: 'discovered' },
    ], pagination: { total: 2, count: 2, limit: 20, offset: 0 } });
    mount();
    expect(await screen.findByText('Collection stale')).toBeInTheDocument();
    expect(screen.getAllByText('Not verified').length).toBeGreaterThan(0);
  });
  it('shows inventory but hides creation from users without write permission', async () => {
    mocks.permissions = ['targets.read']; mount();
    expect(await screen.findByRole('button', { name: 'Branch switch' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Add network device' })).not.toBeInTheDocument();
    expect(mocks.list).toHaveBeenCalledWith(expect.objectContaining({ tenantId: 'tenant-a' }));
  });
});
