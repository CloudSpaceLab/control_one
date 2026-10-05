import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, expect, it, vi } from 'vitest';
import { NetworkObservabilityPanel, NetworkTelemetryPanel } from './NetworkTelemetryPanel';
const mocks = vi.hoisted(() => ({ get: vi.fn(), save: vi.fn(), list: vi.fn() }));
vi.mock('../hooks/useApiClient', () => ({ useApiClient: () => ({ getNetworkTelemetry: mocks.get, configureNetworkSource: mocks.save, listNetworkTargets: mocks.list }) }));
beforeEach(() => { vi.clearAllMocks(); mocks.get.mockResolvedValue({ sources: [{ id: 'binding', target_id: 'target', tenant_id: 'tenant', source_type: 'snmp_poll', state: 'ready', collector_id: 'edge-lagos', site: 'Lagos', collector_status: 'stale', last_contact_at: '2026-10-05T09:00:00Z', queue_depth: 4, lag_millis: 30, stale_after_seconds: 300 }] }); mocks.save.mockResolvedValue(undefined); });
function mount(canConfigure = false) { return render(<MemoryRouter><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><NetworkTelemetryPanel targetId="target" tenantId="tenant" canConfigure={canConfigure} /></QueryClientProvider></MemoryRouter>); }
it('shows independent source and collector states and unconfigured flow', async () => {
  mount(); expect(await screen.findByText('SNMP polling · ready')).toBeInTheDocument();
  expect(screen.getByText(/Collector state: stale/)).toBeInTheDocument();
  expect(screen.getByText('NetFlow · not configured')).toBeInTheDocument();
  expect(screen.getByText(/Assigned site: Lagos/)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Configure source binding' })).not.toBeInTheDocument();
});
it('saves a tenant collector binding through the UI without credentials', async () => {
  const user = userEvent.setup(); mount(true); await screen.findByText('SNMP polling · ready');
  await user.click(screen.getByRole('button', { name: 'Configure source binding' }));
  await user.type(screen.getByLabelText('Collector ID'), 'edge-lagos');
  await user.type(screen.getByLabelText('Transport sender IP'), '192.0.2.10');
  await user.click(screen.getByRole('button', { name: 'Save source binding' }));
  expect(mocks.save).toHaveBeenCalledWith('target', { source_type: 'syslog', collector_id: 'edge-lagos', site: '', sender_address: '192.0.2.10', stale_after_seconds: 300 });
});

it('offers all protocols and requires transport identity only for receivers', async () => {
  const user = userEvent.setup(); mount(true); await screen.findByText('SNMP polling · ready');
  await user.click(screen.getByRole('button', { name: 'Configure source binding' }));
  expect(screen.getAllByRole('option')).toHaveLength(10);
  await user.selectOptions(screen.getByLabelText('Source'), 'netflow');
  await user.type(screen.getByLabelText('Collector ID'), 'flow-lagos');
  await user.type(screen.getByLabelText('Transport sender IP'), '192.0.2.10');
  await user.click(screen.getByRole('button', { name: 'Save source binding' }));
  expect(mocks.save).toHaveBeenCalledWith('target', expect.objectContaining({ source_type: 'netflow', sender_address: '192.0.2.10' }));
  await user.click(screen.getByRole('button', { name: 'Configure source binding' }));
  await user.selectOptions(screen.getByLabelText('Source'), 'netconf');
  expect(screen.queryByLabelText('Transport sender IP')).not.toBeInTheDocument();
  await user.click(screen.getByRole('button', { name: 'Save source binding' }));
  expect(mocks.save).toHaveBeenLastCalledWith('target', expect.objectContaining({ source_type: 'netconf', sender_address: '' }));
});

it('shows network sources in Observability with authorized All tenants pagination', async () => {
  const user = userEvent.setup();
  mocks.list.mockImplementation(({ offset }: { offset: number }) => Promise.resolve({
    data: [{ id: offset === 0 ? 'target-a' : 'target-b', tenant_id: offset === 0 ? 'tenant-a' : 'tenant-b', display_name: offset === 0 ? 'Lagos switch' : 'Abuja router', reachability_state: 'reachable', site: offset === 0 ? 'Lagos' : 'Abuja' }],
    pagination: { total: 11 },
  }));
  render(<MemoryRouter><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><NetworkObservabilityPanel /></QueryClientProvider></MemoryRouter>);
  expect(await screen.findByText(/Lagos switch · Reachability: reachable/)).toBeInTheDocument();
  expect(screen.getByText(/All authorized tenants/)).toBeInTheDocument();
  expect(await screen.findByText('SNMP polling · ready')).toBeInTheDocument();
  expect(screen.getByText(/Collector state: stale/)).toBeInTheDocument();
  expect(screen.getByText('NetFlow · not configured')).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Open device' })).toHaveAttribute('href', '/network-devices?device=target-a');
  expect(mocks.list).toHaveBeenCalledWith({ tenantId: undefined, limit: 10, offset: 0 });
  await user.click(screen.getByRole('button', { name: 'Next network devices' }));
  expect(await screen.findByText(/Abuja router · Reachability: reachable/)).toBeInTheDocument();
  await waitFor(() => expect(mocks.get).toHaveBeenCalledWith('target-b'));
  expect(mocks.list).toHaveBeenCalledWith({ tenantId: undefined, limit: 10, offset: 10 });
  expect(screen.getByRole('button', { name: 'Next network devices' })).toBeDisabled();
});

it('does not claim network source readiness when the Observability target read fails', async () => {
  mocks.list.mockRejectedValue(new Error('access denied'));
  render(<MemoryRouter><QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><NetworkObservabilityPanel tenantId="tenant" /></QueryClientProvider></MemoryRouter>);
  expect(await screen.findByRole('alert')).toHaveTextContent('Network source inventory unavailable.');
  expect(screen.queryByText('SNMP polling · ready')).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Next network devices' })).toBeDisabled();
  expect(mocks.list).toHaveBeenCalledWith({ tenantId: 'tenant', limit: 10, offset: 0 });
});
