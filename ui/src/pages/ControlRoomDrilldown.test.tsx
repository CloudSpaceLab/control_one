import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { ControlRoomDrilldown } from './ControlRoomDrilldown';
import * as useApiClientModule from '../hooks/useApiClient';
import * as useTenantModule from '../providers/TenantProvider';
import type { ControlRoomOverview } from '../lib/api';

const overview: ControlRoomOverview = {
  tenant_id: 'tenant-1',
  generated_at: '2026-05-18T10:00:00Z',
  period: '24h',
  lanes: [
    {
      id: 'exposure',
      title: 'Current Exposure',
      tone: 'warning',
      score: 72,
      summary: '2 public listeners need review',
      primary_metric: { label: 'Public listeners', value: '2', tone: 'warning', drilldown: '/security/network' },
      secondary_metric: { label: 'Protected listeners', value: '4', tone: 'healthy', drilldown: '/control-room/exposure' },
      metrics: [
        { label: 'Firewall default deny', value: '1', tone: 'healthy' },
        { label: 'Firewall unknown/off', value: '1', tone: 'warning' },
      ],
      items: [
        {
          label: 'edge-web-02',
          value: 'open',
          tone: 'warning',
          hint: '443/tcp public listener without default-deny posture',
          drilldown: '/nodes/node-2',
        },
      ],
      drilldown: '/security/network',
      updated_at: '2026-05-18T09:55:00Z',
    },
  ],
  top_incidents: [],
  stale_warnings: [],
  ip_behavior: {
    request_count: 0,
    bytes_out: 0,
    countries: [],
    findings: [],
  },
  webservers: {
    total: 0,
    capture_ready: 0,
    enforce_ready: 0,
    instances: [],
  },
  isolation: {
    online: 3,
    whitelist: 1,
    airgapped: 1,
    protected: 2,
    whitelist_gaps: 1,
    expired: 0,
    expiring_soon: 0,
    nodes: [],
  },
  firewall: {
    enabled: 1,
    disabled: 1,
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
      {
        node_id: 'node-2',
        hostname: 'edge-web-02',
        firewall_type: 'nftables',
        known: true,
        enabled: false,
        default_deny: false,
        stale: false,
        observed_at: '2026-05-18T09:54:00Z',
      },
    ],
  },
  pending_actions: [
    { id: 'block-enforcement', label: 'Block enforcement gaps', tone: 'warning', count: 1, drilldown: '/security/network?tab=blocks' },
    { id: 'isolation-posture', label: 'Isolation posture review', tone: 'warning', count: 1, drilldown: '/control-room/exposure' },
  ],
};

const appDBOverview: ControlRoomOverview = {
  ...overview,
  lanes: [
    {
      id: 'app-db-health',
      title: 'App/DB Health',
      tone: 'warning',
      score: 76,
      summary: '1 app/web, 1 DB, 0 cache services, 2 inferred purposes',
      primary_metric: { label: 'App/DB services', value: '2', tone: 'info', drilldown: '/nodes' },
      secondary_metric: { label: 'Missing web capture', value: '1', tone: 'warning', drilldown: '/security/webservers' },
      metrics: [
        { label: 'Databases', value: '1', tone: 'info', drilldown: '/data-security' },
        { label: 'Web roots', value: '1', tone: 'info', drilldown: '/security/webservers' },
        { label: 'Capture ready', value: '0/1', tone: 'warning', drilldown: '/security/webservers' },
      ],
      items: [
        { label: 'nginx', value: 'no capture', tone: 'warning', hint: '/etc/nginx/nginx.conf', drilldown: '/security/webservers' },
      ],
      drilldown: '/nodes',
      updated_at: '2026-05-18T09:55:00Z',
    },
  ],
  webservers: {
    total: 1,
    capture_ready: 0,
    enforce_ready: 1,
    instances: [
      {
        id: 'web-1',
        node_id: 'node-1',
        kind: 'nginx',
        service: 'nginx',
        config_path: '/etc/nginx/nginx.conf',
        capture_ready: false,
        enforce_ready: true,
        vhosts: [
          {
            server_name: 'core.example.bank',
            document_root: '/srv/core-banking',
            application_type: 'go',
            application_name: 'Go application',
            coverage_state: 'profile_available',
            parser_profile_id: 'go',
            catalog_version: 'builtin-2026-05-18',
            confidence: 86,
            evidence: ['go.mod'],
          },
        ],
        capabilities: {
          server_purposes: ['web_server', 'app_node'],
        },
        observed_at: '2026-05-18T09:55:00Z',
      },
    ],
  },
};

