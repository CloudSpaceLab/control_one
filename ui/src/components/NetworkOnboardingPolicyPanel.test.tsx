import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { NetworkOnboardingPolicyPanel } from './NetworkOnboardingPolicyPanel';

const mocks = vi.hoisted(() => ({
  get: vi.fn(), update: vi.fn(), roles: ['admin'] as string[],
}));
vi.mock('../hooks/useApiClient', () => ({ useApiClient: () => ({ getNetworkOnboardingPolicy: mocks.get, updateNetworkOnboardingPolicy: mocks.update }) }));
vi.mock('../providers/AuthProvider', () => ({ useAuth: () => ({ profile: { roles: mocks.roles } }) }));

function mount() {
  return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><NetworkOnboardingPolicyPanel /></QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks(); mocks.roles = ['admin'];
  mocks.get.mockResolvedValue({ allowed_cidrs: [], source: 'server_config' });
  mocks.update.mockImplementation(async ({ allowed_cidrs }: { allowed_cidrs: string[] }) => ({ allowed_cidrs, source: 'database' }));
});

describe('Network onboarding IP allowlist', () => {
  it('allows an administrator to add a host or CIDR entry', async () => {
    const user = userEvent.setup(); mount();
    expect(await screen.findByText('No device destinations allowed')).toBeInTheDocument();
    await user.type(screen.getByLabelText('Device IP or CIDR block'), '192.168.56.10');
    await user.click(screen.getByRole('button', { name: 'Add allowed IP / CIDR' }));
    await waitFor(() => expect(mocks.update).toHaveBeenCalledWith({ allowed_cidrs: ['192.168.56.10'] }));
    expect(await screen.findByText('New device connection tests will use this allowlist immediately.')).toBeInTheDocument();
  });

  it('allows removing an existing subnet and hides the panel from non-admins', async () => {
    mocks.get.mockResolvedValue({ allowed_cidrs: ['192.168.56.0/24'], source: 'database' });
    const user = userEvent.setup(); const rendered = mount();
    await user.click(await screen.findByRole('button', { name: 'Remove 192.168.56.0/24' }));
    await waitFor(() => expect(mocks.update).toHaveBeenCalledWith({ allowed_cidrs: [] }));
    rendered.unmount();
    mocks.roles = ['operator'];
    const operatorPanel = mount();
    expect(screen.queryByText('Allowed device networks')).not.toBeInTheDocument();
    operatorPanel.unmount();
  });
});
