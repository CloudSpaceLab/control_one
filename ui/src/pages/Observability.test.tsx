import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Observability } from './Observability';

const mocks = vi.hoisted(() => {
  const listWebserverInstances = vi.fn();
  const getContentPackSourceHealth = vi.fn();
  const listTenantNodeServices = vi.fn();
  return {
    listWebserverInstances,
    getContentPackSourceHealth,
    listTenantNodeServices,
    apiClient: {
      listWebserverInstances,
      getContentPackSourceHealth,
      listTenantNodeServices,
    },
  };
});

vi.mock('@/providers/TenantProvider', () => ({
  useTenant: () => ({
    currentTenantId: 'tenant-1',
    currentTenant: { id: 'tenant-1', name: 'Bank Tenant', created_at: '2026-01-01T00:00:00Z' },
  }),
}));

vi.mock('@/hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

vi.mock('@/hooks/useNodes', () => ({
  useNodes: () => ({
    data: [
      {
        id: 'node-1',
        tenant_id: 'tenant-1',
        hostname: 'prod-web-01',
        os: 'linux',
        state: 'active',
        agent_version: '1.2.3',
        last_seen_at: new Date().toISOString(),
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      },
    ],
    pagination: { total: 1, count: 1, limit: 200, offset: 0, nextOffset: null, prevOffset: null },
    loading: false,
    error: null,
    reload: vi.fn(),
  }),
}));

vi.mock('@/hooks/useCoverageMatrix', () => ({
  useCoverageMatrix: () => ({
    data: {
      rows: [
        {
          domain: 'db_audit',
          title: 'Database audit coverage',
          coverage_state: 'manual_evidence',
          reason: 'Manual evidence remains required for DB controls.',
          gaps: ['audit source not verified'],
          evidence: ['coverage:db_audit'],
        },
      ],
    },
    loading: false,
    error: null,
    unavailable: false,
    reload: vi.fn(),
  }),
}));

describe('Observability', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listWebserverInstances.mockResolvedValue({
      data: [
        {
          ID: 'web-1',
          TenantID: 'tenant-1',
          NodeID: 'node-1',
          Kind: 'nginx',
          Version: 'nginx/1.20.1',
          ServiceName: 'edge',
          ConfigPath: '/etc/nginx/nginx.conf',
          AccessLogPath: '/var/log/nginx/access.log',
          ErrorLogPath: '/var/log/nginx/error.log',
          VHosts: [{ server_name: 'bank.example' }],
          Capabilities: {},
          ObservedAt: '2026-06-06T12:00:00Z',
        },
        {
          ID: 'web-2',
          TenantID: 'tenant-1',
          NodeID: 'node-1',
          Kind: 'nginx',
          Version: 'nginx version: nginx/1.20.1',
          ServiceName: 'nginx',
          ConfigPath: '/etc/nginx/nginx.conf',
          AccessLogPath: '/var/log/nginx/access.log',
          ErrorLogPath: '/var/log/nginx/error.log',
          VHosts: [{ server_name: 'bank.example' }],
          Capabilities: {},
          ObservedAt: '2026-06-06T12:00:00Z',
        },
      ],
      pagination: { total: 2, count: 2, limit: 100, offset: 0, nextOffset: null, prevOffset: null },
    });
    mocks.getContentPackSourceHealth.mockResolvedValue({
      tenant_id: 'tenant-1',
      generated_at: '2026-06-06T12:00:00Z',
      items: [
        {
          runtime_state_id: 'runtime-1',
          collector_id: 'node-agent',
          source_id: 'postgres.audit',
          display_name: 'PostgreSQL audit',
          coverage_state: 'approval_required',
          parser_id: 'postgres',
          metrics: { events_received: 10, events_parsed: 8 },
          approval_required: true,
        },
      ],
      totals: { sources: 1, collectors_reporting: 1, by_state: { approval_required: 1 } },
    });
    mocks.listTenantNodeServices.mockResolvedValue({
      data: [
        {
          id: 'service-1',
          node_id: 'node-server',
          tenant_id: 'tenant-1',
          pid: 100,
          process: 'nginx',
          binary_path: '/usr/sbin/nginx',
          listen_addr: '0.0.0.0',
          port: 443,
          service_kind: 'nginx',
          app_name: 'Payments gateway',
          app_profile_id: 'nginx',
          app_confidence: 90,
          observed_at: new Date().toISOString(),
          node_hostname: 'prod-web-01',
          node_target_type: 'server',
          node_state: 'active',
          node_last_seen_at: new Date().toISOString(),
        },
        {
          id: 'service-2',
          node_id: 'node-endpoint',
          tenant_id: 'tenant-1',
          pid: 200,
          process: 'local-helper',
          binary_path: 'C:\\Program Files\\Helper\\helper.exe',
          listen_addr: '127.0.0.1',
          port: 4317,
          service_kind: 'unknown',
          observed_at: new Date().toISOString(),
          node_hostname: 'staff-laptop-17',
          node_target_type: 'workstation',
          node_state: 'active',
          node_last_seen_at: new Date().toISOString(),
        },
      ],
      pagination: {
        total: 2,
        count: 2,
        limit: 50,
        offset: 0,
        nextOffset: null,
        prevOffset: null,
      },
    });
  });

  it('builds the operator stack from live tenant signals', async () => {
    render(
      <MemoryRouter>
        <Observability />
      </MemoryRouter>,
    );

    expect(screen.getByRole('heading', { name: 'Observability' })).toBeInTheDocument();
    expect((await screen.findAllByText('PostgreSQL audit')).length).toBeGreaterThan(0);
    expect(screen.getAllByText('nginx edge').length).toBeGreaterThan(0);
    expect(screen.getByText('Bank Tenant source setup snapshot')).toBeInTheDocument();
    expect(screen.getAllByText('live data').length).toBeGreaterThan(0);
    expect(await screen.findByText('Payments gateway')).toBeInTheDocument();
    expect(screen.getByText('staff-laptop-17')).toBeInTheDocument();
    expect(screen.getByText('Employee endpoint')).toBeInTheDocument();
    expect(screen.getByText('Server / infrastructure')).toBeInTheDocument();
    expect(screen.getByText(/Discovery shows presence, not application health/i)).toBeInTheDocument();
    expect(screen.queryByText(/payments-api/i)).not.toBeInTheDocument();
    expect(screen.getAllByText(/Grant least-privilege access for PostgreSQL audit/i).length).toBeGreaterThan(0);
    expect(document.body.textContent).not.toMatch(/nginx nginx/i);
    expect(document.body.textContent).not.toMatch(/version nginx version/i);
  });

  it('keeps debug start blocked until safety fields are explicit and exposes live citations', async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <Observability />
      </MemoryRouter>,
    );

    await screen.findAllByText('PostgreSQL audit');
    await user.click(screen.getByRole('button', { name: /debug session/i }));
    const dialog = screen.getByRole('dialog');
    expect(within(dialog).getByRole('button', { name: /start preview/i })).toBeDisabled();

    fireEvent.change(within(dialog).getByLabelText(/scope/i), { target: { value: 'postgres' } });
    fireEvent.change(within(dialog).getByLabelText(/ttl minutes/i), { target: { value: '30' } });
    fireEvent.change(within(dialog).getByLabelText(/reason/i), { target: { value: 'Audit proof check' } });
    fireEvent.change(within(dialog).getByLabelText(/quota/i), { target: { value: '100 MB' } });
    fireEvent.change(within(dialog).getByLabelText(/rollback plan/i), { target: { value: 'Restore baseline log level' } });
    fireEvent.click(within(dialog).getByLabelText(/approval state reviewed/i));
    expect(within(dialog).getByRole('button', { name: /start preview/i })).toBeEnabled();

    await user.click(within(dialog).getByRole('button', { name: /close/i }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await user.click(screen.getByRole('button', { name: /Needs approval evidenceattention PostgreSQL audit/i }));
    expect(screen.getByText('observability:source-health:runtime-1')).toBeInTheDocument();
  });

  it('keeps rendering when live observability payloads omit arrays', async () => {
    mocks.listWebserverInstances.mockResolvedValueOnce({
      pagination: { total: 0, count: 0, limit: 100, offset: 0, nextOffset: null, prevOffset: null },
    });
    mocks.getContentPackSourceHealth.mockResolvedValueOnce({
      tenant_id: 'tenant-1',
      generated_at: '2026-06-06T12:00:00Z',
      totals: { sources: 0, collectors_reporting: 0, by_state: {} },
    });

    render(
      <MemoryRouter>
        <Observability />
      </MemoryRouter>,
    );

    expect(await screen.findByRole('heading', { name: 'Observability' })).toBeInTheDocument();
    expect(screen.getAllByText('live data').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Database audit coverage').length).toBeGreaterThan(0);
  });
});
