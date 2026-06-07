import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { TrustCenter } from './TrustCenter';

describe('TrustCenter', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('loads public tenant data with an encoded tenant slug', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({
        tenant_slug: 'Tenant A',
        tenant_name: 'Tenant A',
        subprocessors: [],
        certifications: [],
        faq: [],
        incidents: [],
        last_updated: '2026-06-07T00:00:00Z',
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    vi.stubGlobal('fetch', fetchMock);

    render(
      <MemoryRouter initialEntries={['/trust/Tenant%20A']}>
        <Routes>
          <Route path="/trust/:tenantSlug" element={<TrustCenter />} />
        </Routes>
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith('/api/v1/trust/Tenant%20A');
    });
    expect(await screen.findByRole('heading', { name: 'Tenant A' })).toBeInTheDocument();
  });

  it('renders empty public trust data when collection fields are null', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({
        tenant_slug: 'default',
        tenant_name: 'default',
        subprocessors: null,
        certifications: null,
        faq: null,
        incidents: null,
        last_updated: '2026-06-07T00:00:00Z',
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    vi.stubGlobal('fetch', fetchMock);

    render(
      <MemoryRouter initialEntries={['/trust/default']}>
        <Routes>
          <Route path="/trust/:tenantSlug" element={<TrustCenter />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByRole('heading', { name: 'default' })).toBeInTheDocument();
    expect(screen.getByText('Active Certifications')).toBeInTheDocument();
    expect(screen.getAllByText('Subprocessors').length).toBeGreaterThan(0);
    expect(screen.getByText('Published Incidents')).toBeInTheDocument();
  });
});
