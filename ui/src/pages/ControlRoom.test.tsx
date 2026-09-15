import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { ControlRoom } from './ControlRoom';
import * as useApiClientModule from '../hooks/useApiClient';
import * as useTenantModule from '../providers/TenantProvider';
import type { ControlRoomOverview } from '../lib/api';

const overview: ControlRoomOverview = {
  tenant_id: 'tenant-1',
  generated_at: '2026-05-18T10:00:00Z',
  period: '24h',
  lanes: [
    lane('server-health', 'Server Health', 'warning', '1 health incident'),
    lane('security', 'Security', 'critical', '3 security events'),
    lane('app-db-health', 'App/DB Health', 'warning', '2 app/web, 1 DB'),
    {
      ...lane('exposure', 'Exposure Confidence', 'warning', '72% confidence: 3 exposure gaps, 2 protected nodes, 1 active block'),
      score: 72,
      primary_metric: { label: 'Security confidence', value: '72%', tone: 'warning', hint: 'Reduce exposure gaps', drilldown: '/control-room/exposure' },
      secondary_metric: { label: 'Exposure gaps', value: '3', tone: 'warning', drilldown: '/control-room/exposure' },
      metrics: [
        { label: 'Public listeners', value: '4', tone: 'warning', drilldown: '/connections' },
        { label: 'Protected listeners', value: '1', tone: 'healthy', drilldown: '/control-room/exposure' },
        { label: 'Critical gaps', value: '1', tone: 'critical', drilldown: '/control-room/exposure' },
        { label: 'Public firewall gaps', value: '1', tone: 'warning', drilldown: '/security/network?tab=firewall' },
        { label: 'Web block ready', value: '1/1', tone: 'healthy', drilldown: '/security/webservers' },
      ],
    },
    {
      ...lane('ip-behavior', 'Connection/IP Behavior', 'critical', '2 requests from 1 country'),
      items: [{ label: '203.0.113.10', value: '91', tone: 'critical', hint: '401 spike', drilldown: '/security/network?tab=ip-behavior&ip=203.0.113.10' }],
    },
    lane('patch-posture', 'Patch Posture', 'warning', '1 pending approval'),
  ],
  top_incidents: [
    {
      id: 'finding-1',
      title: 'credential_stuffing from 203.0.113.10',
      severity: 'critical',
      source: 'ip_behavior',
      summary: '401 spike after new-country traffic',
      drilldown: '/security/network?tab=ip-behavior&ip=203.0.113.10',
      opened_at: '2026-05-18T09:55:00Z',
    },
  ],
  stale_warnings: [],
  ip_behavior: {
    request_count: 2900,
    bytes_out: 8_000_000,
    countries: [
      {
        country_code: 'NG',
        country: 'Nigeria',
        unique_source_ips: 12,
        request_count: 2900,
        bytes_out: 8_000_000,
        status_counts: { '401': 1200, '403': 20 },
        first_seen_at: '2026-05-18T09:00:00Z',
        last_seen_at: '2026-05-18T10:00:00Z',
        top_asns: ['AS64500'],
        top_apps: ['Core Banking API'],
        server_groups: ['core-banking'],
      },
    ],
    findings: [
      {
        id: 'finding-1',
        source_ip: '203.0.113.10',
        country_code: 'NG',
        asn: 'AS64500',
        category: 'credential_attack',
        severity: 'critical',
        score: 91,
        reason:
          'credential_attack behavior from 203.0.113.10 scored 91: country/country-app behavior is being evaluated as a baseline dimension; request burst: 21 requests in 1m; auth failure spike: 21 401/403 responses',
        evidence: { request_count: 21, window: '1m', status_counts: { '401': 21 } },
        last_seen_at: '2026-05-18T09:55:00Z',
        drilldown: '/security/network?tab=ip-behavior&ip=203.0.113.10',
      },
    ],
  },
  webservers: {
    total: 1,
    capture_ready: 1,
    enforce_ready: 1,
    instances: [
      {
        id: 'web-1',
        node_id: 'node-1',
        kind: 'nginx',
        service: 'nginx',
        config_path: '/etc/nginx/nginx.conf',
        log_path: '/var/log/nginx/access.log',
        capture_ready: true,
        enforce_ready: true,
        observed_at: '2026-05-18T09:00:00Z',
      },
    ],
  },
  isolation: {
    online: 4,
    whitelist: 1,
    airgapped: 1,
    protected: 2,
    whitelist_gaps: 0,
    expired: 0,
    expiring_soon: 1,
    nodes: [
      {
        id: 'node-1',
        hostname: 'core-api-01',
        mode: 'whitelist',
        active: true,
        expired: false,
        local_only: false,
        expires_at: '2026-05-18T11:00:00Z',
        allowed_applications: ['control-one-agent', 'patch'],
      },
      {
        id: 'node-2',
        hostname: 'vault-offline-01',
        mode: 'airgapped',
        active: true,
        expired: false,
        local_only: true,
        expires_at: '2026-05-18T11:00:00Z',
      },
    ],
  },
  firewall: {
    enabled: 2,
    disabled: 0,
    unknown: 0,
    default_deny: 1,
    stale: 0,
    nodes: [
      {
        node_id: 'node-1',
        hostname: 'core-api-01',
        firewall_type: 'ufw',
        known: true,
        enabled: true,
        default_deny: true,
        stale: false,
        observed_at: '2026-05-18T09:55:00Z',
      },
    ],
  },
  pending_actions: [
    { id: 'ip-findings', label: 'IP findings to review', tone: 'warning', count: 1, drilldown: '/security/network?tab=ip-behavior' },
    { id: 'patch-approvals', label: 'Patch approvals', tone: 'warning', count: 1, drilldown: '/infrastructure/patch' },
    { id: 'isolation-posture', label: 'Isolation posture review', tone: 'warning', count: 1, drilldown: '/control-room/exposure' },
  ],
};

