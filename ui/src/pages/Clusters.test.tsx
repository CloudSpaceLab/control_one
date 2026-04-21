import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { Cluster } from '../lib/api';
import { Clusters, clusterStateClass, clusterStateLabel } from './Clusters';

// ─── Hook mocks ──────────────────────────────────────────────────────
const listClusters = vi.fn();
const showToast = vi.fn();
// Stable references — the useApiClient hook is called on every render and we
// don't want a fresh identity each time (useEffect deps blow up otherwise).
const stableApi = { listClusters };
const stableToast = { showToast };
const stableTenants = {
  data: [{ id: 't-1', name: 'acme', created_at: '2026-01-01T00:00:00Z' }],
  loading: false,
  error: null,
  pagination: { total: 1, count: 1, limit: 10, offset: 0, nextOffset: null, prevOffset: null },
  reload: () => {},
  refresh: () => {},
};

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => stableApi,
}));

vi.mock('../providers/ToastProvider', () => ({
  useToast: () => stableToast,
}));

vi.mock('../hooks/useTenants', () => ({
  useTenants: () => stableTenants,
}));

function buildCluster(overrides: Partial<Cluster>): Cluster {
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
    health: {
      state: 'healthy',
      healthy_count: 5,
      total_count: 5,
      quorum: 3,
      quorum_met: true,
    },
    ...overrides,
  };
}

describe('Clusters page', () => {
  beforeEach(() => {
    listClusters.mockReset();
  });

  it('renders the clusters list with health badges', async () => {
    listClusters.mockResolvedValue({
      data: [
        buildCluster({ id: 'c-1', name: 'prod-eu' }),
        buildCluster({
          id: 'c-2',
          name: 'dev',
          health: {
            state: 'degraded',
            healthy_count: 3,
            total_count: 5,
            quorum: 3,
            quorum_met: true,
          },
        }),
      ],
      pagination: { total: 2, count: 2, limit: 20, offset: 0, nextOffset: null, prevOffset: null },
    });

    render(
      <MemoryRouter>
        <Clusters />
      </MemoryRouter>,
    );

    // Name renders as a link.
    expect(await screen.findByRole('link', { name: 'prod-eu' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'dev' })).toBeInTheDocument();

    // Health badges render for each row — "Healthy" also appears as a stat
    // card label, so assert on the badge elements specifically by class.
    const healthyBadges = document.querySelectorAll('.cluster-badge--healthy');
    const degradedBadges = document.querySelectorAll('.cluster-badge--degraded');
    expect(healthyBadges.length).toBeGreaterThan(0);
    expect(degradedBadges.length).toBeGreaterThan(0);

    // Stat cards reflect the aggregate counts.
    const totalCard = screen.getByText('Total clusters').closest('article');
    expect(totalCard?.querySelector('strong')?.textContent).toBe('2');
  });

  it('shows an empty state when no clusters exist', async () => {
    listClusters.mockResolvedValue({
      data: [],
      pagination: { total: 0, count: 0, limit: 20, offset: 0, nextOffset: null, prevOffset: null },
    });

    render(
      <MemoryRouter>
        <Clusters />
      </MemoryRouter>,
    );

    expect(await screen.findByText(/No clusters yet/i)).toBeInTheDocument();
  });
});

describe('clusterStateClass / clusterStateLabel', () => {
  it('maps each state to a badge class', () => {
    expect(clusterStateClass('healthy')).toContain('cluster-badge--healthy');
    expect(clusterStateClass('degraded')).toContain('cluster-badge--degraded');
    expect(clusterStateClass('unhealthy')).toContain('cluster-badge--unhealthy');
    expect(clusterStateClass('empty')).toContain('cluster-badge--empty');
    expect(clusterStateClass(undefined)).toContain('cluster-badge--unknown');
  });

  it('returns a readable label per state', () => {
    expect(clusterStateLabel('healthy')).toBe('Healthy');
    expect(clusterStateLabel('degraded')).toBe('Degraded');
    expect(clusterStateLabel('unhealthy')).toBe('Unhealthy');
    expect(clusterStateLabel('empty')).toBe('Empty');
    expect(clusterStateLabel(undefined)).toBe('Unknown');
  });
});
