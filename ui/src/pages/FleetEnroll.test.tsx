import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { FleetEnrollStatus, FleetEnrollResponse, NodeSummary } from '../lib/api';
import { FleetEnroll } from './FleetEnroll';

// The FleetEnroll page reaches into a handful of hooks. Mocking at the module
// boundary keeps the test fast and lets us drive the full submit->poll loop
// without spinning up providers.
const startFleetEnroll = vi.fn<(payload: unknown) => Promise<FleetEnrollResponse>>();
const getFleetEnrollStatus = vi.fn<(id: string) => Promise<FleetEnrollStatus>>();
const getNode = vi.fn<(id: string) => Promise<NodeSummary>>();
const showToast = vi.fn();

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => ({
    startFleetEnroll: (payload: unknown) => startFleetEnroll(payload),
    getFleetEnrollStatus: (id: string) => getFleetEnrollStatus(id),
    getNode: (id: string) => getNode(id),
  }),
}));

vi.mock('../providers/ToastProvider', () => ({
  useToast: () => ({ showToast }),
}));

vi.mock('../hooks/useTenants', () => ({
  useTenants: () => ({
    data: [{ id: 't-1', name: 'Tenant One', created_at: '2026-01-01T00:00:00Z' }],
    loading: false,
    error: null,
    pagination: { total: 1, count: 1, limit: 10, offset: 0, nextOffset: null, prevOffset: null },
    reload: () => {},
    refresh: () => {},
  }),
}));

describe('FleetEnroll', () => {
  beforeEach(() => {
    startFleetEnroll.mockReset();
    getFleetEnrollStatus.mockReset();
    getNode.mockReset();
    showToast.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders the form heading', () => {
    render(
      <MemoryRouter>
        <FleetEnroll />
      </MemoryRouter>,
    );
    expect(
      screen.getByRole('heading', { name: /bulk enrol hosts/i }),
    ).toBeInTheDocument();
  });

  it('rejects submit with no targets', async () => {
    render(
      <MemoryRouter>
        <FleetEnroll />
      </MemoryRouter>,
    );
    const submit = screen.getByRole('button', { name: /start fleet enrollment/i });

    await act(async () => {
      fireEvent.click(submit);
    });

    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(/add at least one host/i),
    );
    expect(startFleetEnroll).not.toHaveBeenCalled();
  });

  it('submits the fleet enroll request with parsed targets and starts polling', async () => {
    startFleetEnroll.mockResolvedValue({
      job_id: 'job-1',
      status: 'queued',
      message: 'ok',
    });
    // Resolve polling to a terminal state immediately so the post-submit
    // setInterval has nothing interesting to do during this test.
    getFleetEnrollStatus.mockResolvedValue({
      job_id: 'job-1',
      status: 'succeeded',
      results: [
        {
          id: 'r-1',
          host: '10.0.0.5',
          port: 22,
          success: true,
          node_id: 'node-uuid-1',
          duration_ms: 1234,
          created_at: '2026-04-20T00:00:00Z',
        },
      ],
    });
    getNode.mockResolvedValue({
      id: 'node-uuid-1',
      tenant_id: 't-1',
      hostname: '10.0.0.5',
      state: 'active',
      last_seen_at: '2026-04-20T00:00:05Z',
      first_scan_at: '2026-04-20T00:00:05Z',
      created_at: '2026-04-20T00:00:00Z',
      updated_at: '2026-04-20T00:00:05Z',
    });

    render(
      <MemoryRouter>
        <FleetEnroll />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByLabelText(/targets/i), {
      target: { value: '10.0.0.5\n' },
    });
    fireEvent.change(screen.getByLabelText(/ssh user/i), { target: { value: 'ubuntu' } });
    fireEvent.change(screen.getByLabelText(/ssh private key/i), {
      target: {
        value:
          '-----BEGIN OPENSSH PRIVATE KEY-----\nabc123\n-----END OPENSSH PRIVATE KEY-----',
      },
    });
    fireEvent.change(screen.getByLabelText(/enrollment token/i), {
      target: { value: 'cot_test' },
    });

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /start fleet enrollment/i }));
    });

    await waitFor(() => expect(startFleetEnroll).toHaveBeenCalledTimes(1));
    const payload = startFleetEnroll.mock.calls[0][0] as {
      targets: Array<{ host: string }>;
      token: string;
      ssh_user?: string;
      ssh_key?: string;
    };
    expect(payload.targets).toEqual([{ host: '10.0.0.5', user: undefined }]);
    expect(payload.token).toBe('cot_test');
    expect(payload.ssh_user).toBe('ubuntu');
    expect(payload.ssh_key).toBeTruthy();

    // The per-host table renders once the status poll returns results.
    await waitFor(() =>
      expect(
        screen.getByRole('table', { name: /per-host enrollment progress/i }),
      ).toBeInTheDocument(),
    );
    // The host appears in both the textarea (value) and the table row;
    // asserting the table link-text confirms the polling path landed.
    await waitFor(() => {
      const table = screen.getByRole('table', { name: /per-host enrollment progress/i });
      expect(table).toHaveTextContent('10.0.0.5');
    });
  });

  it('parses user@host:port target syntax and strips comments', async () => {
    startFleetEnroll.mockResolvedValue({ job_id: 'job-2', status: 'queued', message: 'ok' });
    getFleetEnrollStatus.mockResolvedValue({ job_id: 'job-2', status: 'running', results: [] });

    render(
      <MemoryRouter>
        <FleetEnroll />
      </MemoryRouter>,
    );
    fireEvent.change(screen.getByLabelText(/targets/i), {
      target: { value: 'admin@10.0.0.9:2222\n# comment\n   \nroot@10.0.0.10' },
    });
    fireEvent.change(screen.getByLabelText(/ssh private key/i), {
      target: { value: '-----BEGIN OPENSSH PRIVATE KEY-----\nxyz\n-----END OPENSSH PRIVATE KEY-----' },
    });
    fireEvent.change(screen.getByLabelText(/enrollment token/i), {
      target: { value: 'cot_test2' },
    });

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /start fleet enrollment/i }));
    });

    await waitFor(() => expect(startFleetEnroll).toHaveBeenCalledTimes(1));
    const payload = startFleetEnroll.mock.calls[0][0] as {
      targets: Array<{ host: string; port?: number; user?: string }>;
    };
    expect(payload.targets).toEqual([
      { host: '10.0.0.9', port: 2222, user: 'admin' },
      { host: '10.0.0.10', user: 'root' },
    ]);
  });
});
