import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { NetworkConfigurationPanel } from './NetworkConfigurationPanel';

const mocks = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock('../hooks/useApiClient', () => ({ useApiClient: () => ({ getNetworkConfiguration: mocks.get }) }));

function mount() {
  return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><NetworkConfigurationPanel targetId="switch-1" /></QueryClientProvider>);
}

describe('NetworkConfigurationPanel', () => {
  beforeEach(() => vi.clearAllMocks());

  it('shows sanitized revisions, deterministic changes, evidence-linked findings, and unsupported checks', async () => {
    mocks.get.mockResolvedValue({ state: 'ready', target_id: 'switch-1', snapshots: [{
      id: 'snapshot-2', source_type: 'ssh_config', adapter: 'ssh_config/cisco', adapter_version: 'management-read/v1',
      format: 'text', content: 'hostname edge\ntransport input telnet', content_hash: 'abc123', revision: 2,
      observed_at: '2026-10-05T10:00:00Z', added: ['transport input telnet'], removed: [],
      findings: [
        { id: 'telnet_management', title: 'Telnet management enabled', status: 'finding', severity: 'high', snapshot_id: 'snapshot-2', evidence_ref: 'network_configuration_snapshots:snapshot-2', evidence: ['line:2'] },
        { id: 'configuration_drift', title: 'Configuration drift from previous revision', status: 'finding', severity: 'medium', snapshot_id: 'snapshot-2', evidence_ref: 'network_configuration_snapshots:snapshot-2', related_evidence_ref: 'network_configuration_snapshots:snapshot-1' },
        { id: 'firmware_freshness', title: 'Firmware support age requires a vendor/version advisory source', status: 'unsupported' },
      ],
    }] });
    mount();
    await userEvent.click(await screen.findByText(/Revision 2 · ssh_config/));
    expect(screen.getByText('Telnet management enabled: finding')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'line:2' })).toHaveAttribute('href', '#snapshot-2-line:2');
    expect(screen.getAllByRole('link', { name: 'Evidence snapshot' }).map(link => link.getAttribute('href'))).toEqual(['#snapshot-2', '#snapshot-2']);
    expect(screen.getByRole('link', { name: 'Compared snapshot' })).toHaveAttribute('href', '#snapshot-1');
    expect(screen.getByText(/Firmware support age requires a vendor\/version advisory source: unsupported/)).toBeInTheDocument();
    expect(screen.getByText('Added: transport input telnet')).toBeInTheDocument();
    expect(screen.getByText(/No device commands or write actions are available/)).toBeInTheDocument();
  });

  it('states when no supported configuration has been collected', async () => {
    mocks.get.mockResolvedValue({ state: 'not_collected', target_id: 'switch-1', snapshots: [] });
    mount();
    expect(await screen.findByText(/No configuration snapshot has been reported/)).toBeInTheDocument();
    expect(screen.getByText('Write and remediation actions are unavailable.')).toBeInTheDocument();
  });

  it('does not claim posture evidence when the request fails', async () => {
    mocks.get.mockRejectedValue(new Error('offline'));
    mount();
    expect(await screen.findByRole('alert')).toHaveTextContent('Configuration evidence is unavailable.');
  });
});
