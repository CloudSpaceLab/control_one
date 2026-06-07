import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { SearchResults } from './SearchResults';

const mocks = vi.hoisted(() => ({
  investigateSearch: vi.fn(),
}));

vi.mock('@/hooks/useApiClient', () => ({
  useApiClient: () => ({
    investigateSearch: mocks.investigateSearch,
  }),
}));

vi.mock('@/providers/TenantProvider', () => ({
  useTenant: () => ({
    currentTenantId: 'tenant-1',
  }),
}));

function renderSearch(initialEntry: string) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <SearchResults />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  mocks.investigateSearch.mockReset();
});

describe('SearchResults', () => {
  it('renders an intentional empty-query state instead of a broken query heading', () => {
    renderSearch('/search');

    expect(screen.getByRole('heading', { level: 1, name: 'Search' })).toBeInTheDocument();
    expect(screen.getByText('Search events, alerts, audit entries, and tags across the selected tenant.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /save search/i })).toBeDisabled();
    expect(screen.queryByText(/empty query/i)).not.toBeInTheDocument();
    expect(mocks.investigateSearch).not.toHaveBeenCalled();
  });

  it('keeps query context in the description and can clear back to the empty state', async () => {
    mocks.investigateSearch.mockResolvedValue({ items: [], facets: [] });
    const user = userEvent.setup();
    renderSearch('/search?q=nginx');

    expect(screen.getByRole('heading', { level: 1, name: 'Search results' })).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByText('0 matches for "nginx"')).toBeInTheDocument();
    });
    expect(mocks.investigateSearch).toHaveBeenCalledWith({
      tenantId: 'tenant-1',
      q: 'nginx',
      limit: 200,
    });

    const input = screen.getByPlaceholderText('Refine search...');
    await user.clear(input);
    await user.click(screen.getByRole('button', { name: 'Search' }));

    expect(screen.getByRole('heading', { level: 1, name: 'Search' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /save search/i })).toBeDisabled();
  });
});
