import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { NodePatchState } from '../lib/api';
import { DeploymentNodeDetail, NodeConfigEditor } from './PatchManagement';

const api = vi.hoisted(() => ({ listPatchDeploymentNodes: vi.fn(), getNodePatchConfig: vi.fn(), listSquidProxies: vi.fn(), upsertNodePatchConfig: vi.fn() }));
vi.mock('../hooks/useApiClient', () => ({ useApiClient: () => api }));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const node: NodePatchState = { ID: 'state-1', DeploymentID: 'deployment-1', NodeID: 'node-1', TenantID: 'tenant-1', Status: 'applied', RequestedAt: '' };
beforeEach(() => {
  vi.clearAllMocks();
  api.listPatchDeploymentNodes.mockResolvedValue({ rows: [{ ...node, PackagesUpgraded: 0 }] });
  api.getNodePatchConfig.mockResolvedValue({ NodeID: node.NodeID, Mode: 'direct', WindowID: 'window-1', UpdatedAt: '' });
  api.listSquidProxies.mockResolvedValue({ proxies: [{ ID: 'proxy-1', Host: 'repo.internal', Port: 3128, Status: 'healthy' }] });
  api.upsertNodePatchConfig.mockImplementation(async (payload) => ({ NodeID: payload.node_id, Mode: payload.mode, ProxyID: payload.proxy_id, WindowID: payload.window_id, UpdatedAt: '' }));
});
describe('Per-node patch settings', () => {
  it('requires an explicit save and a selected proxy, preserving the maintenance window', async () => {
    const user = userEvent.setup(); render(<NodeConfigEditor node={node} onClose={vi.fn()} />);
    const proxyMode = await screen.findByRole('button', { name: /^proxy / });
    await waitFor(() => expect(proxyMode).toBeEnabled());
    await user.click(proxyMode);
    expect(api.upsertNodePatchConfig).not.toHaveBeenCalled();
    expect(screen.getByRole('button', { name: 'Save patch settings' })).toBeDisabled();
    await user.selectOptions(screen.getByLabelText('Repository proxy'), 'proxy-1');
    await user.click(screen.getByRole('button', { name: 'Save patch settings' }));
    await waitFor(() => expect(api.upsertNodePatchConfig).toHaveBeenCalledWith({ node_id: 'node-1', mode: 'proxy', proxy_id: 'proxy-1', window_id: 'window-1' }));
    expect(api.listSquidProxies).toHaveBeenCalledWith('tenant-1');
  });
  it('retains existing associations when changing mode', async () => {
    api.getNodePatchConfig.mockResolvedValue({ NodeID: node.NodeID, Mode: 'proxy', ProxyID: 'proxy-1', WindowID: 'window-1', UpdatedAt: '' });
    const user = userEvent.setup(); render(<NodeConfigEditor node={node} onClose={vi.fn()} />);
    const directMode = await screen.findByRole('button', { name: /^direct / });
    await waitFor(() => expect(directMode).toBeEnabled()); await user.click(directMode);
    await user.click(screen.getByRole('button', { name: 'Save patch settings' }));
    await waitFor(() => expect(api.upsertNodePatchConfig).toHaveBeenCalledWith({ node_id: 'node-1', mode: 'direct', proxy_id: 'proxy-1', window_id: 'window-1' }));
  });
  it('does not substitute defaults or allow saving when settings cannot load', async () => {
    api.getNodePatchConfig.mockRejectedValue(new Error('settings unavailable'));
    render(<NodeConfigEditor node={node} onClose={vi.fn()} />);
    expect(await screen.findByRole('alert')).toHaveTextContent('settings unavailable');
    expect(screen.getByRole('button', { name: 'Save patch settings' })).toBeDisabled();
    expect(api.upsertNodePatchConfig).not.toHaveBeenCalled();
  });
  it('keeps requested package names distinct from zero actual upgrades in per-node details', async () => {
    render(<DeploymentNodeDetail deployment={{ ID: 'deployment-1', TenantID: 'tenant-1', Mode: 'direct', Status: 'completed', TargetNodeCount: 1, RequestedAt: '', Summary: { package_allowlist: ['zlib1g'], package_denylist: ['other'] } }} onClose={vi.fn()} />);
    expect(screen.getByRole('region', { name: 'Requested package scope' })).toHaveTextContent('zlib1g');
    expect(screen.getByText('Excluded: other')).toBeInTheDocument();
    expect(await screen.findByRole('cell', { name: /^0$/ })).toBeInTheDocument();
    expect(screen.getByText(/PKGS is the actual number upgraded/)).toBeInTheDocument();
  });
  it('shows unavailable historical scope while retaining per-node outcomes', async () => {
    render(<DeploymentNodeDetail deployment={{ ID: 'deployment-1', TenantID: 'tenant-1', Mode: 'direct', Status: 'completed', TargetNodeCount: 1, RequestedAt: '' }} onClose={vi.fn()} />);
    expect(screen.getByText('Package scope unavailable')).toBeInTheDocument();
    expect(await screen.findByRole('cell', { name: /^0$/ })).toBeInTheDocument();
  });});