const appDBCoverageOverview: ControlRoomOverview = {
  ...appDBOverview,
  webservers: {
    total: 4,
    capture_ready: 2,
    enforce_ready: 3,
    instances: [
      {
        id: 'web-healthy',
        node_id: 'node-healthy',
        kind: 'nginx',
        service: 'nginx',
        capture_ready: true,
        enforce_ready: true,
        vhosts: [{
          server_name: 'healthy.example.bank',
          document_root: '/srv/healthy',
          application_name: 'Go application',
          application_type: 'go',
          coverage_state: 'profile_available',
          parser_profile_id: 'go',
          remediation_skill_id: 'go-remediation',
          confidence: 90,
          evidence: ['go.mod'],
        }],
        capabilities: { server_purposes: ['web_server', 'app_node'] },
        observed_at: '2026-05-18T09:55:00Z',
      },
      {
        id: 'web-airgap',
        node_id: 'node-airgap',
        kind: 'apache',
        service: 'apache2',
        capture_ready: false,
        enforce_ready: true,
        vhosts: [{
          server_name: 'airgap.example.bank',
          document_root: '/srv/airgap',
          application_name: 'Unknown application',
          application_type: 'unknown',
          coverage_state: 'skill_required',
          parser_profile_id: 'custom-parser-profile',
          remediation_skill_id: 'custom-remediation-skill',
          confidence: 30,
          evidence: ['path:airgap'],
        }],
        capabilities: { server_purposes: ['app_node'] },
        observed_at: '2026-05-18T09:55:00Z',
      },
      {
        id: 'web-generic',
        node_id: 'node-generic',
        kind: 'tomcat',
        service: 'tomcat',
        capture_ready: true,
        enforce_ready: false,
        vhosts: [{
          server_name: 'generic.example.bank',
          document_root: '/srv/generic',
          application_name: 'Static web content',
          application_type: 'static_web',
          coverage_state: 'generic_access_log',
          parser_profile_id: 'generic-web',
          confidence: 70,
          evidence: ['index.html'],
        }],
        capabilities: { server_purposes: ['app_node'] },
        observed_at: '2026-05-18T09:55:00Z',
      },
      {
        id: 'web-db',
        node_id: 'node-db',
        kind: 'iis',
        service: 'w3svc',
        capture_ready: false,
        enforce_ready: true,
        vhosts: [{
          server_name: 'db.example.bank',
          document_root: 'C:/apps/customdb',
          application_name: 'Custom database gateway',
          application_type: 'database',
          coverage_state: 'unsupported_dbms',
          parser_profile_id: 'custom-parser-profile',
          remediation_skill_id: 'custom-remediation-skill',
          confidence: 40,
          evidence: ['customdb.conf'],
        }],
        capabilities: { server_purposes: ['db_node', 'app_node'] },
        observed_at: '2026-05-18T09:55:00Z',
      },
    ],
  },
  isolation: {
    ...appDBOverview.isolation,
    nodes: [
      { id: 'node-airgap', hostname: 'airgap-node', mode: 'airgapped', active: true, expired: false, local_only: true },
      { id: 'node-healthy', hostname: 'healthy-node', mode: 'online', active: false, expired: false, local_only: false },
      { id: 'node-generic', hostname: 'generic-node', mode: 'online', active: false, expired: false, local_only: false },
      { id: 'node-db', hostname: 'db-node', mode: 'online', active: false, expired: false, local_only: false },
    ],
  },
};

let mockedOverview: ControlRoomOverview;
let getControlRoomOverviewMock: ReturnType<typeof vi.fn>;

