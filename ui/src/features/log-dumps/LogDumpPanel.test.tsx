import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { LogDumpPanel } from './LogDumpPanel';

const createLogDump = vi.fn();
const getLogDumpPreview = vi.fn();
const downloadLogDump = vi.fn();
const reload = vi.fn();

const capturedDump = {
  id: 'dump-1',
  tenant_id: 'tenant-1',
  node_id: 'node-1',
  source: 'control_plane' as const,
  entity_filter: { ip: '203.0.113.7' },
  window_start: '2026-10-01T08:00:00Z',
  window_end: '2026-10-01T09:00:00Z',
  retention_days: 7 as const,
  status: 'captured' as const,
  row_count: 0,
  size_bytes: 0,
  truncated: true,
  source_available: true,
  created_at: '2026-10-01T09:00:00Z',
  captured_at: '2026-10-01T09:00:01Z',
  expires_at: '2026-10-08T09:00:00Z',
  expired: false,
  download_url: '/api/v1/log-dumps/dump-1/download',
  preview_url: '/api/v1/log-dumps/dump-1/preview',
};

vi.mock('@/hooks/useApiClient', () => ({
  useApiClient: () => ({
    createLogDump,
    getLogDumpPreview,
    downloadLogDump,
  }),
}));

vi.mock('@/hooks/useNodes', () => ({
  useNodes: () => ({
    data: [
      {
        id: 'node-1',
        tenant_id: 'tenant-1',
        hostname: 'db-01',
        state: 'online',
        created_at: '2026-10-01T00:00:00Z',
        updated_at: '2026-10-01T00:00:00Z',
      },
    ],
    pagination: {
      total: 1,
      count: 1,
      limit: 200,
      offset: 0,
      nextOffset: null,
      prevOffset: null,
    },
    loading: false,
    error: null,
    reload: vi.fn(),
  }),
}));

vi.mock('@/hooks/useRolePick', () => ({
  useRolePick: () => ({
    role: 'operator',
    isAdmin: false,
    isOperator: true,
    isViewer: false,
    hasRole: vi.fn(),
    roles: ['operator'],
  }),
}));

vi.mock('./useLogDumps', () => ({
  useLogDumps: () => ({
    data: [capturedDump],
    loading: false,
    error: null,
    reload,
  }),
}));

describe('LogDumpPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    createLogDump.mockResolvedValue(capturedDump);
    getLogDumpPreview.mockResolvedValue({ lines: ['{"message":"ok"}'], truncated: false });
  });

  it('submits the selected source/window with 7-day retention and entity scope', async () => {
    const user = userEvent.setup();
    render(
      <LogDumpPanel
        tenantId="tenant-1"
        fixedNodeId="node-1"
        entityFilter={{ ip: '203.0.113.7' }}
      />,
    );

    await user.selectOptions(screen.getByLabelText('Source'), 'node_agent');
    await user.selectOptions(screen.getByLabelText('Window'), '15');
    await user.click(screen.getByRole('button', { name: /request/i }));

    await waitFor(() => expect(createLogDump).toHaveBeenCalledTimes(1));
    const payload = createLogDump.mock.calls[0][0];
    expect(payload).toMatchObject({
      tenant_id: 'tenant-1',
      node_id: 'node-1',
      source: 'node_agent',
      retention_days: 7,
      entity_filter: { ip: '203.0.113.7' },
    });
    const start = new Date(payload.window_start).getTime();
    const end = new Date(payload.window_end).getTime();
    expect(end - start).toBe(15 * 60_000);
    expect(reload).toHaveBeenCalled();
  });

  it('shows empty-source and truncation state and loads preview through the API client', async () => {
    render(
      <LogDumpPanel
        tenantId="tenant-1"
        fixedNodeId="node-1"
        entityFilter={{ ip: '203.0.113.7' }}
      />,
    );

    expect(screen.getByText('Available · no rows')).toBeInTheDocument();
    expect(screen.getByText('truncated')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /preview/i }));
    await waitFor(() =>
      expect(getLogDumpPreview).toHaveBeenCalledWith('tenant-1', 'dump-1'),
    );
    expect(await screen.findByText('{"message":"ok"}')).toBeInTheDocument();
  });
});
