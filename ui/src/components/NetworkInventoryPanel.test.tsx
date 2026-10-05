import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { NetworkInventoryPanel } from './NetworkInventoryPanel';

const mocks = vi.hoisted(() => ({ get: vi.fn(), refresh: vi.fn() }));
vi.mock('../hooks/useApiClient', () => ({ useApiClient: () => ({ getNetworkInventory: mocks.get, refreshNetworkInventory: mocks.refresh }) }));
const observed = '2026-10-02T12:00:00Z';
const snapshot = { state: 'inventory_ready', adapter: 'synthetic fixture', observed_at: observed, facts: { hostname: { value: 'fixture-switch', protocol: 'snmpv3', source: 'sysName', observed_at: observed } }, interfaces: [{ id: '7', facts: { name: { value: 'port7', protocol: 'snmpv3', source: 'ifName', observed_at: observed } } }], neighbors: [], entities: [], addresses: [], vlans: [], arp: [], routes: [], resources: [], raw_evidence: [], unavailable: ['HA: no adapter'] };
beforeEach(() => { vi.clearAllMocks(); mocks.get.mockResolvedValue({ target_id: 'device-1', state: 'not_collected' }); mocks.refresh.mockResolvedValue({ target_id: 'device-1', state: 'inventory_ready', snapshot }); });
function mount(canRefresh = true, supportsSNMP = true) {
  return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><NetworkInventoryPanel targetId="device-1" canRefresh={canRefresh} supportsSNMP={supportsSNMP} /></QueryClientProvider>);
}
describe('Network inventory detail', () => {
  it('refreshes through the target endpoint and displays collected facts with provenance', async () => {
    const user = userEvent.setup(); mount();
    expect(await screen.findByText(/No successful inventory collection recorded/)).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Refresh inventory' }));
    expect(await screen.findByText('fixture-switch')).toBeInTheDocument();
    expect(mocks.refresh).toHaveBeenCalledWith('device-1');
    expect(screen.getByText(/snmpv3 · sysName/)).toBeInTheDocument();
    await user.click(screen.getByText('Interfaces (1)'));
    expect(screen.getByText('port7')).toBeInTheDocument();
    expect(screen.getByText(/recurring polling and telemetry are not enabled/)).toBeInTheDocument();
  });
  it('preserves old facts after failed refresh and does not claim a new observation', async () => {
    mocks.get.mockResolvedValue({ target_id: 'device-1', state: 'inventory_ready', snapshot });
    mocks.refresh.mockResolvedValue({ target_id: 'device-1', state: 'auth_failed', snapshot });
    const user = userEvent.setup(); mount();
    await screen.findByText('fixture-switch');
    await user.click(screen.getByRole('button', { name: 'Refresh inventory' }));
    expect(await screen.findByText('Previous snapshot')).toBeInTheDocument();
    expect(screen.getByText(/Latest attempt: auth failed/)).toBeInTheDocument();
    expect(screen.getByText('fixture-switch')).toBeInTheDocument();
    expect(screen.getByText(/retain their previous observation times/)).toBeInTheDocument();
  });
  it('hides refresh for read-only users and disables unsupported SSH collection', async () => {
    const view = mount(false, false);
    await screen.findByText(/No successful inventory/);
    expect(screen.queryByRole('button', { name: 'Refresh inventory' })).not.toBeInTheDocument();
    view.unmount(); mount(true, false);
    expect(screen.getByRole('button', { name: 'Refresh inventory' })).toBeDisabled();
    expect(screen.getByText(/SSH inventory commands are not implemented/)).toBeInTheDocument();
  });
});
