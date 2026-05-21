import { describe, expect, it, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { Sidebar } from './Sidebar';
import { GlobalSearch } from './GlobalSearch';
import { ProfileMenu } from './ProfileMenu';
import { TopBar } from './TopBar';

class ResizeObserverMock {
  observe() {}
  unobserve() {}
  disconnect() {}
}

vi.stubGlobal('ResizeObserver', ResizeObserverMock);
window.HTMLElement.prototype.scrollIntoView = vi.fn();

vi.mock('@/providers/TenantProvider', () => ({
  useTenant: () => ({
    tenants: [{ id: 'tenant-1', name: 'Bank Operations' }],
    currentTenant: { id: 'tenant-1', name: 'Bank Operations' },
    currentTenantId: 'tenant-1',
    setCurrentTenantId: () => undefined,
    loading: false,
  }),
}));

vi.mock('@/providers/AuthProvider', () => ({
  useAuth: () => ({
    profile: { name: 'Ada CISO', email: 'ada@example.com' },
    signOut: vi.fn(),
  }),
}));

vi.mock('@/providers/ThemeProvider', () => ({
  useTheme: () => ({
    theme: 'dark',
    toggleTheme: vi.fn(),
  }),
}));

vi.mock('@/hooks/useRolePick', () => ({
  useRolePick: () => ({ role: 'admin' }),
}));

vi.mock('@/hooks/useFleetSummary', () => ({
  useFleetSummary: () => ({
    loading: false,
    data: {
      totals: {
        nodes: 0,
        healthy: 0,
        warning: 0,
        degraded: 0,
        critical: 0,
        unknown: 0,
      },
    },
  }),
}));

const PRIMARY_DESTINATIONS = [
  'Control Room',
  'Alerts',
  'Cases',
  'Search & lifecycle',
  'Ask AI',
  'Servers',
  'Network & exposure',
  'Observability',
  'Patch posture',
  'Coverage',
  'Compliance',
  'Access',
  'Audit log',
];

const GLOBAL_SEARCH_DESTINATIONS = [
  'Control Room',
  'Alerts',
  'Cases',
  'Investigate',
  'Ask AI',
  'Servers',
  'Network & exposure',
  'Observability',
  'Patch posture',
  'Coverage',
  'Compliance',
  'Access',
  'Audit log',
];

const DRILLDOWN_ONLY_LABELS = [
  'Saved searches',
  'Knowledge graph',
  'Session replay',
  'Rules',
  'App/DB health',
  'Webserver control',
  'Recommendations',
  'Behavioral',
  'Telemetry',
  'Reports',
  'Custom dashboards',
  'Hypervisors',
  'Templates',
  'Jobs',
  'Users',
  'Roles',
];

describe('navigation scope', () => {
  it('keeps the sidebar focused on core control-room destinations', () => {
    render(
      <MemoryRouter>
        <Sidebar userRoles={['admin']} />
      </MemoryRouter>,
    );

    const nav = screen.getByRole('navigation');
    expect(within(nav).getAllByRole('link')).toHaveLength(PRIMARY_DESTINATIONS.length);

    for (const label of PRIMARY_DESTINATIONS) {
      expect(within(nav).getByRole('link', { name: new RegExp(label, 'i') })).toBeInTheDocument();
    }
    for (const label of DRILLDOWN_ONLY_LABELS) {
      expect(within(nav).queryByText(label)).not.toBeInTheDocument();
    }
  });

  it('keeps Ask AI visible as a primary investigation surface', () => {
    render(
      <MemoryRouter>
        <Sidebar userRoles={['admin']} />
      </MemoryRouter>,
    );

    const nav = screen.getByRole('navigation');
    expect(within(nav).getByRole('link', { name: /ask ai/i })).toHaveAttribute('href', '/ask');
  });

  it('keeps global search quick navigation aligned with the primary IA', async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    );

    await user.click(screen.getByRole('button', { name: /open search/i }));

    for (const label of GLOBAL_SEARCH_DESTINATIONS) {
      expect(screen.getAllByText(label).length).toBeGreaterThan(0);
    }
    for (const label of DRILLDOWN_ONLY_LABELS) {
      expect(screen.queryAllByText(label)).toHaveLength(0);
    }
  });

  it('keeps enrollment one click away without adding it to the sidebar', () => {
    render(
      <MemoryRouter>
        <TopBar />
      </MemoryRouter>,
    );

    expect(screen.getByRole('link', { name: /open enrollment/i })).toHaveAttribute('href', '/onboard');
  });

  it('keeps deeper enrollment paths in the profile drilldown', async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <ProfileMenu />
      </MemoryRouter>,
    );

    await user.click(screen.getByRole('button', { name: /profile menu/i }));

    expectMenuLink('Onboarding', '/onboard');
    expectMenuLink('Bulk server enrollment', '/fleet-enroll');
    expectMenuLink('Hypervisors and cloud', '/hypervisors');
    expectMenuLink('Offline bundles', '/offline-bundle');
  });
});

function expectMenuLink(label: string, href: string) {
  const link = screen.getByText(label).closest('a');
  expect(link).toHaveAttribute('href', href);
}
