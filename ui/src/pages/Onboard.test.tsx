import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
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
  it('uses universal agent-managed machine language', () => {
    renderOnboard();

    expect(screen.getByText('Add machines to Control One')).toBeInTheDocument();
    expect(screen.getByText('Agent identity survives hostname and IP changes.')).toBeInTheDocument();
    expect(screen.getByText('Offline or restricted network')).toBeInTheDocument();
    expect(screen.queryByText(/Add servers to Control One/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Choose a scenario/i)).not.toBeInTheDocument();
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
});
