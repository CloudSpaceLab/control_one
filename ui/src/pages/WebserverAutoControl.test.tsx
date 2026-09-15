import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { WebserverInstance } from '../lib/api';
import { WebserverAutoControl } from './WebserverAutoControl';

const mocks = vi.hoisted(() => {
  const listWebserverInstances = vi.fn();
  const listWebserverConfigActions = vi.fn();
  const listWebserverConfigReceipts = vi.fn();
  const planWebserverConfig = vi.fn();
  const applyWebserverConfig = vi.fn();
  const rollbackWebserverConfig = vi.fn();

  return {
    listWebserverInstances,
    listWebserverConfigActions,
    listWebserverConfigReceipts,
    planWebserverConfig,
    applyWebserverConfig,
    rollbackWebserverConfig,
    apiClient: {
      listWebserverInstances,
      listWebserverConfigActions,
      listWebserverConfigReceipts,
      planWebserverConfig,
      applyWebserverConfig,
      rollbackWebserverConfig,
    },
  };
});

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

vi.mock('../providers/TenantProvider', () => ({
  useTenant: () => ({
    currentTenantId: 'tenant-1',
    currentTenant: { id: 'tenant-1', name: 'Bank Tenant' },
  }),
}));

const webserver: WebserverInstance = {
  ID: 'web-1',
  TenantID: 'tenant-1',
  NodeID: 'node-1',
  Kind: 'nginx',
  Version: '1.24',
  ServiceName: 'nginx',
  ConfigPath: '/etc/nginx/nginx.conf',
  AccessLogPath: '/var/log/nginx/access.log',
  ErrorLogPath: '/var/log/nginx/error.log',
  VHosts: [{ name: 'core-bank', document_root: '/srv/core-bank', evidence: ['nginx.conf'] }],
  Capabilities: { enforce_supported: true, capture_supported: true },
  ObservedAt: '2026-06-08T00:00:00Z',
};

function paginated<T>(data: T[]) {
  return {
    data,
    pagination: { total: data.length, count: data.length, limit: 20, offset: 0, nextOffset: null, prevOffset: null },
  };
}

function renderWebservers() {
  return render(
    <MemoryRouter>
      <WebserverAutoControl />
    </MemoryRouter>,
  );
}

describe('WebserverAutoControl production hardening', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listWebserverInstances.mockResolvedValue(paginated([webserver]));
    mocks.listWebserverConfigActions.mockResolvedValue(paginated([]));
    mocks.listWebserverConfigReceipts.mockResolvedValue(paginated([]));
    mocks.planWebserverConfig.mockResolvedValue({ job_id: 'plan-job-12345678', action_id: 'action-plan', status: 'queued' });
    mocks.applyWebserverConfig.mockResolvedValue({ job_id: 'apply-job-12345678', action_id: 'action-apply', status: 'queued' });
    mocks.rollbackWebserverConfig.mockResolvedValue({ job_id: 'rollback-job-12345678', action_id: 'action-rollback', status: 'queued' });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('shows inventory failures as unavailable instead of false empty inventory', async () => {
    mocks.listWebserverInstances.mockRejectedValueOnce(new Error('inventory store offline'));

    renderWebservers();

    await waitFor(() => expect(mocks.listWebserverInstances).toHaveBeenCalledWith({ tenantId: 'tenant-1', limit: 200 }));
    expect(await screen.findAllByText('Webserver inventory unavailable')).toHaveLength(2);
    expect(screen.getByRole('alert')).toHaveTextContent('inventory store offline');
    expect(screen.queryByText('No webserver inventory')).not.toBeInTheDocument();
    expect(screen.getAllByText('N/A')).toHaveLength(4);
  });

  it('uses in-app confirmation and keeps failed apply actions visible', async () => {
    const user = userEvent.setup();
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    mocks.applyWebserverConfig.mockRejectedValueOnce(new Error('webserver gate denied'));

    renderWebservers();

    await user.click(await screen.findByRole('button', { name: /apply capture for nginx nginx/i }));
    expect(confirmSpy).not.toHaveBeenCalled();
    expect(mocks.applyWebserverConfig).not.toHaveBeenCalled();

    const dialog = screen.getByRole('dialog', { name: /apply capture webserver control/i });
    expect(dialog).toHaveTextContent('Apply capture for nginx nginx');
    await user.click(within(dialog).getByRole('button', { name: 'Apply capture' }));

    const message = 'Apply capture failed for nginx nginx: webserver gate denied';
    await waitFor(() => expect(mocks.applyWebserverConfig).toHaveBeenCalledTimes(1));
    expect(mocks.applyWebserverConfig).toHaveBeenCalledWith('web-1', expect.objectContaining({
      tenant_id: 'tenant-1',
      node_id: 'node-1',
      policy: expect.objectContaining({ mode: 'capture', approval_required: true }),
    }));
    expect(screen.getByRole('dialog', { name: /apply capture webserver control/i })).toBeInTheDocument();
    expect(screen.getByText('Webserver action failed')).toBeInTheDocument();
    expect(screen.getAllByText(message)).toHaveLength(2);
  });

  it('surfaces action history failures without false empty history or receipts', async () => {
    mocks.listWebserverConfigActions.mockRejectedValueOnce(new Error('history store offline'));

    renderWebservers();

    expect(await screen.findByText('Webserver action history unavailable')).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('history store offline');
    expect(screen.getByText('Config action history unavailable')).toBeInTheDocument();
    expect(screen.getByText('Receipts unavailable')).toBeInTheDocument();
    expect(screen.queryByText('No config actions')).not.toBeInTheDocument();
    expect(screen.queryByText('No receipts')).not.toBeInTheDocument();
  });
});
