import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { NetworkDeviceWizard } from './NetworkDeviceWizard';

const mocks = vi.hoisted(() => ({ create: vi.fn(), test: vi.fn(), save: vi.fn(), saved: vi.fn(), cancel: vi.fn() }));
vi.mock('../hooks/useApiClient', () => ({ useApiClient: () => ({ createNetworkCredential: mocks.create, testNetworkConnection: mocks.test, saveNetworkOnboarding: mocks.save }) }));
vi.mock('../providers/TenantProvider', () => ({ useTenant: () => ({ currentTenantId: 'tenant-a', tenants: [{ id: 'tenant-a', name: 'Lagos' }] }) }));
const result = {
  id: 'receipt-1', tenant_id: 'tenant-a', credential_id: 'secret-ref', protocol: 'snmpv3', state: 'authenticated',
  result: { state: 'authenticated', message: 'Read-only identity verified.', vendor: '', model: '', platform: '', suggested_type: 'network_appliance', confidence: 0, evidence: ['Unknown vendor; operator review required.'], capabilities: ['snmp_identity'], required_privileges: 'Read-only system MIB access' },
};
beforeEach(() => { vi.clearAllMocks(); mocks.create.mockResolvedValue({ id: 'secret-ref' }); mocks.test.mockResolvedValue(result); mocks.save.mockResolvedValue({ id: 'device-1' }); });
async function enterCredentials() {
  const user = userEvent.setup();
  await user.type(screen.getByLabelText('Device name'), 'Branch switch');
  await user.type(screen.getByLabelText('Management address'), '192.0.2.1');
  await user.type(screen.getByLabelText('Read-only username'), 'readonly');
  await user.type(screen.getByLabelText('Authentication secret'), 'auth-secret');
  await user.type(screen.getByLabelText('Privacy secret'), 'priv-secret');
  return user;
}
describe('Network device verification wizard', () => {
  it('tests only a secret reference and requires classification review before saving', async () => {
    render(<NetworkDeviceWizard onSaved={mocks.saved} onCancel={mocks.cancel} />);
    const snapshotOption = screen.getByRole('option', { name: 'NETCONF / RESTCONF / vendor API (snapshot sources; not for identity test)' });
    expect(snapshotOption).toBeDisabled();
    expect(screen.getByText(/Configure snapshot sources separately under Network telemetry sources/)).toBeInTheDocument();
    const user = await enterCredentials();
    await user.click(screen.getByRole('button', { name: 'Test connection' }));
    expect(await screen.findByText('Classification needs review')).toBeInTheDocument();
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ protocol: 'snmpv3', config: { username: 'readonly', auth_protocol: 'SHA256', auth_secret: 'auth-secret', priv_protocol: 'AES', priv_secret: 'priv-secret' } }));
    expect(mocks.test).toHaveBeenCalledWith({ tenant_id: 'tenant-a', credential_id: 'secret-ref', address: '192.0.2.1', port: 161 });
    expect(screen.getByRole('button', { name: 'Save verified device' })).toBeDisabled();
    await user.selectOptions(screen.getByLabelText('Device type'), 'switch');
    await user.click(screen.getByLabelText('Keep SNMP identity source selection'));
    await user.click(screen.getByLabelText('I reviewed the classification and connection result.'));
    await user.click(screen.getByRole('button', { name: 'Save verified device' }));
    await waitFor(() => expect(mocks.save).toHaveBeenCalledWith({ test_id: 'receipt-1', display_name: 'Branch switch', type: 'switch', site: '', group: '', telemetry_sources: ['snmp_identity'] }));
    expect(mocks.saved).toHaveBeenCalledWith({ id: 'device-1' });
  });
  it.each(['auth_failed', 'unreachable', 'policy_blocked'])('displays %s and never offers a verified save', async (state) => {
    mocks.test.mockResolvedValue({ ...result, state, result: { ...result.result, state, message: `Fixture ${state}` } });
    render(<NetworkDeviceWizard onSaved={mocks.saved} onCancel={mocks.cancel} />);
    const user = await enterCredentials(); await user.click(screen.getByRole('button', { name: 'Test connection' }));
    expect(await screen.findByText(`Fixture ${state}`)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Save verified device' })).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Authentication secret')).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Test connection' }));
    await waitFor(() => expect(mocks.test).toHaveBeenCalledTimes(2));
    expect(mocks.create).toHaveBeenCalledTimes(1); expect(mocks.save).not.toHaveBeenCalled();
  });
  it('refuses to test if encrypted credential storage fails', async () => {
    mocks.create.mockRejectedValue(new Error('Credential encryption is not configured'));
    render(<NetworkDeviceWizard onSaved={mocks.saved} onCancel={mocks.cancel} />);
    const user = await enterCredentials(); await user.click(screen.getByRole('button', { name: 'Test connection' }));
    expect(await screen.findByText('Credential encryption is not configured')).toBeInTheDocument();
    expect(mocks.test).not.toHaveBeenCalled(); expect(mocks.saved).not.toHaveBeenCalled();
  });
});
