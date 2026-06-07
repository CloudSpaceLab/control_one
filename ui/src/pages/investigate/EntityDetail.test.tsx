import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import {
  investigationTimelineItemToLifecycle,
  IPBehaviorRecommendationPanel,
  IPBehaviorSummaryPanel,
  mergeLifecycleItems,
} from './EntityDetail';
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

describe('investigation timeline lifecycle helpers', () => {
  it('maps small-analytics connection timeline rows into lifecycle evidence', () => {
    const item = investigationTimelineItemToLifecycle({
      source_table: 'process_connections',
      source_record_id: 'process_connections:conn-1:open',
      tenant_id: 'tenant-1',
      ts: '2026-06-07T09:00:00Z',
      event_type: 'conn.open',
      severity: 'info',
      message: 'nginx opened 10.0.0.5:52100 -> 203.0.113.10:443',
      conn_id: 'conn-1',
      node_id: 'node-1',
      process_name: 'nginx',
      src_ip: '10.0.0.5',
      dst_ip: '203.0.113.10',
      dst_port: 443,
    });

    expect(item.source).toBe('event');
    expect(item.raw_id).toBe('process_connections:conn-1:open');
    expect(item.actor).toBe('nginx');
    expect(item.target).toBe('10.0.0.5 -> 203.0.113.10:443');
    expect(item.metadata).toMatchObject({
      source_table: 'process_connections',
      event_type: 'conn.open',
      conn_id: 'conn-1',
    });
  });

  it('merges supplemental timeline rows newest-first without duplicate raw evidence', () => {
    const legacy = {
      ts: '2026-06-07T08:00:00Z',
      source: 'audit',
      summary: 'User tagged the IP',
      raw_id: 'audit:1',
    };
    const supplemental = {
      ts: '2026-06-07T09:00:00Z',
      source: 'event',
      summary: 'Connection opened',
      raw_id: 'process_connections:conn-1:open',
    };

    const merged = mergeLifecycleItems([legacy, supplemental], [supplemental]);

    expect(merged).toHaveLength(2);
    expect(merged.map((item) => item.raw_id)).toEqual([
      'process_connections:conn-1:open',
      'audit:1',
    ]);
  });
});
