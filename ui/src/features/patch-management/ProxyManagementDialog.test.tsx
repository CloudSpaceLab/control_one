import {render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach, describe, expect, it, vi} from 'vitest';
import {ProxyManagementDialog} from './ProxyManagementDialog';
import type {SquidProxy} from '../../lib/api';
const mocks = vi.hoisted(() => ({getProxyManagement: vi.fn(), listNodes: vi.fn(), bindProxyManagement: vi.fn(), requestProxyLifecycle: vi.fn()}));
vi.mock('../../hooks/useApiClient', () => ({useApiClient: () => mocks}));
const proxy = {ID: 'proxy', TenantID: 'tenant', Host: 'ubuntu-lab', Port: 3128, Status: 'registered', Whitelist: ['archive.ubuntu.com']} as SquidProxy;
const policy = {proxy_id: 'proxy', node_id: 'node', revision: 3, client_cidrs: ['10.20.30.0/24'], probe_urls: ['https://archive.ubuntu.com/']};
describe('managed proxy workflow', () => {
  beforeEach(() => {
    vi.resetAllMocks();
    mocks.listNodes.mockResolvedValue({data: [{id: 'node', tenant_id: 'tenant', hostname: 'ubuntu-lab'}, {id: 'wrong', tenant_id: 'other', hostname: 'ubuntu-lab'}]});
    mocks.getProxyManagement.mockResolvedValue({management: policy, job: null, eligible: true, reason: 'Qualified software capability'});
    mocks.requestProxyLifecycle.mockResolvedValue({job_id: 'job', status: 'running'});
  });
  const open = () => render(<ProxyManagementDialog proxy={proxy} tenantId="tenant" onClose={vi.fn()} onChanged={vi.fn()}/>);
  it('requires explicit review and sends the immutable reviewed revision and scope', async () => {
    const user = userEvent.setup(); open();
    await user.click(await screen.findByRole('button', {name: 'Review reconfiguration'}));
    expect(mocks.requestProxyLifecycle).not.toHaveBeenCalled();
    expect(screen.getByText('Host: node · base revision 3')).toBeInTheDocument();
    expect(screen.getByText('Clients: 10.20.30.0/24')).toBeInTheDocument();
    await user.click(screen.getByRole('button', {name: 'Submit reviewed change'}));
    await waitFor(() => expect(mocks.requestProxyLifecycle).toHaveBeenCalledWith('proxy', 'reconfigure', {expected_revision: 3, whitelist: ['archive.ubuntu.com'], client_cidrs: policy.client_cidrs, probe_urls: policy.probe_urls}));
  });
  it('blocks actions on an unsupported host without claiming queued success', async () => {
    mocks.getProxyManagement.mockResolvedValue({management: policy, job: null, eligible: false, reason: 'systemd capability unavailable'});
    open(); expect(await screen.findByText('systemd capability unavailable')).toBeInTheDocument();
    expect(screen.getByRole('button', {name: 'Review reconfiguration'})).toBeDisabled();
    expect(screen.getByRole('button', {name: 'Review removal'})).toBeDisabled();
    expect(mocks.requestProxyLifecycle).not.toHaveBeenCalled();
  });
  it('keeps API failures visible and disables submission', async () => {
    mocks.getProxyManagement.mockRejectedValue(new Error('management evidence unavailable'));
    open(); expect(await screen.findByText('management evidence unavailable')).toBeInTheDocument();
    expect(screen.queryByRole('button', {name: 'Review reconfiguration'})).not.toBeInTheDocument();
  });
  it('offers only matching tenant-owned enrollment targets for binding', async () => {
    mocks.getProxyManagement.mockResolvedValue({management: null, job: null, eligible: false, reason: 'Bind host first'});
    mocks.bindProxyManagement.mockResolvedValue({management: policy});
    const user = userEvent.setup();open();
    const select = await screen.findByRole('combobox', {name: 'Enrolled management host'});
    await waitFor(() => expect(screen.getAllByRole('option')).toHaveLength(2));
    await user.selectOptions(select, 'node');
    await user.click(screen.getByRole('button', {name: 'Bind management host'}));
    await waitFor(() => expect(mocks.bindProxyManagement).toHaveBeenCalledWith('proxy', {node_id: 'node', client_cidrs: ['127.0.0.1/32'], probe_urls: ['https://archive.ubuntu.com/']}));
  });
  it('shows pending work without enabling a conflicting request', async () => {
    mocks.getProxyManagement.mockResolvedValue({management: policy, job: {id: 'job', status: 'running'}, eligible: true, reason: 'Supported'});
    open(); expect(await screen.findByText('Result: running')).toBeInTheDocument();
    expect(screen.getByRole('button', {name: 'Review reconfiguration'})).toBeDisabled();
    expect(screen.getByText(/Do not infer that the change has been applied/)).toBeInTheDocument();
  });
});
