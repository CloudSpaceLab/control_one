import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
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

describe('ControlRoom', () => {
  beforeEach(() => {
    vi.spyOn(useApiClientModule, 'useApiClient').mockReturnValue({
      getControlRoomOverview: vi.fn().mockResolvedValue(overview),
      planWebserverConfig: vi.fn(),
      applyWebserverConfig: vi.fn(),
      rollbackWebserverConfig: vi.fn(),
      setNodeIsolation: vi.fn(),
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
});
