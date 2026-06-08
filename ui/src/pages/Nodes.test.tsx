import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { NodeHealthScore, NodeSummary } from '../lib/api';
import { Nodes } from './Nodes';

const mocks = vi.hoisted(() => {
  const listNodes = vi.fn();
  const fleetHealthSnapshot = vi.fn();
  const listJobs = vi.fn();
  const getNodeHealth = vi.fn();
  const listAtRiskNodes = vi.fn();
  const enrichIp = vi.fn();
  const setNodeIsolation = vi.fn();
  const updateAgent = vi.fn();
  const showToast = vi.fn();

  return {
    listNodes,
    fleetHealthSnapshot,
    listJobs,
    getNodeHealth,
    listAtRiskNodes,
    enrichIp,
    setNodeIsolation,
    updateAgent,
    showToast,
    apiClient: {
      listNodes,
      fleetHealthSnapshot,
      listJobs,
      getNodeHealth,
      listAtRiskNodes,
      enrichIp,
      setNodeIsolation,
      updateAgent,
    },
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
    pagination: { total: 1, count: 1, limit: 10, offset: 0, nextOffset: null, prevOffset: null },
    reload: vi.fn(),
    refresh: vi.fn(),
  }),
}));

vi.mock('../providers/TenantProvider', () => ({
  useTenant: () => ({ currentTenantId: 'tenant-1' }),
}));

vi.mock('../providers/ToastProvider', () => ({
  useToast: () => ({ showToast: mocks.showToast }),
}));

const node: NodeSummary = {
  id: 'node-1',
  tenant_id: 'tenant-1',
  hostname: 'core-api-01',
  os: 'linux',
  arch: 'amd64',
  public_ip: '203.0.113.10',
  state: 'active',
  last_seen_at: new Date().toISOString(),
  agent_version: '1.2.3',
  labels: {},
  created_at: '2026-06-08T00:00:00Z',
  updated_at: '2026-06-08T00:00:00Z',
};

const health: NodeHealthScore = {
  node_id: 'node-1',
  score: 91,
  risk_level: 'low',
  components: {},
  computed_at: '2026-06-08T00:00:00Z',
};

function paginated<T>(data: T[]) {
  return {
    data,
    pagination: { total: data.length, count: data.length, limit: 500, offset: 0, nextOffset: null, prevOffset: null },
  };
}

function renderNodes() {
  return render(
    <MemoryRouter>
      <Nodes />
    </MemoryRouter>,
  );
}

describe('Nodes page production hardening', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listNodes.mockResolvedValue(paginated([node]));
    mocks.fleetHealthSnapshot.mockResolvedValue({
      source: 'small-analytics-postgres',
      totals: { nodes: 1, healthy: 1, warning: 0, degraded: 0, critical: 0, unknown: 0 },
      nodes: [],
      computed_at: '2026-06-08T00:00:00Z',
    });
    mocks.listJobs.mockResolvedValue(paginated([]));
    mocks.getNodeHealth.mockResolvedValue(health);
    mocks.listAtRiskNodes.mockResolvedValue({ data: [], total_count: 0, critical: 0, high: 0 });
    mocks.enrichIp.mockResolvedValue({ geo: { latitude: 51.5, longitude: -0.1, city: 'London', country: 'United Kingdom' } });
    mocks.setNodeIsolation.mockResolvedValue({ ...node, labels: { 'control_one.isolation.mode': 'airgapped' } });
    mocks.updateAgent.mockResolvedValue({ job_id: 'job-1', status: 'queued' });
  });

  it('scopes the fleet and at-risk reads to the active tenant', async () => {
    renderNodes();

    await waitFor(() => expect(mocks.listNodes).toHaveBeenCalledWith(expect.objectContaining({ tenantId: 'tenant-1' })));
    await waitFor(() => expect(mocks.listAtRiskNodes).toHaveBeenCalledWith('tenant-1'));
    expect(await screen.findByText('core-api-01')).toBeInTheDocument();
  });

  it('does not show false empty states when nodes fail to load', async () => {
    mocks.listNodes.mockRejectedValueOnce(new Error('fleet store offline'));

    renderNodes();

    expect(await screen.findByRole('alert')).toHaveTextContent('fleet store offline');
    expect(screen.getByText('Fleet data unavailable.')).toBeInTheDocument();
    expect(screen.getByText('Fleet map unavailable')).toBeInTheDocument();
    expect(screen.getByText('Fleet nodes could not be loaded')).toBeInTheDocument();
    expect(screen.queryByText(/^No nodes$/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/No nodes online/i)).not.toBeInTheDocument();
  });

  it('surfaces at-risk lookup failures instead of swallowing them', async () => {
    mocks.listAtRiskNodes.mockRejectedValueOnce(new Error('risk model unavailable'));

    renderNodes();

    expect(await screen.findByText('At-risk fleet unavailable')).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('risk model unavailable');
    expect(mocks.listAtRiskNodes).toHaveBeenCalledWith('tenant-1');
  });

  it('uses an in-app confirmation and keeps failed isolation changes visible', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    mocks.setNodeIsolation.mockRejectedValueOnce(new Error('isolation gate denied'));

    renderNodes();

    await user.click(await screen.findByRole('button', { name: /show node table/i }));
    await user.click(await screen.findByRole('button', { name: /airgap core-api-01 for 1 hour/i }));
    const dialog = screen.getByRole('dialog', { name: /change node isolation/i });
    expect(dialog).toHaveTextContent('Airgap core-api-01 for 1 hour.');
    expect(confirmSpy).not.toHaveBeenCalled();

    await user.click(within(dialog).getByRole('button', { name: /airgap node/i }));

    expect(await within(dialog).findByRole('alert')).toHaveTextContent(
      'Airgap core-api-01 for 1 hour failed: isolation gate denied',
    );
    expect(screen.getAllByText('Airgap core-api-01 for 1 hour failed: isolation gate denied').length).toBeGreaterThanOrEqual(2);
    expect(mocks.setNodeIsolation).toHaveBeenCalledWith('node-1', {
      mode: 'airgapped',
      duration_seconds: 3600,
      reason: 'Operator quick airgap from fleet list',
    });
    confirmSpy.mockRestore();
  });

  it('keeps failed agent update queue requests in the confirmation modal', async () => {
    const user = userEvent.setup();
    mocks.updateAgent.mockRejectedValueOnce(new Error('update queue denied'));

    renderNodes();

    await user.click(await screen.findByRole('button', { name: /queue agent update for core-api-01/i }));
    const dialog = screen.getByRole('dialog', { name: /queue agent self-update/i });
    expect(dialog).toHaveTextContent('core-api-01 will download the latest binary');

    await user.click(within(dialog).getByRole('button', { name: /update agent/i }));

    expect(await within(dialog).findByRole('alert')).toHaveTextContent(
      'Agent update failed for core-api-01: update queue denied',
    );
    expect(screen.getByRole('dialog', { name: /queue agent self-update/i })).toBeInTheDocument();
    expect(mocks.updateAgent).toHaveBeenCalledWith('node-1');
  });
});
