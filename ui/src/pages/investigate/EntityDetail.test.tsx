import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import { IPBehaviorRecommendationPanel, IPBehaviorSummaryPanel } from './EntityDetail';
import type { BehavioralAnomaly, IPBehaviorIPProfile, IpEnrichment } from '@/lib/api';

describe('IPBehaviorRecommendationPanel', () => {
  it('keeps Smart Response focused on governance instead of duplicating IP action controls', () => {
    const profile: IPBehaviorIPProfile = {
      source_ip: '45.135.193.156',
      countries: ['DE'],
      asns: ['AS51396'],
      apps: ['nginx'],
      server_groups: ['edge'],
      node_ids: ['node-1', 'node-2'],
      request_count: 183,
      bytes_out: 2_400_000,
      status_counts: { '401': 2, '403': 1, '500': 120 },
      first_seen_at: '2026-05-18T17:24:20Z',
      last_seen_at: '2026-05-18T18:33:47Z',
    };
    const finding: BehavioralAnomaly = {
      id: 'finding-1',
      tenant_id: 'tenant-1',
      baseline_id: 'baseline-1',
      source_ip: '45.135.193.156',
      country_code: 'DE',
      asn: 'AS51396',
      metric: 'exploit_attempt',
      severity: 'critical',
      reason: 'request burst: 61 requests in 1m; server error spike: 60 500/502/503 responses; sensitive/admin path probing',
      observed_value: 100,
      z_score: 7,
      resolved: false,
      created_at: '2026-05-18T18:00:00Z',
    };
    const enrichment: IpEnrichment = {
      addr: '45.135.193.156',
      threat_feeds: [{ feed: 'spamhaus-drop', severity: 'critical' }],
    };

    render(
      <MemoryRouter>
        <IPBehaviorRecommendationPanel
          ip="45.135.193.156"
          profile={profile}
          findings={[finding]}
          enrichment={enrichment}
        />
      </MemoryRouter>,
    );

    expect(screen.getByText('Recommended response for this IP')).toBeInTheDocument();
    expect(screen.getByText('Response governance')).toBeInTheDocument();
    expect(screen.getByText('Approval gate')).toBeInTheDocument();
    expect(screen.getByText('Receipt required')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /block \/ allow ip/i })).not.toBeInTheDocument();
  });

  it('renders sentinel observation timestamps as missing evidence', () => {
    const profile: IPBehaviorIPProfile = {
      source_ip: '8.8.8.8',
      countries: [],
      asns: [],
      apps: [],
      server_groups: [],
      node_ids: [],
      request_count: 0,
      bytes_out: 0,
      status_counts: {},
      first_seen_at: '0001-01-01T00:13:35Z',
      last_seen_at: '0001-01-01T00:13:35Z',
    };

    render(
      <IPBehaviorSummaryPanel
        ip="8.8.8.8"
        profile={profile}
        findings={[]}
        loading={false}
        error={null}
      />,
    );

    expect(screen.getByText('No observations')).toBeInTheDocument();
    expect(screen.queryByText(/1\/1\/1/)).not.toBeInTheDocument();
  });
});
