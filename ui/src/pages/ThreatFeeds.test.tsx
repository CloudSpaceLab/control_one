import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ThreatFeeds } from './ThreatFeeds';
import type { ThreatFeed, ThreatIntelSummary } from '../lib/api';

const mocks = vi.hoisted(() => {
  const listThreatFeeds = vi.fn();
  const getThreatIntelSummary = vi.fn();
  const createThreatFeed = vi.fn();
  const updateThreatFeed = vi.fn();
  const deleteThreatFeed = vi.fn();
  return {
    apiClient: {
      listThreatFeeds,
      getThreatIntelSummary,
      createThreatFeed,
      updateThreatFeed,
      deleteThreatFeed,
    },
    listThreatFeeds,
    getThreatIntelSummary,
    createThreatFeed,
    updateThreatFeed,
    deleteThreatFeed,
  };
});

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

vi.mock('../hooks/useTenants', () => ({
  useTenants: () => ({
    data: [{ id: 'tenant-1', name: 'Bank Tenant', created_at: '2026-06-01T00:00:00Z' }],
    loading: false,
    error: null,
    reload: vi.fn(),
  }),
}));

const feedRow: ThreatFeed = {
  id: 'feed-1',
  tenant_id: 'tenant-1',
  name: 'FireHOL production',
  feed_type: 'firehol_l1',
  url: 'https://iplists.firehol.org/files/firehol_level1.netset',
  has_api_key: false,
  score_floor: 80,
  refresh_seconds: 3600,
  enabled: true,
  last_status: 'ok',
  last_indicator_count: 1200,
  last_refreshed_at: '2026-06-08T00:00:00Z',
  created_at: '2026-06-07T00:00:00Z',
  updated_at: '2026-06-08T00:00:00Z',
};

const summary: ThreatIntelSummary = {
  available: true,
  generated_at: '2026-06-08T00:00:00Z',
  total_indicators: 1200,
  global_indicators: 1000,
  tenant_indicators: 200,
  sources: [
    {
      feed: 'FireHOL production',
      scope: 'tenant',
      tenant_id: 'tenant-1',
      indicators: 1200,
      max_score: 80,
      sample: [{ cidr: '203.0.113.0/24', score: 80 }],
    },
  ],
};

describe('ThreatFeeds', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listThreatFeeds.mockResolvedValue({ data: [feedRow] });
    mocks.getThreatIntelSummary.mockResolvedValue(summary);
    mocks.createThreatFeed.mockResolvedValue(feedRow);
    mocks.updateThreatFeed.mockResolvedValue({ ...feedRow, enabled: false });
    mocks.deleteThreatFeed.mockResolvedValue(undefined);
  });

  it('does not show a false empty state when feeds fail to load', async () => {
    mocks.listThreatFeeds.mockRejectedValueOnce(new Error('threat feed store unavailable'));

    render(<ThreatFeeds />);

    expect(await screen.findByRole('alert')).toHaveTextContent('threat feed store unavailable');
    expect(screen.getByText('Threat feeds could not be loaded')).toBeInTheDocument();
    expect(screen.queryByText('No threat feeds configured')).not.toBeInTheDocument();
  });

  it('surfaces blacklist summary failures without showing warming copy', async () => {
    mocks.getThreatIntelSummary.mockRejectedValueOnce(new Error('snapshot unavailable'));

    render(<ThreatFeeds />);

    expect(await screen.findByRole('alert')).toHaveTextContent('snapshot unavailable');
    expect(screen.getByText(/blacklist cache could not be loaded/i)).toBeInTheDocument();
    expect(screen.queryByText(/still warming up/i)).not.toBeInTheDocument();
  });

  it('keeps failed enable toggles visible and names the row action', async () => {
    const user = userEvent.setup();
    mocks.updateThreatFeed.mockRejectedValueOnce(new Error('toggle denied'));

    render(<ThreatFeeds />);

    const toggle = await screen.findByRole('button', { name: /disable threat feed firehol production/i });
    await user.click(toggle);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Threat feed update failed for FireHOL production: toggle denied',
    );
    expect(mocks.updateThreatFeed).toHaveBeenCalledWith('feed-1', { enabled: false });
  });

  it('keeps a failed delete visible in the confirmation modal', async () => {
    const user = userEvent.setup();
    mocks.deleteThreatFeed.mockRejectedValueOnce(new Error('delete denied'));

    render(<ThreatFeeds />);

    await user.click(await screen.findByRole('button', { name: /remove threat feed firehol production/i }));
    expect(screen.getByRole('dialog', { name: /remove threat source/i })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: /^remove$/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Threat feed delete failed: delete denied');
    expect(screen.getByRole('dialog', { name: /remove threat source/i })).toBeInTheDocument();
  });

  it('blocks invalid refresh intervals before creating a feed', async () => {
    const user = userEvent.setup();

    render(<ThreatFeeds />);

    await user.type(screen.getByLabelText(/name/i), 'Spamhaus demo');
    const refresh = screen.getByLabelText(/refresh/i);
    await user.clear(refresh);
    await user.type(refresh, '30');

    expect(screen.getByRole('alert')).toHaveTextContent('Refresh interval must be at least 60 seconds.');
    expect(screen.getByRole('button', { name: /add source/i })).toBeDisabled();
    expect(mocks.createThreatFeed).not.toHaveBeenCalled();
  });
});
