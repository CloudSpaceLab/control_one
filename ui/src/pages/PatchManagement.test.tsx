import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { MaintenanceWindow, PatchApproval, PatchDeployment, PatchPosture, SquidProxy } from '../lib/api';
import { PatchManagement } from './PatchManagement';

const mocks = vi.hoisted(() => {
  const getPatchPosture = vi.fn();
  const listPatchDeployments = vi.fn();
  const listSquidProxies = vi.fn();
  const listMaintenanceWindows = vi.fn();
  const listPatchApprovals = vi.fn();
  const removeSquidProxy = vi.fn();
  const forceCloseMaintenanceWindow = vi.fn();
  const approvePatchApproval = vi.fn();
  const denyPatchApproval = vi.fn();
  const toastSuccess = vi.fn();
  const toastError = vi.fn();

  return {
    getPatchPosture,
    listPatchDeployments,
    listSquidProxies,
    listMaintenanceWindows,
    listPatchApprovals,
    removeSquidProxy,
    forceCloseMaintenanceWindow,
    approvePatchApproval,
    denyPatchApproval,
    toastSuccess,
    toastError,
    apiClient: {
      getPatchPosture: (tenantId: string) => getPatchPosture(tenantId),
      listPatchDeployments: (params: unknown) => listPatchDeployments(params),
      listSquidProxies: (tenantId: string) => listSquidProxies(tenantId),
      listMaintenanceWindows: (tenantId: string) => listMaintenanceWindows(tenantId),
      listPatchApprovals: (params: unknown) => listPatchApprovals(params),
      removeSquidProxy: (id: string) => removeSquidProxy(id),
      forceCloseMaintenanceWindow: (id: string) => forceCloseMaintenanceWindow(id),
      approvePatchApproval: (id: string) => approvePatchApproval(id),
      denyPatchApproval: (id: string) => denyPatchApproval(id),
    },
  };
});

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

vi.mock('../providers/TenantProvider', () => ({
  useTenant: () => ({ currentTenantId: 'tenant-1' }),
}));

vi.mock('sonner', () => ({
  toast: {
    success: mocks.toastSuccess,
    error: mocks.toastError,
  },
}));

const samplePosture: PatchPosture = {
  active_nodes: 12,
  inventory_nodes: 11,
  known_affected_nodes: 3,
  known_active_findings: 7,
  known_critical_findings: 2,
  known_high_findings: 3,
  known_kev_findings: 1,
  known_patchable_findings: 5,
  deployments_total: 14,
  deployments_pending: 1,
  deployments_in_progress: 1,
  deployments_completed: 10,
  deployments_partial: 1,
  deployments_failed: 1,
  pending_approvals: 2,
  expired_approvals: 1,
  windows_scheduled: 1,
  windows_open: 0,
  windows_closing: 0,
  proxies_healthy: 1,
  proxies_degraded: 0,
  direct_nodes: 9,
  proxy_nodes: 2,
  airgapped_nodes: 1,
};

const sampleDeployment: PatchDeployment = {
  ID: 'deployment-1',
  TenantID: 'tenant-1',
  Mode: 'proxy',
  Status: 'pending',
  TargetNodeCount: 1,
  RequestedAt: '2026-06-08T00:00:00Z',
  nodes_applied: 0,
  nodes_failed: 0,
};

const sampleProxy: SquidProxy = {
  ID: 'proxy-1',
  TenantID: 'tenant-1',
  Host: 'patch-proxy.local',
  Port: 3128,
  Status: 'healthy',
  Whitelist: ['archive.ubuntu.com'],
  CreatedAt: '2026-06-08T00:00:00Z',
  UpdatedAt: '2026-06-08T00:00:00Z',
};

const sampleWindow: MaintenanceWindow = {
  ID: 'window-1',
  TenantID: 'tenant-1',
  Name: 'Emergency patch window',
  NodeIDs: ['node-1234567890'],
  OpensAt: '2026-06-08T01:00:00Z',
  ClosesAt: '2026-06-08T03:00:00Z',
  AllowRepos: ['archive.ubuntu.com'],
  Status: 'open',
  CreatedAt: '2026-06-08T00:00:00Z',
  UpdatedAt: '2026-06-08T00:00:00Z',
};

