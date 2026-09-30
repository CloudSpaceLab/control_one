import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { TeamActivity } from './TeamActivity';
import * as useApiClientModule from '@/hooks/useApiClient';
import * as useTenantModule from '@/providers/TenantProvider';
import type {
  TeamActivityItem,
  TeamCoverageGaps,
  TeamMetricsResponse,
  TeamTrendPoint,
  SOCCase,
} from '@/lib/api';

class ResizeObserverMock {
  observe() {}
  unobserve() {}
  disconnect() {}
}

vi.stubGlobal('ResizeObserver', ResizeObserverMock);
HTMLElement.prototype.scrollIntoView = vi.fn();

const openCase: SOCCase = {
  case_id: 'case-open-1',
  tenant_id: 'tenant-1',
  title: 'Suspicious login',
  status: 'open',
  severity: 'high',
  source: 'ai_investigation',
  trigger_type: 'alert',
  trigger_event_type: 'auth.failure',
  dedup_key: 'case-open-1',
  summary: 'Repeated failed authentication attempts.',
  evidence: {},
  evidence_refs: [],
  timeline: [],
  notes: [],
  citations: [],
  coverage_badges: [],
  export_url: '',
  created_at: '2026-06-02T09:00:00Z',
  updated_at: '2026-06-02T09:00:00Z',
};

const metrics: TeamMetricsResponse = {
  summary: {
    alerts_reviewed: 12,
    alerts_resolved: 4,
    cases_created: 3,
    containment_actions: 2,
    notes_added: 7,
    active_analysts: 2,
    avg_time_to_investigate_seconds: 1_800,
    avg_time_to_resolve_seconds: 7_200,
  },
  analysts: [
    {
      analyst_id: 'analyst-1',
      analyst_name: 'Alex Rivera',
      alerts_reviewed: 8,
      alerts_resolved: 3,
      cases_created: 2,
      containment_actions: 2,
      notes_added: 4,
      avg_time_to_investigate_seconds: 1_500,
    },
    {
      analyst_id: 'analyst-2',
      analyst_name: 'Morgan Lee',
      alerts_reviewed: 4,
      alerts_resolved: 1,
      cases_created: 1,
      containment_actions: 0,
      notes_added: 3,
      avg_time_to_investigate_seconds: 2_400,
    },
  ],
};

const trends: TeamTrendPoint[] = [
  {
    bucket: '2026-06-01T00:00:00Z',
    alerts_reviewed: 3,
    alerts_resolved: 1,
    cases_created: 1,
    containment_actions: 1,
    cases_closed: 0,
  },
  {
    bucket: '2026-06-02T00:00:00Z',
    alerts_reviewed: 9,
    alerts_resolved: 3,
    cases_created: 2,
    containment_actions: 1,
    cases_closed: 0,
  },
];

const feed: TeamActivityItem[] = [
  {
    timestamp: '2026-06-02T09:15:00Z',
    kind: 'case_created',
    actor_id: 'analyst-1',
    actor_name: 'Alex Rivera',
    detail: 'Promoted from alert: Suspicious SSH login',
    severity: 'high',
    link_id: 'case-1',
  },
  {
    timestamp: '2026-06-02T08:40:00Z',
    kind: 'alert_reviewed',
    actor_id: 'analyst-2',
    actor_name: 'Morgan Lee',
    detail: 'Port scan across 14 hosts',
    severity: 'medium',
    link_id: 'alert-9',
  },
];

const gaps: TeamCoverageGaps = {
  unreviewed_open_alerts: 2,
  open_alerts_older_than_24h: 1,
  stale_cases: 0,
  active_analysts_last_7_days: 2,
  inactive_analysts_90d: 0,
};

// eslint-disable-next-line @typescript-eslint/no-explicit-any
let mockApi: any;

