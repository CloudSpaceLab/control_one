import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ServiceInventory } from './ServiceInventory';

const mocks = vi.hoisted(() => {
  const listTenantNodeServices = vi.fn();
  return {
    listTenantNodeServices,
    apiClient: { listTenantNodeServices },
  };
});

vi.mock('@/hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

describe('ServiceInventory', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listTenantNodeServices.mockImplementation(
      async ({ targetScope }: { targetScope?: string }) => {
        const endpoint = {
          id: 'svc-endpoint',
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
        };
        const server = {
          id: 'svc-server',
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
        };
        const data =
          targetScope === 'endpoint'
            ? [endpoint]
            : targetScope === 'server'
              ? [server]
              : [server, endpoint];
        return {
          data,
          pagination: {
            total: data.length,
            count: data.length,
            limit: 50,
            offset: 0,
            nextOffset: null,
            prevOffset: null,
          },
        };
      },
    );
  });

  it('lists services across nodes with device context', async () => {
    render(
      <MemoryRouter>
        <ServiceInventory tenantId="tenant-1" tenantLabel="Bank Tenant" />
      </MemoryRouter>,
    );

    expect(await screen.findByText('Payments gateway')).toBeInTheDocument();
    expect(screen.getByText('staff-laptop-17')).toBeInTheDocument();
    expect(screen.getByText('Server / infrastructure')).toBeInTheDocument();
    expect(screen.getByText('Employee endpoint')).toBeInTheDocument();
    expect(screen.getByText('all interfaces:443')).toBeInTheDocument();
    expect(mocks.listTenantNodeServices).toHaveBeenCalledWith(
      expect.objectContaining({
        tenantId: 'tenant-1',
        targetScope: 'all',
        limit: 50,
        offset: 0,
      }),
    );
  });

  it('requests endpoint-only inventory when the device filter changes', async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <ServiceInventory tenantId="tenant-1" tenantLabel="Bank Tenant" />
      </MemoryRouter>,
    );

    await screen.findByText('Payments gateway');
    await user.selectOptions(screen.getByLabelText('Device scope'), 'endpoint');

    await waitFor(() =>
      expect(mocks.listTenantNodeServices).toHaveBeenLastCalledWith(
        expect.objectContaining({ targetScope: 'endpoint', offset: 0 }),
      ),
    );
    expect(await screen.findByText('staff-laptop-17')).toBeInTheDocument();
    expect(screen.queryByText('Payments gateway')).not.toBeInTheDocument();
  });

  it('does not imply discovered service health', async () => {
    render(
      <MemoryRouter>
        <ServiceInventory tenantId="tenant-1" tenantLabel="Bank Tenant" />
      </MemoryRouter>,
    );

    await screen.findByText('Payments gateway');
    expect(screen.getByText(/Discovery shows presence, not application health/i)).toBeInTheDocument();
    expect(screen.queryByText(/^Healthy$/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/^Stale$/i)).not.toBeInTheDocument();
  });
});