const sampleApproval: PatchApproval = {
  id: 'approval-1',
  tenant_id: 'tenant-1',
  deployment_id: 'deployment-1234567890',
  node_id: 'node-1234567890',
  mode: 'proxy',
  status: 'pending',
  created_at: '2026-06-08T00:00:00Z',
  expires_at: '2099-01-01T00:00:00Z',
};

describe('PatchManagement', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getPatchPosture.mockResolvedValue({ data: samplePosture, generated_at: '2026-06-08T00:00:00Z' });
    mocks.listPatchDeployments.mockResolvedValue({ deployments: [], generated_at: '2026-06-08T00:00:00Z' });
    mocks.listSquidProxies.mockResolvedValue({ proxies: [] });
    mocks.listMaintenanceWindows.mockResolvedValue({ windows: [] });
    mocks.listPatchApprovals.mockResolvedValue({ data: [], pagination: { total: 0, limit: 100, offset: 0, count: 0 } });
    mocks.removeSquidProxy.mockResolvedValue({ status: 'queued' });
    mocks.forceCloseMaintenanceWindow.mockResolvedValue({ status: 'closed' });
    mocks.approvePatchApproval.mockResolvedValue({ ...sampleApproval, status: 'approved' });
    mocks.denyPatchApproval.mockResolvedValue({ ...sampleApproval, status: 'denied' });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('shows authoritative posture independently of the bounded deployment list', async () => {
    const deployments = Array.from({ length: 50 }, (_, index) => ({
      ...sampleDeployment,
      ID: `deployment-${index}`,
      Status: index === 0 ? 'failed' as const : 'completed' as const,
    }));
    mocks.listPatchDeployments.mockResolvedValue({ deployments, generated_at: '2026-06-08T00:00:00Z' });

    render(<PatchManagement />);

    expect(await screen.findByText('11 / 12')).toBeInTheDocument();
    expect(screen.getAllByText('2').length).toBeGreaterThan(0);
    expect(screen.getAllByText('3').length).toBeGreaterThan(0);
    expect(screen.getByText('Recent deployment activity · latest 50')).toBeInTheDocument();
    expect(screen.getByText('50')).toBeInTheDocument();
  });

  it('shows explicit unavailable states for partial patch-management load failures', async () => {
    const user = userEvent.setup();
    mocks.listSquidProxies.mockRejectedValueOnce(new Error('proxy store offline'));
    mocks.listMaintenanceWindows.mockRejectedValueOnce(new Error('window store offline'));
    mocks.listPatchApprovals.mockRejectedValueOnce(new Error('approval store offline'));

    render(<PatchManagement />);

    expect(await screen.findByText('Patch management data partially unavailable')).toBeInTheDocument();
    expect(screen.getByText(/Proxies: proxy store offline/)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /Proxies \(!\)/ }));
    expect(screen.getByText('Managed proxies unavailable')).toBeInTheDocument();
    expect(screen.queryByText('No managed proxies')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /Windows \(!\)/ }));
    expect(screen.getByText('Maintenance windows unavailable')).toBeInTheDocument();
    expect(screen.queryByText('No maintenance windows')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /Approvals \(!\)/ }));
    expect(screen.getByText('Patch approvals unavailable')).toBeInTheDocument();
    expect(screen.queryByText('No approvals waiting')).not.toBeInTheDocument();
  });

  it('shows deployment KPIs as unavailable when deployment loading fails', async () => {
    mocks.listPatchDeployments.mockRejectedValueOnce(new Error('deployment store offline'));

    render(<PatchManagement />);

    expect(await screen.findByText('Patch management data unavailable')).toBeInTheDocument();
    expect(screen.getByText('Patch deployments unavailable')).toBeInTheDocument();
    expect(screen.queryByText('No deployments yet')).not.toBeInTheDocument();
    expect(screen.getAllByText('N/A')).toHaveLength(4);
  });

  it('requires modal confirmation before removing a proxy and keeps failed removal visible', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    mocks.listSquidProxies.mockResolvedValue({ proxies: [sampleProxy] });
    mocks.removeSquidProxy.mockRejectedValueOnce(new Error('remove denied'));

    render(<PatchManagement />);

    await user.click(await screen.findByRole('button', { name: /Proxies \(1\)/ }));
    await user.click(screen.getByRole('button', { name: 'Remove patch proxy patch-proxy.local:3128' }));

    expect(confirmSpy).not.toHaveBeenCalled();
    expect(mocks.removeSquidProxy).not.toHaveBeenCalled();

    const dialog = screen.getByRole('dialog', { name: /remove patch proxy patch-proxy.local:3128/i });
    await user.click(within(dialog).getByRole('button', { name: 'Remove proxy' }));

    await waitFor(() => expect(mocks.removeSquidProxy).toHaveBeenCalledWith('proxy-1'));
    expect(screen.getByRole('dialog', { name: /remove patch proxy patch-proxy.local:3128/i })).toBeInTheDocument();
    expect(screen.getByText('Proxy removal failed')).toBeInTheDocument();
    expect(screen.getAllByText('Failed to remove proxy patch-proxy.local:3128: remove denied').length).toBeGreaterThanOrEqual(2);
  });

  it('requires modal confirmation before force-closing a maintenance window and keeps failed force-close visible', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    mocks.listMaintenanceWindows.mockResolvedValue({ windows: [sampleWindow] });
    mocks.forceCloseMaintenanceWindow.mockRejectedValueOnce(new Error('teardown audit lock'));

    render(<PatchManagement />);

    await user.click(await screen.findByRole('button', { name: /Windows \(1\)/ }));
    await user.click(screen.getByRole('button', { name: 'Force-close maintenance window Emergency patch window' }));

    expect(confirmSpy).not.toHaveBeenCalled();
    expect(mocks.forceCloseMaintenanceWindow).not.toHaveBeenCalled();

    const dialog = screen.getByRole('dialog', { name: /force-close maintenance window emergency patch window/i });
    await user.click(within(dialog).getByRole('button', { name: 'Force-close window' }));

    await waitFor(() => expect(mocks.forceCloseMaintenanceWindow).toHaveBeenCalledWith('window-1'));
    expect(screen.getByRole('dialog', { name: /force-close maintenance window emergency patch window/i })).toBeInTheDocument();
    expect(screen.getByText('Window force-close failed')).toBeInTheDocument();
    expect(screen.getAllByText('Failed to force-close window Emergency patch window: teardown audit lock').length).toBeGreaterThanOrEqual(2);
  });

  it('requires modal confirmation before denying a patch approval and keeps failed denial visible', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    mocks.listPatchDeployments.mockResolvedValue({ deployments: [sampleDeployment], generated_at: '2026-06-08T00:00:00Z' });
    mocks.listPatchApprovals.mockResolvedValue({ data: [sampleApproval], pagination: { total: 1, limit: 100, offset: 0, count: 1 } });
    mocks.denyPatchApproval.mockRejectedValueOnce(new Error('approval store locked'));

    render(<PatchManagement />);

    await user.click(await screen.findByRole('button', { name: /Approvals \(1\)/ }));
    await user.click(screen.getByRole('button', { name: 'Deny patch deployment for node node-1234567890' }));

    expect(confirmSpy).not.toHaveBeenCalled();
    expect(mocks.denyPatchApproval).not.toHaveBeenCalled();

    const dialog = screen.getByRole('dialog', { name: /deny patch approval for node node-123/i });
    await user.click(within(dialog).getByRole('button', { name: 'Deny approval' }));

    await waitFor(() => expect(mocks.denyPatchApproval).toHaveBeenCalledWith('approval-1'));
    expect(screen.getByRole('dialog', { name: /deny patch approval for node node-123/i })).toBeInTheDocument();
    expect(screen.getByText('Patch approval denial failed')).toBeInTheDocument();
    expect(screen.getAllByText('Failed to deny patch approval for node node-123: approval store locked').length).toBeGreaterThanOrEqual(2);
  });
});
