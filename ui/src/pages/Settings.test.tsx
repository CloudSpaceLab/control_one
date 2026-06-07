import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Webhook } from '../lib/api';
import { Settings } from './Settings';

const mocks = vi.hoisted(() => {
  const createWebhook = vi.fn();
  const updateWebhook = vi.fn();
  const deleteWebhook = vi.fn();
  const testWebhook = vi.fn();
  const reloadWebhooks = vi.fn();
  const showToast = vi.fn();

  return {
    webhooks: [] as Webhook[],
    createWebhook,
    updateWebhook,
    deleteWebhook,
    testWebhook,
    reloadWebhooks,
    showToast,
    apiClient: {
      createWebhook,
      updateWebhook,
      deleteWebhook,
      testWebhook,
      listMFAFactors: vi.fn().mockResolvedValue({ factors: [] }),
      deleteMFAFactor: vi.fn().mockResolvedValue(undefined),
      beginTOTPEnroll: vi.fn(),
      finishTOTPEnroll: vi.fn(),
      beginWebAuthnEnroll: vi.fn(),
      generateMFARecoveryCodes: vi.fn().mockResolvedValue({ codes: [] }),
    },
  };
});

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

vi.mock('../hooks/useTenants', () => ({
  useTenants: () => ({
    data: [{ id: 'tenant-a', name: 'Tenant A' }],
    loading: false,
    error: null,
  }),
}));

vi.mock('../providers/TenantProvider', () => ({
  useTenant: () => ({ currentTenantId: 'tenant-a' }),
}));

vi.mock('../providers/ToastProvider', () => ({
  useToast: () => ({
    toasts: [],
    showToast: mocks.showToast,
    dismissToast: vi.fn(),
  }),
}));

vi.mock('../hooks/useWorkerStatus', () => ({
  useWorkerStatus: () => ({
    status: {
      active: 0,
      queue_depth: 0,
      backend: 'asynq',
      started: true,
    },
    loading: false,
    error: null,
    refresh: vi.fn(),
  }),
}));

vi.mock('../hooks/useWebhooks', () => ({
  useWebhooks: () => ({
    data: mocks.webhooks,
    loading: false,
    error: null,
    pagination: { total: mocks.webhooks.length, count: mocks.webhooks.length, limit: 100, offset: 0 },
    reload: mocks.reloadWebhooks,
  }),
}));

vi.mock('../components/settings/AISettingsTab', () => ({
  AISettingsTab: () => <div>AI settings</div>,
}));

function configuredWebhook() {
  return {
    id: 'webhook-1',
    tenant_id: 'tenant-a',
    name: 'SOC forwarder',
    url: 'https://hooks.example.test/control-one',
    events: ['job.failed'],
    enabled: true,
    verify_ssl: true,
    timeout_seconds: 30,
    retry_count: 3,
    secret_configured: true,
    headers_configured: true,
    headers: {
      Authorization: '[redacted]',
      'X-Team': 'secops',
    },
    failure_count: 0,
    created_at: '2026-06-07T00:00:00Z',
    updated_at: '2026-06-07T00:00:00Z',
  };
}

describe('Settings webhooks', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.webhooks = [];
    mocks.createWebhook.mockResolvedValue(configuredWebhook());
    mocks.updateWebhook.mockResolvedValue(configuredWebhook());
    mocks.deleteWebhook.mockResolvedValue(undefined);
    mocks.testWebhook.mockResolvedValue({ success: true });
  });

  it('creates a signed webhook with custom headers', async () => {
    const user = userEvent.setup();
    render(<Settings />);

    await user.click(screen.getByRole('button', { name: /new webhook/i }));
    await user.type(screen.getByLabelText(/^name$/i), 'SOC forwarder');
    await user.type(screen.getByLabelText(/^url$/i), 'https://hooks.example.test/control-one');
    await user.type(screen.getByLabelText(/signing secret/i), 'hmac-secret');
    fireEvent.change(screen.getByLabelText(/custom headers json/i), {
      target: { value: '{"Authorization":"Bearer token","X-Team":"secops"}' },
    });
    await user.click(screen.getByLabelText('job.failed'));
    await user.click(screen.getByRole('button', { name: /create webhook/i }));

    await waitFor(() => {
      expect(mocks.createWebhook).toHaveBeenCalled();
    });
    expect(mocks.createWebhook).toHaveBeenCalledWith(expect.objectContaining({
      tenant_id: 'tenant-a',
      name: 'SOC forwarder',
      url: 'https://hooks.example.test/control-one',
      events: ['job.failed'],
      secret: 'hmac-secret',
      headers: {
        Authorization: 'Bearer token',
        'X-Team': 'secops',
      },
    }));
  });

  it('shows secure configuration badges and does not overwrite them on ordinary edit', async () => {
    const user = userEvent.setup();
    mocks.webhooks = [configuredWebhook()];

    const { container } = render(<Settings />);

    expect(screen.getByText('Signed')).toBeInTheDocument();
    expect(screen.getByText('Custom headers')).toBeInTheDocument();
    expect(container.textContent).not.toContain(String.fromCharCode(183));
    expect(container.textContent).not.toContain(String.fromCharCode(8212));
    expect(container.textContent).not.toContain(String.fromCharCode(8230));

    await user.click(screen.getByRole('button', { name: /edit/i }));
    await user.click(screen.getByRole('button', { name: /save changes/i }));

    await waitFor(() => {
      expect(mocks.updateWebhook).toHaveBeenCalled();
    });
    const [, payload] = mocks.updateWebhook.mock.calls[0];
    expect(payload).not.toHaveProperty('secret');
    expect(payload).not.toHaveProperty('headers');
  });
});
