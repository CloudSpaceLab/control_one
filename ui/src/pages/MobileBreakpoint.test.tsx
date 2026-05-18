import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ControlRoom } from './ControlRoom';
import * as useApiClientModule from '../hooks/useApiClient';
import * as useTenantModule from '../providers/TenantProvider';
import type { ControlRoomOverview } from '../lib/api';

// Breakpoint smoke: jsdom cannot validate the visual grid, but this keeps the
// Control Room render path honest across the viewport sizes operators use.

const viewports = [
  { label: 'mobile-375', width: 375, height: 667 },
  { label: 'tablet-768', width: 768, height: 1024 },
  { label: 'desktop-1280', width: 1280, height: 900 },
];

const overview: ControlRoomOverview = {
  tenant_id: 'tenant-1',
  generated_at: new Date().toISOString(),
  period: '24h',
  lanes: [
    lane('server-health', 'Server Health', 'healthy', 'All monitored servers responding'),
    lane('security', 'Security', 'healthy', 'No critical findings'),
    lane('app-db-health', 'App/DB Health', 'healthy', 'Log capture healthy'),
    lane('exposure', 'Current Exposure', 'warning', '1 public listener'),
    lane('ip-behavior', 'Connection/IP Behavior', 'healthy', 'Traffic matches baseline'),
    lane('patch-posture', 'Patch Posture', 'warning', '1 patch approval pending'),
  ],
  top_incidents: [],
  stale_warnings: [],
  ip_behavior: {
    request_count: 0,
    bytes_out: 0,
    countries: [],
    findings: [],
  },
  webservers: {
    total: 0,
    capture_ready: 0,
    enforce_ready: 0,
    instances: [],
  },
  isolation: {
    online: 3,
    whitelist: 0,
    airgapped: 0,
    protected: 0,
    whitelist_gaps: 0,
    expired: 0,
    expiring_soon: 0,
    nodes: [],
  },
  firewall: {
    enabled: 0,
    disabled: 0,
    unknown: 0,
    default_deny: 0,
    stale: 0,
    nodes: [],
  },
  pending_actions: [],
};

function lane(id: string, title: string, tone: ControlRoomOverview['lanes'][number]['tone'], summary: string): ControlRoomOverview['lanes'][number] {
  return {
    id,
    title,
    tone,
    score: tone === 'healthy' ? 95 : 62,
    summary,
    primary_metric: { label: 'Status', value: summary, tone },
    secondary_metric: { label: 'Open', value: '0', tone },
    metrics: [],
    items: [],
    drilldown: `/control-room/${id}`,
    updated_at: new Date().toISOString(),
  };
}

describe('Control Room at multiple breakpoints', () => {
  beforeEach(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (vi.spyOn(useApiClientModule, 'useApiClient') as any).mockReturnValue({
      getControlRoomOverview: vi.fn().mockResolvedValue(overview),
      setNodeIsolation: vi.fn(),
    });
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (vi.spyOn(useTenantModule, 'useTenant') as any).mockReturnValue({
      currentTenantId: 'tenant-1',
      currentTenant: { id: 'tenant-1', name: 't', created_at: '', updated_at: '' },
      tenants: [{ id: 'tenant-1', name: 't', created_at: '', updated_at: '' }],
      setCurrentTenantId: vi.fn(),
      refresh: vi.fn(),
      loading: false,
      error: null,
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  viewports.forEach((vp) => {
    it(`renders at ${vp.label}`, async () => {
      // jsdom ignores window.resize, but we still set the sizes so any code
      // that reads them gets correct values.
      Object.defineProperty(window, 'innerWidth', { writable: true, configurable: true, value: vp.width });
      Object.defineProperty(window, 'innerHeight', { writable: true, configurable: true, value: vp.height });
      window.matchMedia = ((query: string) => ({
        matches: query.includes(`max-width: ${vp.width}px`),
        media: query,
        onchange: null,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })) as typeof window.matchMedia;

      render(
        <MemoryRouter>
          <ControlRoom />
        </MemoryRouter>,
      );
      expect(await screen.findByText(/Connection\/IP Behavior/)).toBeInTheDocument();
      expect(screen.getByText(/Patch Posture/)).toBeInTheDocument();
    });
  });
});