describe('ControlRoomDrilldown', () => {
  beforeEach(() => {
    mockedOverview = overview;
    getControlRoomOverviewMock = vi.fn().mockImplementation(() => Promise.resolve(mockedOverview));
    vi.spyOn(useApiClientModule, 'useApiClient').mockReturnValue({
      getControlRoomOverview: getControlRoomOverviewMock,
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

  it('waits for tenant selection before requesting drilldown data', () => {
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
      <MemoryRouter initialEntries={['/control-room/exposure']}>
        <Routes>
          <Route path="/control-room/:laneId" element={<ControlRoomDrilldown />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(getControlRoomOverviewMock).not.toHaveBeenCalled();
    expect(screen.queryByText('Control Room data unavailable')).not.toBeInTheDocument();
  });

  it('shows firewall and isolation posture inside the exposure drilldown', async () => {
    render(
      <MemoryRouter initialEntries={['/control-room/exposure']}>
        <Routes>
          <Route path="/control-room/:laneId" element={<ControlRoomDrilldown />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByText('Exposure protection posture')).toBeInTheDocument();
    expect(screen.getByText('Firewall active')).toBeInTheDocument();
    expect(screen.getByText('Default deny')).toBeInTheDocument();
    expect(screen.getByText('Unknown/off')).toBeInTheDocument();
    expect(screen.getByText('Isolation protected')).toBeInTheDocument();
    expect(screen.getByText('Whitelist gaps')).toBeInTheDocument();
    expect(screen.getByText('core-api-01')).toBeInTheDocument();
    expect(screen.getAllByText('edge-web-02').length).toBeGreaterThan(0);
    expect(screen.getByText('counts as protected')).toBeInTheDocument();
    expect(screen.getByText('not reducing exposure')).toBeInTheDocument();
    expect(screen.getByText('Isolation posture review')).toBeInTheDocument();
    expect(screen.getByText(/lack airgap, whitelist mode, or default-deny firewall protection/i)).toBeInTheDocument();
  });

  it('shows app roots, inferred purposes, and webserver control from the app/db drilldown', async () => {
    mockedOverview = appDBOverview;

    render(
      <MemoryRouter initialEntries={['/control-room/app-db-health']}>
        <Routes>
          <Route path="/control-room/:laneId" element={<ControlRoomDrilldown />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByText('Detected apps, DBMS, and parser coverage')).toBeInTheDocument();
    expect(screen.getByText('core.example.bank')).toBeInTheDocument();
    expect(screen.getByText('/srv/core-banking')).toBeInTheDocument();
    expect(screen.getByText('Go application - 86% confidence')).toBeInTheDocument();
    expect(screen.getByText('go.mod')).toBeInTheDocument();
    expect(screen.getByText('builtin-2026-05-18')).toBeInTheDocument();
    expect(screen.getAllByText('go').length).toBeGreaterThan(0);
    expect(screen.getByText(/web_server, app_node/i)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /open webserver control/i })).toHaveAttribute('href', '/security/webservers');
  });

  it('filters app/db coverage by skill, generic, unsupported, direct, and airgapped states', async () => {
    mockedOverview = appDBCoverageOverview;

    render(
      <MemoryRouter initialEntries={['/control-room/app-db-health']}>
        <Routes>
          <Route path="/control-room/:laneId" element={<ControlRoomDrilldown />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByText('healthy.example.bank')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /skills needed/i }));
    expect(screen.getByText('airgap.example.bank')).toBeInTheDocument();
    expect(screen.getByText('db.example.bank')).toBeInTheDocument();
    expect(screen.queryByText('healthy.example.bank')).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /generic parser/i }));
    expect(screen.getByText('generic.example.bank')).toBeInTheDocument();
    expect(screen.queryByText('airgap.example.bank')).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /unsupported dbms/i }));
    expect(screen.getByText('db.example.bank')).toBeInTheDocument();
    expect(screen.queryByText('generic.example.bank')).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /airgap bundle gaps/i }));
    expect(screen.getByText('airgap.example.bank')).toBeInTheDocument();
    expect(screen.queryByText('db.example.bank')).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /direct edge/i }));
    expect(screen.getByText('healthy.example.bank')).toBeInTheDocument();
    expect(screen.getByText('db.example.bank')).toBeInTheDocument();
    expect(screen.queryByText('airgap.example.bank')).not.toBeInTheDocument();
  });
});