describe('TeamActivity', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    mockApi = {
      getTeamMetrics: vi.fn().mockResolvedValue(metrics),
      getTeamTrends: vi.fn().mockResolvedValue({ granularity: 'day', points: trends }),
      getTeamActivity: vi.fn().mockResolvedValue({
        data: feed,
        pagination: { total: feed.length, count: feed.length, limit: 50, offset: 0, nextOffset: null, prevOffset: null },
      }),
      getTeamCoverageGaps: vi.fn().mockResolvedValue(gaps),
      listSOCCases: vi.fn().mockResolvedValue({
        data: [openCase],
        pagination: { total: 1, count: 1, limit: 25, offset: 0, nextOffset: null, prevOffset: null },
      }),
      getTeamUsers: vi.fn().mockResolvedValue([{ id: 'u1', name: 'Ada CISO' }]),
      assignSOCCase: vi.fn().mockResolvedValue({
        ...openCase,
        assignee: { id: 'u1', name: 'Ada CISO' },
      }),
    };
    vi.spyOn(useTenantModule, 'useTenant').mockReturnValue({
      currentTenantId: 'tenant-1',
      currentTenant: { id: 'tenant-1', name: 'Bank Tenant', created_at: '2026-05-21T00:00:00Z' },
      tenants: [],
      loading: false,
      error: null,
      setCurrentTenantId: vi.fn(),
      refresh: vi.fn(),
    });
    vi.spyOn(useApiClientModule, 'useApiClient').mockReturnValue(mockApi);
  });

  it('renders summary KPIs and analyst workload from the metrics endpoint', async () => {
    render(
      <MemoryRouter>
        <TeamActivity />
      </MemoryRouter>,
    );

    expect(await screen.findByText('12')).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getAllByText('Alerts reviewed').length).toBeGreaterThan(0);
    });
    expect(screen.getAllByText('Alex Rivera').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Morgan Lee').length).toBeGreaterThan(0);
    expect(mockApi.getTeamMetrics).toHaveBeenCalledWith('tenant-1', { days: 30 });
  });

  it('lists recent activity in the feed with actor and detail', async () => {
    render(
      <MemoryRouter>
        <TeamActivity />
      </MemoryRouter>,
    );

    expect(await screen.findByText('Promoted from alert: Suspicious SSH login')).toBeInTheDocument();
    expect(screen.getByText('Port scan across 14 hosts')).toBeInTheDocument();
    expect(screen.getAllByText('Open →').length).toBeGreaterThan(0);
  });

  it('surfaces coverage gaps when the team is behind', async () => {
    render(
      <MemoryRouter>
        <TeamActivity />
      </MemoryRouter>,
    );

    expect(await screen.findByText('Coverage gaps')).toBeInTheDocument();
    expect(screen.getByText(/2 open alerts never acknowledged/i)).toBeInTheDocument();
  });

  it('does not render the gaps banner when the team is healthy', async () => {
    mockApi.getTeamCoverageGaps.mockResolvedValueOnce({
      unreviewed_open_alerts: 0,
      open_alerts_older_than_24h: 0,
      stale_cases: 0,
      active_analysts_last_7_days: 2,
      inactive_analysts_90d: 0,
    });
    render(
      <MemoryRouter>
        <TeamActivity />
      </MemoryRouter>,
    );

    await waitFor(() => expect(mockApi.getTeamCoverageGaps).toHaveBeenCalled());
    expect(screen.queryByText('Coverage gaps')).not.toBeInTheDocument();
  });

  it('shows an error banner with retry when the API fails', async () => {
    mockApi.getTeamMetrics.mockRejectedValueOnce(new Error('boom'));
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <TeamActivity />
      </MemoryRouter>,
    );

    expect(await screen.findByRole('alert')).toBeInTheDocument();
    expect(screen.getByText(/boom/i)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /retry/i }));
    await waitFor(() => {
      expect(mockApi.getTeamMetrics).toHaveBeenCalledTimes(2);
    });
  });

  it('lists open cases and assigns an owner inline', async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <TeamActivity />
      </MemoryRouter>,
    );

    expect(await screen.findByText('Suspicious login')).toBeInTheDocument();
    await user.click(screen.getByRole('combobox', { name: 'Assignee' }));
    await user.click(await screen.findByText('Ada CISO'));

    await waitFor(() => {
      expect(mockApi.assignSOCCase).toHaveBeenCalledWith('case-open-1', 'tenant-1', {
        assignee_id: 'u1',
      });
    });
  });
});
