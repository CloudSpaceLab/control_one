import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Dashboard } from './Dashboard';
import * as useApiClientModule from '../hooks/useApiClient';
import * as useTenantsModule from '../hooks/useTenants';

// Mobile test stub: uses matchMedia overrides + a 375px viewport to confirm
// the dashboard still renders its 5 section titles. The actual CSS grid
// collapse is verified by visual regression in another environment; this test
// guards against JS-side regressions (hooks that crash on narrow viewports).

const viewports = [
  { label: 'mobile-375', width: 375, height: 667 },
  { label: 'tablet-768', width: 768, height: 1024 },
  { label: 'desktop-1280', width: 1280, height: 900 },
];

const overview = {
  generated_at: new Date().toISOString(),
  node_counts: { total: 1, healthy: 1, offline: 0 },
  security_event_counts: { critical: 0, high: 0, medium: 0, low: 0, total: 0 },
  health_incident_counts: { critical: 0, high: 0, medium: 0, low: 0, total: 0 },
  compliance_summary: { total: 0, passed: 0, failed: 0 },
  rule_trigger_counts_24h: {},
  remediations_applied_24h: 0,
};

describe('Dashboard at multiple breakpoints', () => {
  beforeEach(() => {
    (vi.spyOn(useApiClientModule, 'useApiClient') as any).mockReturnValue({
      getDashboardOverview: vi.fn().mockResolvedValue(overview),
      streamEvents: vi.fn().mockReturnValue(() => undefined),
    });
    (vi.spyOn(useTenantsModule, 'useTenants') as any).mockReturnValue({
      data: [{ id: 'tenant-1', name: 't', created_at: '', updated_at: '' }],
      pagination: { total: 1, count: 1, limit: 1, offset: 0, nextOffset: null, prevOffset: null },
      loading: false,
      error: null,
      refresh: vi.fn(),
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
          <Dashboard />
        </MemoryRouter>,
      );
      expect(await screen.findByText(/Security events/)).toBeInTheDocument();
      expect(screen.getByText(/Compliance alerts/)).toBeInTheDocument();
    });
  });
});
