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

    await user.click(screen.getByRole('tab', { name: /Windows/i }));
    expect(screen.getByText(/install\.ps1/)).toBeInTheDocument();

    await user.click(screen.getByRole('tab', { name: /Linux/i }));
    expect(screen.getByText(/install\.sh/)).toBeInTheDocument();
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
