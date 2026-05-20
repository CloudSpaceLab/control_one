import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Coverage } from './Coverage';
import * as useCoverageMatrixModule from '@/hooks/useCoverageMatrix';
import * as useTenantModule from '@/providers/TenantProvider';
import type { CoverageMatrixResponse } from '@/lib/api';

const reload = vi.fn();

const matrix: CoverageMatrixResponse = {
  catalog_version: 'coverage.truth.v1',
  scope: 'tenant',
  tenant_id: 'tenant-1',
  generated_at: '2026-05-20T08:00:00Z',
  domains: [
    { domain: 'telemetry', title: 'Telemetry', description: 'Agent collection' },
    { domain: 'parser', title: 'Parser', description: 'Typed normalization' },
    { domain: 'db_audit', title: 'DB audit', description: 'Database audit discovery' },
  ],
  rows: [
    {
      domain: 'telemetry',
      title: 'Tenant heartbeat freshness',
      state: 'supported',
      quality: ['production_tested'],
      evidence: ['nodes.last_seen_at'],
    },
    {
      domain: 'parser',
      title: 'Typed parser coverage',
      state: 'raw_only',
      quality: ['fixture_tested'],
      gaps: ['split parser coverage by source and parser version'],
    },
    {
      domain: 'db_audit',
      title: 'Postgres query audit',
      state: 'unsupported',
      gaps: ['grant least-privilege audit access before claiming coverage'],
    },
    {
      domain: 'compliance',
      title: 'Oracle hardening evidence',
      state: 'not_applicable',
      gaps: ['Oracle is not deployed for this tenant'],
    },
  ],
};

describe('Coverage', () => {
  beforeEach(() => {
    reload.mockReset();
    vi.spyOn(useTenantModule, 'useTenant').mockReturnValue({
      tenants: [{ id: 'tenant-1', name: 'Bank Operations', created_at: '2026-05-20T00:00:00Z' }],
      currentTenant: { id: 'tenant-1', name: 'Bank Operations', created_at: '2026-05-20T00:00:00Z' },
      currentTenantId: 'tenant-1',
      loading: false,
      error: null,
      setCurrentTenantId: vi.fn(),
      refresh: vi.fn(),
    } as ReturnType<typeof useTenantModule.useTenant>);
    vi.spyOn(useCoverageMatrixModule, 'useCoverageMatrix').mockReturnValue({
      data: matrix,
      loading: false,
      error: null,
      unavailable: false,
      reload,
    });
  });

  it('renders a first-class truth surface without marking unsupported rows as passing', () => {
    render(
      <MemoryRouter>
        <Coverage />
      </MemoryRouter>,
    );

    expect(screen.getByRole('heading', { name: 'Coverage' })).toBeInTheDocument();
    expect(screen.getByText('coverage.truth.v1')).toBeInTheDocument();
    expect(screen.getByText(/Unsupported and not-applicable rows are never counted as passing/i)).toBeInTheDocument();

    const scopePanel = screen.getByRole('heading', { name: /Tenant overlay/i }).closest('section');
    expect(scopePanel).not.toBeNull();
    expect(within(scopePanel as HTMLElement).getByLabelText('Passing: 1')).toBeInTheDocument();
    expect(within(scopePanel as HTMLElement).getByLabelText('Unsupported: 1')).toBeInTheDocument();
    expect(screen.getByText('2 attention states')).toBeInTheDocument();
  });

  it('filters by DB audit and keeps the next action visible', async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <Coverage />
      </MemoryRouter>,
    );

    await user.click(screen.getByRole('button', { name: /DB audit/i }));

    expect(screen.getAllByText('Postgres query audit').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Unsupported').length).toBeGreaterThan(0);
    expect(screen.getByText(/least-privilege grants/i)).toBeInTheDocument();
    expect(screen.queryByText('Tenant heartbeat freshness')).not.toBeInTheDocument();
  });

  it('refreshes the coverage matrix on demand', async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <Coverage />
      </MemoryRouter>,
    );

    await user.click(screen.getByRole('button', { name: /refresh/i }));

    expect(reload).toHaveBeenCalledTimes(1);
  });
});
