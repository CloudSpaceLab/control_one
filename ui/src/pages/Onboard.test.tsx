import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Onboard } from './Onboard';

const mocks = vi.hoisted(() => ({
  enrichIp: vi.fn(),
  testServerConnection: vi.fn(),
  createEnrollmentToken: vi.fn(),
  startFleetEnroll: vi.fn(),
  createTenant: vi.fn(),
  refreshTenants: vi.fn(),
}));

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => ({
    enrichIp: mocks.enrichIp,
    testServerConnection: mocks.testServerConnection,
    createEnrollmentToken: mocks.createEnrollmentToken,
    startFleetEnroll: mocks.startFleetEnroll,
    createTenant: mocks.createTenant,
  }),
}));

vi.mock('../providers/TenantProvider', () => ({
  useTenant: () => ({
    currentTenantId: 'tenant-1',
    tenants: [{ id: 'tenant-1', name: 'Production' }],
    refresh: mocks.refreshTenants,
  }),
}));

vi.mock('../components/settings/OnboardAIPanel', () => ({
  OnboardAIPanel: () => null,
}));

function renderOnboard() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter future={{ v7_relativeSplatPath: true, v7_startTransition: true }}>
        <Onboard />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('Onboard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.enrichIp.mockResolvedValue(null);
  });

  it('uses universal agent-managed machine language', () => {
    renderOnboard();

    expect(screen.getByText('Add machines to Control One')).toBeInTheDocument();
    expect(screen.getByText('Agent identity survives hostname and IP changes.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Command install/i })).toBeInTheDocument();
    expect(screen.getByText('Offline or restricted network')).toBeInTheDocument();
    expect(screen.queryByText(/Add servers to Control One/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Choose a scenario/i)).not.toBeInTheDocument();
  });

  it('lets operators choose the target OS for command install', async () => {
    const user = userEvent.setup();
    renderOnboard();

    await user.click(screen.getByRole('button', { name: /Command install/i }));
    expect(screen.getByText(/Static IP and inbound access are not required/i)).toBeInTheDocument();

    await user.click(screen.getByRole('tab', { name: /Linux/i }));
    expect(screen.getByText(/platform=linux/)).toBeInTheDocument();

    await user.click(screen.getByRole('tab', { name: /macOS/i }));
    expect(screen.getByText(/platform=darwin/)).toBeInTheDocument();

    await user.click(screen.getByRole('tab', { name: /Windows/i }));
    expect(screen.getByText(/platform=windows/)).toBeInTheDocument();
  });

  it('generates a tokenized command for machines without inbound access', async () => {
    const user = userEvent.setup();
    const writeTextMock = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: writeTextMock },
    });
    mocks.createEnrollmentToken.mockResolvedValueOnce({
      id: 'token-1',
      tenant_id: 'tenant-1',
      name: 'command-install-linux',
      token: 'cot_dd38fd89714ab5948dd92afd2e51e501',
      max_nodes: 1,
      nodes_enrolled: 0,
      labels: { onboard_source: 'command-install', platform: 'linux' },
      capabilities: ['agent.run'],
      created_at: '2026-09-10T00:00:00Z',
      expires_at: '2026-09-11T00:00:00Z',
    });
    renderOnboard();

    await user.click(screen.getByRole('button', { name: /Command install/i }));
    await user.click(screen.getByRole('tab', { name: /Linux/i }));
    await user.click(screen.getByRole('button', { name: /Generate token/i }));

    expect(mocks.createEnrollmentToken).toHaveBeenCalledWith({
      name: expect.stringMatching(/^command-install-linux-/),
      tenant_id: 'tenant-1',
      max_nodes: 1,
      ttl: '24h',
      labels: { onboard_source: 'command-install', platform: 'linux' },
      capabilities: ['agent.run'],
    });
    expect(
      await screen.findByText(
        /\/api\/v1\/agent\/install-script\?token=cot_dd38fd89714ab5948dd92afd2e51e501&platform=linux/,
      ),
    ).toBeInTheDocument();
    expect(screen.getByText(/\| sudo bash/)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Copy' }));
    expect(writeTextMock).toHaveBeenCalledWith(
      "curl -fsSL 'http://localhost:3000/api/v1/agent/install-script?token=cot_dd38fd89714ab5948dd92afd2e51e501&platform=linux' | sudo bash",
    );
  });

  it('copies a cmd.exe-compatible Windows installer command', async () => {
    const user = userEvent.setup();
    const writeTextMock = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: writeTextMock },
    });
    mocks.createEnrollmentToken.mockResolvedValueOnce({
      id: 'token-2',
      tenant_id: 'tenant-1',
      name: 'command-install-windows',
      token: 'cot_windows_token',
      max_nodes: 1,
      nodes_enrolled: 0,
      labels: { onboard_source: 'command-install', platform: 'windows' },
      capabilities: ['agent.run'],
      created_at: '2026-09-10T00:00:00Z',
      expires_at: '2026-09-11T00:00:00Z',
    });
    renderOnboard();

    await user.click(screen.getByRole('button', { name: /Command install/i }));
    await user.click(screen.getByRole('tab', { name: /Windows/i }));
    await user.click(screen.getByRole('button', { name: /Generate token/i }));

    expect(await screen.findByText(/powershell\.exe -NoProfile -ExecutionPolicy Bypass -Command/)).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Copy' }));
    expect(writeTextMock).toHaveBeenCalledWith(
      "powershell.exe -NoProfile -ExecutionPolicy Bypass -Command \"Invoke-RestMethod -Uri 'http://localhost:3000/api/v1/agent/install-script?token=cot_windows_token&platform=windows' | Invoke-Expression\"",
    );
  });

  it('keeps remote install hints target-neutral', async () => {
    const user = userEvent.setup();
    renderOnboard();

    await user.click(screen.getByRole('button', { name: /Install on another machine/i }));
    expect(screen.getByText('Linux or macOS with SSH enabled. Default port 22.')).toBeInTheDocument();

    await user.click(screen.getByRole('tab', { name: /WinRM/i }));
    expect(screen.getByText('Windows machine with WinRM enabled. Default port 5985 (HTTP) / 5986 (HTTPS).')).toBeInTheDocument();
    expect(screen.queryByText(/Windows Server with WinRM enabled/i)).not.toBeInTheDocument();
  });

  it('offers non-static-IP fallbacks when push install cannot reach the address', async () => {
    const user = userEvent.setup();
    mocks.testServerConnection.mockResolvedValueOnce({ ok: false, error: 'No route to target' });
    renderOnboard();

    await user.click(screen.getByRole('button', { name: /Install on another machine/i }));
    await user.type(screen.getByLabelText(/Address/i), 'dynamic-laptop.example.com');
    await user.type(screen.getByLabelText(/Username/i), 'ubuntu');
    await user.type(screen.getByLabelText(/Password/i, { selector: 'input' }), 'secret');
    await user.click(screen.getByRole('button', { name: /Test connection/i }));

    expect(await screen.findByText('No route to target')).toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: /Command install/i }).length).toBeGreaterThan(0);
    expect(screen.getByRole('link', { name: /Offline bundle/i })).toBeInTheDocument();
  });
});
