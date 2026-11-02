import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { MaintenanceWindow, PatchApproval, PatchDeployment, SquidProxy } from '../lib/api';
import { PatchManagement } from './PatchManagement';
import {onlineFixture,onlineNode} from '../lib/onlinePatchPlan.testFixtures';

const mocks = vi.hoisted(() => {
  const getPatchSummary = vi.fn();
  const listPatchDeployments = vi.fn();
  const listSquidProxies = vi.fn();
  const listMaintenanceWindows = vi.fn();
  const listPatchApprovals = vi.fn();
  const removeSquidProxy = vi.fn();
  const forceCloseMaintenanceWindow = vi.fn();
  const approvePatchApproval = vi.fn();
  const denyPatchApproval = vi.fn();
  const listNodes = vi.fn();
  const createPatchDeployment = vi.fn();
 const prepareOnlinePlan=vi.fn();const getOnlinePreparation=vi.fn();
  const listPatchDeploymentNodes = vi.fn();
  const toastSuccess = vi.fn();
  const toastError = vi.fn();
  const toastWarning = vi.fn();

  return {
    getPatchSummary,
    listPatchDeployments,
    listSquidProxies,
    listMaintenanceWindows,
    listPatchApprovals,
    removeSquidProxy,
    forceCloseMaintenanceWindow,
    approvePatchApproval,
    denyPatchApproval,
    listNodes,
    createPatchDeployment,
    prepareOnlinePlan,getOnlinePreparation,
    listPatchDeploymentNodes,
    toastSuccess,
    toastError,
    toastWarning,
    currentTenantId: 'tenant-1' as string | null,
    tenantList: [{ id: 'tenant-1', name: 'Bank A', created_at: '2026-01-01T00:00:00Z' }],
    setCurrentTenantId: vi.fn(),
    apiClient: {
 prepareOnlinePlan:(...args:unknown[])=>prepareOnlinePlan(...args),getOnlinePreparation:(...args:unknown[])=>getOnlinePreparation(...args),
      listNodes: (params: unknown) => listNodes(params),
      createPatchDeployment: (payload: unknown) => createPatchDeployment(payload),
      listPatchDeploymentNodes: (id: string) => listPatchDeploymentNodes(id),
      getPatchSummary: (tenantId?: string) => getPatchSummary(tenantId),
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
  useTenant: () => ({
    currentTenantId: mocks.currentTenantId,
    currentTenant: mocks.currentTenantId
      ? mocks.tenantList.find((tenant) => tenant.id === mocks.currentTenantId) ?? null
      : null,
    tenants: mocks.tenantList,
    loading: false,
    error: null,
    setCurrentTenantId: mocks.setCurrentTenantId,
    refresh: vi.fn(),
  }),
}));

vi.mock('sonner', () => ({
  toast: {
    success: mocks.toastSuccess,
    error: mocks.toastError,
    warning: mocks.toastWarning,
  },
}));

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
  it('requires and submits the selected exact online plan with its one target and mandatory verification',async()=>{
    const user=userEvent.setup();const now=Date.now();const node=onlineNode(now);const plan=onlineFixture(now);
    mocks.listNodes.mockResolvedValue({data:[node]});mocks.createPatchDeployment.mockResolvedValue({awaiting_approval:[{node_id:node.id}]});
    mocks.getOnlinePreparation.mockResolvedValue({status:'succeeded',job_id:'prepared-online-job',message:'Prepared without installation'});
    render(<PatchManagement/>);await user.click(await screen.findByRole('button',{name:/Deploy patches/}));
    await user.click(await screen.findByRole('checkbox',{name:'Select node online-lab'}));
    expect(screen.getByRole('button',{name:'Deploy to 1 node'})).toBeDisabled();
    await user.selectOptions(screen.getByRole('combobox',{name:'Mode'}),'direct');
    await user.selectOptions(await screen.findByRole('combobox',{name:'Online preparation target'}),node.id);
    await waitFor(()=>expect(mocks.listNodes.mock.calls.length).toBeGreaterThan(1));
    expect(screen.getByRole('combobox',{name:'Online preparation target'})).toHaveValue(node.id);
    await user.click(await screen.findByRole('button',{name:'Use prepared direct plan for online-lab'}));
    expect(screen.getByRole('region',{name:'Exact online plan review'})).toHaveTextContent('Required dependency');
    expect(screen.getByRole('checkbox',{name:'Refresh package inventory after patching'})).toBeDisabled();
    await user.click(screen.getByRole('button',{name:'Deploy to 1 node'}));
    await waitFor(()=>expect(mocks.createPatchDeployment).toHaveBeenCalledWith(expect.objectContaining({node_ids:[node.id],mode:'direct',online_plan:plan,package_allowlist:['liba'],post_patch_rescan:true})));
  });
  it('distinguishes proxy registration from installation and connectivity qualification',async()=>{
    const user=userEvent.setup();
    render(<PatchManagement/>);
    await user.click(await screen.findByRole('button',{name:/Proxies \(/}));
    expect(screen.getByText('Managed proxy requirements')).toBeInTheDocument();
    await user.click(screen.getByRole('button',{name:'Register proxy'}));
    expect(screen.getByRole('heading',{name:'Register patch proxy'})).toBeInTheDocument();
    expect(screen.getByText(/does not install or reconfigure Squid/)).toBeInTheDocument();
    expect(screen.queryByRole('button',{name:'Install proxy'})).not.toBeInTheDocument();
  });
  it('disables an expired exact plan even when the approval row has a later legacy deadline',async()=>{
    const user=userEvent.setup();
    mocks.listPatchApprovals.mockResolvedValue({data:[{...sampleApproval,mode:'airgapped',offline_content:{format:3,manifest_sha256:'a'.repeat(64),installed_status_sha256:'b'.repeat(64),keyring_sha256:'c'.repeat(64),suite:'noble-updates',component:'main',architecture:'amd64',requested:[{name:'liba',version:'2'}],resolved:[{name:'liba',version:'2'}],review:{prepared_at:'2020-01-01T10:00:00Z',expires_at:'2020-01-01T11:00:00Z',reboot:'unknown',service_impact:'requires_operator_review',changes:[{name:'liba',installed_version:'1',proposed_version:'2',role:'requested'}]}}}],pagination:{total:1,limit:100,offset:0,count:1}});
    render(<PatchManagement/>);
    await user.click(await screen.findByRole('button',{name:/Approvals \(/}));
    expect(await screen.findByRole('button',{name:/Approve patch deployment for node/})).toBeDisabled();
    expect(mocks.approvePatchApproval).not.toHaveBeenCalled();
  });
  it('retains approved offline versions and refreshes a pending result without certifying remediation', async () => {
    const user = userEvent.setup();
    const plan = { format: 2, manifest_sha256: 'a'.repeat(64), installed_status_sha256: 'b'.repeat(64), keyring_sha256: 'c'.repeat(64), suite: 'noble-updates', component: 'main', architecture: 'amd64', requested: [{ name: 'libudev1', version: '255.4-1ubuntu8.18' }], resolved: [{ name: 'libsystemd0', version: '255.4-1ubuntu8.18' }, { name: 'libudev1', version: '255.4-1ubuntu8.18' }] };
    mocks.listPatchDeployments.mockResolvedValue({ deployments: [{ ...sampleDeployment, Mode: 'airgapped', Status: 'completed', Summary: { package_allowlist: ['libudev1'], offline_content: plan } }] });
    mocks.listPatchDeploymentNodes.mockResolvedValueOnce({ rows: [{ ID: 'state', NodeID: 'lab-node', Status: 'pending' }] }).mockResolvedValueOnce({ rows: [{ ID: 'state', NodeID: 'lab-node', Status: 'applied', PackagesUpgraded: 2, Verification: { current_state: 'verification_unknown', promotion_eligible: false } }] });
    render(<PatchManagement />);
    await user.click(await screen.findByRole('button', { name: 'Per-node' }));
    expect(await screen.findByText('pending', { exact: true })).toBeInTheDocument();
    await user.click(screen.getByText('Approved offline content plan', { exact: true }));
    expect(screen.getByText('libsystemd0 = 255.4-1ubuntu8.18')).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Refresh result' }));
    expect(await screen.findByText('applied', { exact: true })).toBeInTheDocument();
    expect(screen.getByText('Verification: verification unknown')).toBeInTheDocument();
    expect(screen.getByText('Promotion: blocked')).toBeInTheDocument();
    expect(mocks.listPatchDeploymentNodes).toHaveBeenCalledTimes(2);
  });
  it('selects an agent-published plan without uploading a file and binds its one target', async () => {
    const user = userEvent.setup();
    const plan = { format: 2, manifest_sha256: 'a'.repeat(64), installed_status_sha256: 'b'.repeat(64), keyring_sha256: 'c'.repeat(64), suite: 'noble-updates', component: 'main', architecture: 'amd64', requested: [{ name: 'libudev1', version: '1.2.3' }], resolved: [{ name: 'libudev1', version: '1.2.3' }] };
    mocks.listNodes.mockResolvedValue({ data: [{ id: 'lab-node', hostname: 'ubuntu-lab', state: 'active', labels: { 'patch.offline_plans': { observed_at: new Date().toISOString(), report: { version: 1, status: 'ready', plans: [plan] } } } }] });
    mocks.createPatchDeployment.mockResolvedValue({ awaiting_approval: [{ node_id: 'lab-node' }] });
    render(<PatchManagement />);
    await user.click(await screen.findByRole('button', { name: 'Deploy patches…' }));
    await user.selectOptions(screen.getByRole('combobox', { name: 'Mode' }), 'airgapped');
    expect(screen.getByRole('combobox', { name: 'Plan source' })).toHaveValue('agent');
    expect(screen.queryByLabelText('Offline content-plan file')).not.toBeInTheDocument();
    await screen.findByRole('option', { name: /ubuntu-lab.*libudev1/ });
    await user.selectOptions(screen.getByRole('combobox', { name: 'Available offline plan' }), 'lab-node');
    expect(screen.getByRole('checkbox', { name: 'Select node ubuntu-lab' })).toBeChecked();
    await user.click(screen.getByRole('button', { name: 'Deploy to 1 node' }));
    await waitFor(() => expect(mocks.createPatchDeployment).toHaveBeenCalledWith(expect.objectContaining({ node_ids: ['lab-node'], published_offline_plan_sha256: plan.manifest_sha256, offline_content: plan, post_patch_rescan: true })));
  });

  it('imports and submits the exact offline plan for one node with mandatory inventory refresh', async () => {
    const user = userEvent.setup();
    const plan = { format: 2, manifest_sha256: 'a'.repeat(64), installed_status_sha256: 'b'.repeat(64), keyring_sha256: 'c'.repeat(64), suite: 'noble-updates', component: 'main', architecture: 'amd64', requested: [{ name: 'liblzma5', version: '1.2.3' }], resolved: [{ name: 'liblzma5', version: '1.2.3' }] };
    mocks.listNodes.mockResolvedValue({ data: [{ id: 'lab-node', hostname: 'ubuntu-lab', state: 'active', os: 'ubuntu' }] });
    mocks.createPatchDeployment.mockResolvedValue({ node_count: 0, awaiting_approval: [{ node_id: 'lab-node' }] });
    render(<PatchManagement />);
    await user.click(await screen.findByRole('button', { name: 'Deploy patches…' }));
    await user.selectOptions(screen.getByRole('combobox', { name: 'Mode' }), 'airgapped');
    await user.click(await screen.findByRole('checkbox', { name: 'Select node ubuntu-lab' }));
    await user.click(screen.getByRole('button', { name: 'Deploy to 1 node' }));
    expect(mocks.createPatchDeployment).not.toHaveBeenCalled();
    expect(await screen.findByText('Load an offline content plan and select exactly one target node.')).toBeVisible();
    await user.selectOptions(screen.getByRole('combobox', { name: 'Plan source' }), 'manual');
    const file = new File([JSON.stringify(plan)], 'offline-plan.json', { type: 'application/json' });
    Object.defineProperty(file, 'text', { value: async () => JSON.stringify(plan) });
    await user.upload(screen.getByLabelText('Offline content-plan file'), file);
    expect(await screen.findByText('Exact offline installation plan')).toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: /Package allowlist/ })).toBeDisabled();
    expect(screen.getByRole('checkbox', { name: 'Refresh package inventory after patching' })).toBeDisabled();
    await user.click(screen.getByRole('button', { name: 'Deploy to 1 node' }));
    await waitFor(() => expect(mocks.createPatchDeployment).toHaveBeenCalledWith(expect.objectContaining({ mode: 'airgapped', node_ids: ['lab-node'], offline_content: plan, package_allowlist: ['liblzma5'], post_patch_rescan: true })));
  });
  it('shows the planned proxy mode for historical auto deployments instead of the legacy direct header', async () => {
    mocks.listPatchDeployments.mockResolvedValue({ deployments: [{ ...sampleDeployment, Mode: 'direct', Summary: { requested_mode: 'auto', waves: [{ nodes: [{ mode: 'proxy' }] }] } }] });
    render(<PatchManagement />);
    expect(await screen.findByText('auto (proxy)')).toBeInTheDocument();
    expect(screen.queryByText('direct')).not.toBeInTheDocument();
  });
  it('does not label mixed auto deployments as a single transport', async () => {
    mocks.listPatchDeployments.mockResolvedValue({ deployments: [{ ...sampleDeployment, Summary: { requested_mode: 'auto', waves: [{ nodes: [{ mode: 'proxy' }, { mode: 'direct' }] }] } }] });
    render(<PatchManagement />);
    expect(await screen.findByText('auto (per node)')).toBeInTheDocument();
  });
  it('sends only the selected node and explicit package scope with a post-patch inventory refresh', async () => {
    const user = userEvent.setup();
    mocks.listNodes.mockResolvedValue({ data: [{ id: 'lab-node', hostname: 'ubuntu-lab', state: 'active', os: 'ubuntu' }] });
    mocks.createPatchDeployment.mockResolvedValue({ node_count: 1 });
    render(<PatchManagement />);
    await user.click(await screen.findByRole('button', { name: 'Deploy patches…' }));
    await user.click(await screen.findByRole('checkbox', { name: 'Select node ubuntu-lab' }));
    await user.type(screen.getByRole('textbox', { name: /Package allowlist/ }), 'perl-base, perl-base');
    await user.click(screen.getByRole('button', { name: 'Deploy to 1 node' }));
    await waitFor(() => expect(mocks.createPatchDeployment).toHaveBeenCalledWith(expect.objectContaining({
      tenant_id: 'tenant-1', node_ids: ['lab-node'], package_allowlist: ['perl-base'], post_patch_rescan: true,
    })));
  });
  it('shows blocked runtime reasons as a warning without claiming dispatch success', async () => {
    const user = userEvent.setup();
    mocks.listNodes.mockResolvedValue({ data: [{ id: 'lab-node', hostname: 'ubuntu-lab', state: 'active', os: 'ubuntu' }] });
    mocks.createPatchDeployment.mockResolvedValue({ node_count: 0, gate_blocked: [{ node_id: 'lab-node', reason: 'offline repository isolation is not qualified' }] });
    render(<PatchManagement />);
    await user.click(await screen.findByRole('button', { name: 'Deploy patches…' }));
    await user.click(await screen.findByRole('checkbox', { name: 'Select node ubuntu-lab' }));
    await user.click(screen.getByRole('button', { name: 'Deploy to 1 node' }));
    await waitFor(() => expect(mocks.toastWarning).toHaveBeenCalledWith(expect.stringContaining('offline repository isolation is not qualified')));
    expect(mocks.toastSuccess).not.toHaveBeenCalled();
  });
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.currentTenantId = 'tenant-1';
    mocks.tenantList = [{ id: 'tenant-1', name: 'Bank A', created_at: '2026-01-01T00:00:00Z' }];
    mocks.getPatchSummary.mockResolvedValue({
      total: 0,
      pending: 0,
      in_progress: 0,
      completed: 0,
      partial: 0,
      failed: 0,
      pending_approvals: 0,
      expired_approvals: 0,
      active_nodes: 12,
      inventory_nodes: 12,
      fresh_inventory_nodes: 12,
      direct_nodes: 10,
      proxy_nodes: 1,
      airgapped_nodes: 1,
      known_affected_nodes: 0,
      known_active_findings: 0,
      known_critical_findings: 0,
      known_high_findings: 0,
      known_kev_findings: 0,
      known_patchable_findings: 0,
      windows_scheduled: 0,
      windows_open: 0,
      windows_closing: 0,
      proxies_healthy: 1,
      proxies_degraded: 0,
      generated_at: '2026-06-08T00:00:00Z',
    });
    mocks.listPatchDeployments.mockResolvedValue({ deployments: [], generated_at: '2026-06-08T00:00:00Z' });
    mocks.listSquidProxies.mockResolvedValue({ proxies: [] });
    mocks.listMaintenanceWindows.mockResolvedValue({ windows: [] });
    mocks.listPatchApprovals.mockResolvedValue({ data: [], pagination: { total: 0, limit: 100, offset: 0, count: 0 } });
    mocks.removeSquidProxy.mockResolvedValue({ status: 'queued' });
    mocks.forceCloseMaintenanceWindow.mockResolvedValue({ status: 'closed' });
    mocks.approvePatchApproval.mockResolvedValue({ ...sampleApproval, status: 'approved' });
    mocks.denyPatchApproval.mockResolvedValue({ ...sampleApproval, status: 'denied' });
 mocks.getOnlinePreparation.mockResolvedValue({status:'idle'});mocks.prepareOnlinePlan.mockResolvedValue({job_id:'online-job',status:'running'});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it.each(['partial', 'expired', 'unavailable'] as const)('shows %s assessment without treating zero findings as clean', async (status) => {
    const summary = await mocks.getPatchSummary();
    mocks.getPatchSummary.mockResolvedValue({ ...summary, assessment: { status, feed_count: status === 'partial' ? 1 : 0, coverage: 'unknown' } });
    render(<PatchManagement />);
    const title = status === 'partial' ? 'Assessment coverage is partial' : status === 'expired' ? 'Vulnerability feed expired' : 'Assessment status unavailable';
    expect(await screen.findByText(title)).toBeInTheDocument();
    expect(screen.getByText(/Zero known findings does not establish/)).toBeInTheDocument();
  });

  it('shows exact all-tenant patch totals and requires tenant selection before changes', async () => {
    const user = userEvent.setup();
    mocks.currentTenantId = null;
    mocks.tenantList = [
      { id: 'tenant-1', name: 'Bank A', created_at: '2026-01-01T00:00:00Z' },
      { id: 'tenant-2', name: 'Bank B', created_at: '2026-01-02T00:00:00Z' },
    ];
    mocks.getPatchSummary.mockResolvedValue({
      total: 14,
      pending: 2,
      in_progress: 3,
      completed: 7,
      partial: 1,
      failed: 1,
      pending_approvals: 4,
      expired_approvals: 1,
      active_nodes: 20,
      inventory_nodes: 20,
      fresh_inventory_nodes: 19,
      direct_nodes: 15,
      proxy_nodes: 3,
      airgapped_nodes: 2,
      known_affected_nodes: 6,
      known_active_findings: 11,
      known_critical_findings: 2,
      known_high_findings: 4,
      known_kev_findings: 1,
      known_patchable_findings: 8,
      windows_scheduled: 2,
      windows_open: 1,
      windows_closing: 0,
      proxies_healthy: 2,
      proxies_degraded: 0,
      generated_at: '2026-06-08T00:00:00Z',
    });

    render(<PatchManagement />);

    expect(await screen.findByText('All tenants · package coverage, known risk and deployment state.')).toBeInTheDocument();
    expect(screen.getByText('19 / 20')).toBeInTheDocument();
    expect(screen.getByText('5')).toBeInTheDocument();
    expect(screen.getByText('6')).toBeInTheDocument();
    expect(screen.getByText('4')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /deploy patches/i })).not.toBeInTheDocument();
    expect(mocks.listPatchDeployments).not.toHaveBeenCalled();

    await user.click(screen.getByRole('button', { name: /bank b/i }));
    expect(mocks.setCurrentTenantId).toHaveBeenCalledWith('tenant-2');
  });

  it('keeps measured posture separate from the latest 50 deployments', async () => {
    mocks.getPatchSummary.mockResolvedValue({
      total: 88,
      pending: 2,
      in_progress: 3,
      completed: 80,
      partial: 1,
      failed: 2,
      pending_approvals: 2,
      expired_approvals: 1,
      active_nodes: 12,
      inventory_nodes: 11,
      fresh_inventory_nodes: 10,
      direct_nodes: 9,
      proxy_nodes: 2,
      airgapped_nodes: 1,
      known_affected_nodes: 3,
      known_active_findings: 7,
      known_critical_findings: 2,
      known_high_findings: 3,
      known_kev_findings: 1,
      known_patchable_findings: 5,
      windows_scheduled: 1,
      windows_open: 0,
      windows_closing: 0,
      proxies_healthy: 1,
      proxies_degraded: 0,
      generated_at: '2026-06-08T00:00:00Z',
    });
    const deployments = Array.from({ length: 50 }, (_, index) => ({
      ...sampleDeployment,
      ID: `deployment-${index}`,
      Status: index === 0 ? 'failed' as const : 'completed' as const,
    }));
    mocks.listPatchDeployments.mockResolvedValue({ deployments, generated_at: '2026-06-08T00:00:00Z' });
    mocks.listPatchApprovals.mockResolvedValue({ data: [sampleApproval], pagination: { total: 2, limit: 100, offset: 0, count: 1 } });

    render(<PatchManagement />);

    expect(await screen.findByText('10 / 12')).toBeInTheDocument();
    expect(screen.getByText('Recent deployment activity · latest 50')).toBeInTheDocument();
    expect(screen.getByText('50')).toBeInTheDocument();
    expect(screen.getAllByText('2').length).toBeGreaterThan(0);
    expect(screen.getAllByText('3').length).toBeGreaterThan(0);
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

    expect(await screen.findByText('Patch deployment data unavailable')).toBeInTheDocument();
    expect(screen.getByText('Patch deployments unavailable')).toBeInTheDocument();
    expect(screen.queryByText('No deployments yet')).not.toBeInTheDocument();
    expect(screen.getAllByText('N/A')).toHaveLength(4);
  });

  it('shows evidence-based proxy states instead of treating saved installing as live activity', async () => {
    const user=userEvent.setup();
    mocks.listSquidProxies.mockResolvedValue({proxies:[{...sampleProxy,Status:'installing',Evidence:{state:'legacy_unverified',message:'Legacy status has no active job.'}},{...sampleProxy,ID:'registered-2',Host:'registered.local',Status:'registered',Evidence:{state:'registered',message:'No installation requested.'}}]});
    render(<PatchManagement />);
    await user.click(await screen.findByRole('button',{name:/Proxies \(2\)/}));
    expect(screen.getByText('Legacy / unverified')).toBeInTheDocument();
    expect(screen.getByText('Registered / unverified')).toBeInTheDocument();
    expect(screen.getByText('Legacy status has no active job.')).toBeInTheDocument();
    expect(screen.queryByText('INSTALLING')).not.toBeInTheDocument();
  });

  it('routes proxy changes through the reviewed management workflow', async () => {
    const user = userEvent.setup();
    mocks.listSquidProxies.mockResolvedValue({ proxies: [sampleProxy] });
    render(<PatchManagement />);
    await user.click(await screen.findByRole('button', { name: /Proxies \(1\)/ }));
    expect(screen.getByText(/Recorded status is not a live connectivity check/)).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Manage patch proxy patch-proxy.local:3128' }));
    expect(screen.getByRole('dialog', {name: 'Manage proxy patch-proxy.local'})).toBeInTheDocument();
    expect(mocks.removeSquidProxy).not.toHaveBeenCalled();
  });

  it('requires modal confirmation before force-closing a maintenance window and keeps failed force-close visible', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    mocks.listMaintenanceWindows.mockResolvedValue({ windows: [sampleWindow] });
    mocks.forceCloseMaintenanceWindow.mockRejectedValueOnce(new Error('teardown audit lock'));

    render(<PatchManagement />);

    await user.click(await screen.findByRole('button', { name: /Windows \(1\)/ }));
    expect(screen.getByText(/Calendar windows control when a new installation may start/)).toBeInTheDocument();
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
  it.each([
    [{ package_scope_available: true, package_allowlist: ['zlib1g'], package_denylist: ['other'] }, 'zlib1g'],
    [{ package_scope_available: true, package_allowlist: [] }, 'All eligible packages'],
    [{}, 'Package scope unavailable'],
  ])('shows approval scope without guessing missing evidence: %s', async (scope, expected) => {
    mocks.listPatchApprovals.mockResolvedValue({ data: [{ ...sampleApproval, ...scope }], pagination: { total: 1, limit: 100, offset: 0, count: 1 } });
    const user = userEvent.setup();
    render(<PatchManagement />);
    await user.click(await screen.findByRole('button', { name: 'Approvals (1)' }));
    expect(await screen.findByText(expected)).toBeInTheDocument();
    if ('package_denylist' in scope) expect(screen.getByText('Excluded: other')).toBeInTheDocument();
  });  it.each([
    [{ package_allowlist: ['zlib1g'], package_denylist: ['other'] }, 'zlib1g'],
    [{ package_allowlist: [], package_denylist: [] }, 'All eligible packages'],
    [{ package_allowlist: null }, 'All eligible packages'],
    [{}, 'Package scope unavailable'],
    [{ package_allowlist: 'zlib1g' }, 'Package scope unavailable'],
    [{ package_allowlist: ['zlib1g'], package_denylist: 'other' }, 'Package scope unavailable'],
  ])('shows deployment request scope conservatively: %s', async (summary, expected) => {
    mocks.listPatchDeployments.mockResolvedValue({ deployments: [{ ...sampleDeployment, Summary: summary }] });
    render(<PatchManagement />);
    expect(await screen.findByText(expected)).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: 'Package scope' })).toBeInTheDocument();
    if ('package_denylist' in summary && Array.isArray(summary.package_denylist) && summary.package_denylist.length) expect(screen.getByText('Excluded: other')).toBeInTheDocument();
  });});
