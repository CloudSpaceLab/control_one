import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { Cluster, ClusterHealth, ClusterRolloutDetail } from '../lib/api';
import { ClusterDetail } from './ClusterDetail';

const getCluster = vi.fn();
const getClusterHealth = vi.fn();
const getClusterRollout = vi.fn();
const updateCluster = vi.fn();
const showToast = vi.fn();

// Stable refs so hooks that depend on the returned object (via useCallback
// deps) don't see a fresh identity each render.
const stableApi = {
  getCluster,
  getClusterHealth,
  getClusterRollout,
  updateCluster,
};
const stableToast = { showToast };

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => stableApi,
}));

vi.mock('../providers/ToastProvider', () => ({
  useToast: () => stableToast,
}));

function buildCluster(overrides: Partial<Cluster> = {}): Cluster {
  return {
    id: 'c-1',
    tenant_id: 't-1',
    name: 'prod-eu',
    provider: 'mock',
    desired_size: 5,
    role_plan: {},
    labels: {},
    failure_domain_strategy: 'spread',
    state: 'running',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

function buildHealth(overrides: Partial<ClusterHealth> = {}): ClusterHealth {
  return {
    cluster_id: 'c-1',
    state: 'healthy',
    healthy_count: 5,
    total_count: 5,
    desired_size: 5,
    quorum: 3,
    quorum_met: true,
    computed_at: '2026-01-01T00:00:00Z',
    members: [
      {
        node_id: 'n-1',
        hostname: 'host-a',
        role: 'control-plane',
        position: 0,
        state: 'active',
        heartbeat_age_seconds: 30,
        healthy: true,
      },
      {
        node_id: 'n-2',
        hostname: 'host-b',
        role: 'worker',
        position: 0,
        state: 'active',
        heartbeat_age_seconds: 45,
        healthy: true,
      },
    ],
    ...overrides,
  };
}

function renderDetail() {
  return render(
    <MemoryRouter initialEntries={['/clusters/c-1']}>
      <Routes>
        <Route path="/clusters/:clusterId" element={<ClusterDetail />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('ClusterDetail', () => {
  beforeEach(() => {
    getCluster.mockReset();
    getClusterHealth.mockReset();
    getClusterRollout.mockReset();
    updateCluster.mockReset();
    showToast.mockReset();
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders the member table and topology grouped by role', async () => {
    getCluster.mockResolvedValue(buildCluster());
    getClusterHealth.mockResolvedValue(buildHealth());

    renderDetail();

    await waitFor(() => expect(screen.getByTestId('cluster-member-table')).toBeInTheDocument());
    const topology = screen.getByTestId('cluster-topology');
    expect(topology).toBeInTheDocument();
    // Role labels appear in the topology grouping.
    expect(topology.textContent).toContain('control-plane');
    expect(topology.textContent).toContain('worker');
    // Hostnames render in the member table.
    const memberTable = screen.getByTestId('cluster-member-table');
    expect(memberTable.textContent).toContain('host-a');
    expect(memberTable.textContent).toContain('host-b');

    // Health badge renders.
    const badge = screen.getByTestId('cluster-health-badge');
    expect(badge.textContent).toBe('Healthy');
  });

  it('dispatches PATCH on scale-up via the slider', async () => {
    getCluster.mockResolvedValue(buildCluster());
    getClusterHealth.mockResolvedValue(buildHealth());
    updateCluster.mockResolvedValue({ cluster_id: 'c-1', job_id: 'j-1', state: 'scaling' });

    renderDetail();

    await waitFor(() => expect(screen.getByTestId('cluster-scale-slider')).toBeInTheDocument());

    const numericInput = screen.getByTestId('cluster-scale-input') as HTMLInputElement;
    fireEvent.change(numericInput, { target: { value: '7' } });
    fireEvent.click(screen.getByTestId('cluster-scale-submit'));

    await waitFor(() => {
      expect(updateCluster).toHaveBeenCalledWith('c-1', { desired_size: 7 });
    });
  });

  it('shows the drain confirm modal on scale-down and only PATCHes after confirmation', async () => {
    getCluster.mockResolvedValue(buildCluster({ desired_size: 5 }));
    getClusterHealth.mockResolvedValue(buildHealth());
    updateCluster.mockResolvedValue({ cluster_id: 'c-1', job_id: 'j-1', state: 'scaling' });

    renderDetail();

    await waitFor(() => expect(screen.getByTestId('cluster-scale-slider')).toBeInTheDocument());

    const numericInput = screen.getByTestId('cluster-scale-input') as HTMLInputElement;
    fireEvent.change(numericInput, { target: { value: '3' } });
    fireEvent.click(screen.getByTestId('cluster-scale-submit'));

    // Modal opens; PATCH not called yet.
    expect(await screen.findByTestId('drain-confirm-modal')).toBeInTheDocument();
    expect(updateCluster).not.toHaveBeenCalled();

    // Confirm triggers the actual PATCH.
    const confirmButton = screen.getByRole('button', { name: /drain and scale down/i });
    fireEvent.click(confirmButton);

    await waitFor(() => {
      expect(updateCluster).toHaveBeenCalledWith('c-1', { desired_size: 3 });
    });
  });

  it('renders the rollout progress bar when the cluster has a rollout', async () => {
    const rollout: ClusterRolloutDetail = {
      id: 'r-1',
      cluster_id: 'c-1',
      template_version_id: 'tv-1',
      wave_size: 2,
      wave_strategy: 'rolling',
      health_gate: {},
      state: 'running',
      current_wave: 1,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
      waves: [
        {
          id: 'w-1',
          wave_number: 0,
          member_ids: ['n-1', 'n-2'],
          state: 'healthy',
          started_at: '2026-01-01T00:00:00Z',
        },
        {
          id: 'w-2',
          wave_number: 1,
          member_ids: ['n-3', 'n-4'],
          state: 'running',
          started_at: '2026-01-01T00:01:00Z',
        },
      ],
    };
    getCluster.mockResolvedValue(
      buildCluster({
        latest_rollout: {
          id: 'r-1',
          template_version_id: 'tv-1',
          wave_size: 2,
          wave_strategy: 'rolling',
          health_gate: {},
          state: 'running',
          current_wave: 1,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      }),
    );
    getClusterHealth.mockResolvedValue(buildHealth());
    getClusterRollout.mockResolvedValue(rollout);

    renderDetail();

    await waitFor(() => expect(screen.getByTestId('cluster-rollout-panel')).toBeInTheDocument());
    expect(screen.getByRole('progressbar')).toBeInTheDocument();
  });
});
