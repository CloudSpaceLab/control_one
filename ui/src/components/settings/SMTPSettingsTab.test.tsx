import { render, screen, waitFor, cleanup } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { SMTPSettingsTab } from './SMTPSettingsTab';

const mocks = vi.hoisted(() => ({ tenant: 'tenant-a', roles: ['admin'], client: { getSMTPSettings: vi.fn(), updateSMTPSettings: vi.fn() } }));
vi.mock('@/hooks/useApiClient', () => ({ useApiClient: () => mocks.client }));
vi.mock('@/providers/TenantProvider', () => ({ useTenant: () => ({ currentTenantId: mocks.tenant }) }));
vi.mock('@/providers/AuthProvider', () => ({ useAuth: () => ({ profile: { roles: mocks.roles } }) }));
const config = { host: 'smtp.example.com', port: 587, tls_mode: 'starttls', auth_enabled: true, username: 'user', sender_name: 'Alerts', sender_email: 'alerts@example.com', enabled: false, configured: true, password_configured: true, encryption_available: true };
beforeEach(() => { vi.clearAllMocks(); mocks.tenant = 'tenant-a'; mocks.roles = ['admin']; mocks.client.getSMTPSettings.mockResolvedValue(config); mocks.client.updateSMTPSettings.mockResolvedValue(config); });
afterEach(cleanup);

it('preserves a saved password without putting a placeholder in the payload', async () => {
  render(<SMTPSettingsTab />);
  const password = await screen.findByLabelText('Password');
  expect(password).toHaveValue('');
  await userEvent.click(screen.getByRole('button', { name: 'Save SMTP settings' }));
  await screen.findByText('SMTP settings saved. No email has been sent.');
  expect(mocks.client.updateSMTPSettings.mock.calls[0][0]).toBe('tenant-a');
  expect(mocks.client.updateSMTPSettings.mock.calls[0][1]).not.toHaveProperty('password');
});

it('clears an unsaved password and ignores old requests when switching tenants', async () => {
  const { rerender } = render(<SMTPSettingsTab />);
  await userEvent.type(await screen.findByLabelText('Password'), 'unsaved-secret');
  mocks.tenant = 'tenant-b'; rerender(<SMTPSettingsTab />);
  await waitFor(() => expect(screen.getByLabelText('Password')).toHaveValue(''));
  expect(mocks.client.getSMTPSettings).toHaveBeenLastCalledWith('tenant-b');
});

it('does not fetch SMTP settings for a viewer', () => {
  mocks.roles = ['viewer']; render(<SMTPSettingsTab />);
  expect(screen.getByText(/Administrator access is required/)).toBeInTheDocument();
  expect(mocks.client.getSMTPSettings).not.toHaveBeenCalled();
});

it('keeps failed loads out of the editable form', async () => {
  mocks.client.getSMTPSettings.mockRejectedValue(new Error('Unavailable'));
  render(<SMTPSettingsTab />);
  expect(await screen.findByRole('alert')).toHaveTextContent('Unavailable');
  expect(screen.queryByRole('button', { name: 'Save SMTP settings' })).not.toBeInTheDocument();
});
