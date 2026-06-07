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
});