function lane(id: string, title: string, tone: ControlRoomOverview['lanes'][number]['tone'], summary: string): ControlRoomOverview['lanes'][number] {
  return {
    id,
    title,
    tone,
    score: tone === 'critical' ? 40 : 76,
    summary,
    primary_metric: { label: 'Primary', value: '1', tone, drilldown: '/alerts' },
    secondary_metric: { label: 'Secondary', value: '2', tone, drilldown: '/nodes' },
    drilldown: '/alerts',
    updated_at: '2026-05-18T10:00:00Z',
    metrics: [{ label: 'Detail', value: '3', tone, drilldown: '/alerts' }],
  };
}

let getControlRoomOverviewMock: ReturnType<typeof vi.fn>;
let planWebserverConfigMock: ReturnType<typeof vi.fn>;
let applyWebserverConfigMock: ReturnType<typeof vi.fn>;
let rollbackWebserverConfigMock: ReturnType<typeof vi.fn>;
let setNodeIsolationMock: ReturnType<typeof vi.fn>;

describe('ControlRoom', () => {
  beforeEach(() => {
    getControlRoomOverviewMock = vi.fn().mockResolvedValue(overview);
    planWebserverConfigMock = vi.fn().mockResolvedValue({
      job_id: 'plan-job-12345678',
      action_id: 'plan-action',
      status: 'queued',
    });
    applyWebserverConfigMock = vi.fn().mockResolvedValue({
      job_id: 'apply-job-12345678',
      action_id: 'apply-action',
      status: 'queued',
    });
    rollbackWebserverConfigMock = vi.fn().mockResolvedValue({
      job_id: 'rollback-job-12345678',
      action_id: 'rollback-action',
      status: 'queued',
    });
    setNodeIsolationMock = vi.fn().mockResolvedValue({});
    vi.spyOn(useApiClientModule, 'useApiClient').mockReturnValue({
      getControlRoomOverview: getControlRoomOverviewMock,
      planWebserverConfig: planWebserverConfigMock,
      applyWebserverConfig: applyWebserverConfigMock,
      rollbackWebserverConfig: rollbackWebserverConfigMock,
      setNodeIsolation: setNodeIsolationMock,
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } as any);
    vi.spyOn(useTenantModule, 'useTenant').mockReturnValue({
      currentTenantId: 'tenant-1',
      currentTenant: { id: 'tenant-1', name: 'Bank Tenant', created_at: '2024-01-01' },
      tenants: [],
      loading: false,
      error: null,
      setCurrentTenantId: vi.fn(),
      refresh: vi.fn(),
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('waits for tenant selection before requesting overview data', () => {
    vi.mocked(useTenantModule.useTenant).mockReturnValue({
      currentTenantId: null,
      currentTenant: null,
      tenants: [],
      loading: true,
      error: null,
      setCurrentTenantId: vi.fn(),
      refresh: vi.fn(),
    });

    render(
      <MemoryRouter>
        <ControlRoom />
      </MemoryRouter>,
    );

    expect(getControlRoomOverviewMock).not.toHaveBeenCalled();
    expect(screen.queryByText('Overview data unavailable')).not.toBeInTheDocument();
  });

  it('renders the six control room lanes and backend IP anomaly score', async () => {
    render(
      <MemoryRouter>
        <ControlRoom />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(screen.getByText('Server Health')).toBeInTheDocument();
    });

    expect(screen.getByText('Security')).toBeInTheDocument();
    expect(screen.getByText('App/DB Health')).toBeInTheDocument();
    expect(screen.getByText('Exposure Confidence')).toBeInTheDocument();
    expect(screen.getAllByText(/Security confidence/i).length).toBeGreaterThan(0);
    expect(screen.getByText('Connection/IP Behavior')).toBeInTheDocument();
    expect(screen.getByText('Patch Posture')).toBeInTheDocument();
    expect(screen.getByText(/1 open incidents, 3 pending actions, 1 recent IP finding \(24h\)/i)).toBeInTheDocument();
    expect(screen.getByText('Open incidents')).toBeInTheDocument();
    expect(screen.getByText('Health incidents')).toBeInTheDocument();
    expect(screen.queryByText('Broken')).not.toBeInTheDocument();
    expect(screen.getByText('Patch failures')).toBeInTheDocument();
    expect(screen.getAllByText('91%').length).toBeGreaterThan(0);
    expect(screen.getAllByText(/91% confidence/i).length).toBeGreaterThan(0);
    expect(screen.queryByText(/baseline dimension/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/scored 91:/i)).not.toBeInTheDocument();
    expect(screen.getByText(/webserver auto-control/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/network isolation/i)).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText(/network isolation/i));
    expect(screen.getByText(/Firewall posture/i)).toBeInTheDocument();
    expect(screen.getByText('core-api-01')).toBeInTheDocument();
    expect(screen.getByText('vault-offline-01')).toBeInTheDocument();
    expect(screen.getAllByText(/Whitelist gaps/i).length).toBeGreaterThan(0);
  });

  it('does not show healthy empty states when the overview fails to load', async () => {
    getControlRoomOverviewMock.mockRejectedValueOnce(new Error('overview store offline'));

    render(
      <MemoryRouter>
        <ControlRoom />
      </MemoryRouter>,
    );

    expect(await screen.findByRole('alert')).toHaveTextContent('overview store offline');
    expect(screen.getByText('Control Room data could not be loaded')).toBeInTheDocument();
    expect(screen.getByText('Control lanes unavailable')).toBeInTheDocument();
    expect(screen.getByText('Operator queue unavailable')).toBeInTheDocument();
    expect(screen.getByText('IP behavior unavailable')).toBeInTheDocument();
    expect(screen.getByText('Incidents unavailable')).toBeInTheDocument();
    expect(screen.getByText('Webserver inventory unavailable')).toBeInTheDocument();
    expect(screen.queryByText('No open incidents')).not.toBeInTheDocument();
    expect(screen.queryByText('No approvals waiting')).not.toBeInTheDocument();
    expect(screen.queryByText('No open IP behavior anomalies')).not.toBeInTheDocument();
    expect(screen.queryByText('No webserver inventory yet')).not.toBeInTheDocument();
    expect(screen.queryByText('No lanes yet')).not.toBeInTheDocument();
  });

  it('marks last-known data stale when a refresh fails', async () => {
    render(
      <MemoryRouter>
        <ControlRoom />
      </MemoryRouter>,
    );

    await waitFor(() => expect(screen.getByText('Server Health')).toBeInTheDocument());
    getControlRoomOverviewMock.mockRejectedValueOnce(new Error('refresh unavailable'));

    fireEvent.click(screen.getByRole('button', { name: /refresh/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Refresh failed. The data below is last known data for 24h: refresh unavailable',
    );
    expect(screen.getByText('Server Health')).toBeInTheDocument();
    expect(screen.getByText(/Last known data: Bank Tenant:/)).toBeInTheDocument();
  });

  it('uses an in-app confirmation and keeps failed webserver apply visible', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.spyOn(window, 'confirm');
    applyWebserverConfigMock.mockRejectedValueOnce(new Error('webserver gate denied'));

    render(
      <MemoryRouter>
        <ControlRoom />
      </MemoryRouter>,
    );

    await user.click(await screen.findByRole('button', { name: /apply webserver control for nginx nginx/i }));
    const dialog = screen.getByRole('dialog', { name: /apply webserver control/i });
    expect(dialog).toHaveTextContent('Apply the managed Control One capture/enforcement policy to nginx nginx.');
    expect(confirmSpy).not.toHaveBeenCalled();

    await user.click(within(dialog).getByRole('button', { name: /^apply$/i }));

    expect(await within(dialog).findByRole('alert')).toHaveTextContent(
      'Apply failed for nginx nginx: webserver gate denied',
    );
    expect(screen.getByText('apply: Apply failed for nginx nginx: webserver gate denied')).toBeInTheDocument();
    expect(applyWebserverConfigMock).toHaveBeenCalledWith('web-1', expect.objectContaining({
      tenant_id: 'tenant-1',
      node_id: 'node-1',
    }));
  });

  it('uses an in-app confirmation and keeps failed isolation changes visible', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.spyOn(window, 'confirm');
    setNodeIsolationMock.mockRejectedValueOnce(new Error('isolation gate denied'));

    render(
      <MemoryRouter>
        <ControlRoom />
      </MemoryRouter>,
    );

    await user.click(await screen.findByLabelText(/network isolation/i));
    await user.click(screen.getByRole('button', { name: /airgap core-api-01 for 1 hour/i }));
    const dialog = screen.getByRole('dialog', { name: /change network isolation/i });
    expect(dialog).toHaveTextContent('Airgap core-api-01.');
    expect(confirmSpy).not.toHaveBeenCalled();

    await user.click(within(dialog).getByRole('button', { name: /airgap node/i }));

    expect(await within(dialog).findByRole('alert')).toHaveTextContent(
      'Airgap core-api-01 failed: isolation gate denied',
    );
    expect(screen.getAllByText('Airgap core-api-01 failed: isolation gate denied').length).toBeGreaterThanOrEqual(2);
    expect(setNodeIsolationMock).toHaveBeenCalledWith('node-1', expect.objectContaining({
      mode: 'airgapped',
      duration_seconds: 3600,
      reason: 'Isolation set from Control Room',
    }));
  });
});
