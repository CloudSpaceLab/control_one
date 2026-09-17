import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import { NotificationsBell } from './NotificationsBell';
import * as useApiClientModule from '@/hooks/useApiClient';
import * as useTenantModule from '@/providers/TenantProvider';

describe('NotificationsBell', () => {
  it('shows the unread count and marks all notifications read', async () => {
    const user = userEvent.setup();
    const api = {
      getUnreadNotificationsCount: vi.fn().mockResolvedValueOnce(2).mockResolvedValue(0),
      listNotifications: vi.fn().mockResolvedValue({
        data: [{
          id: 'notification-1',
          tenant_id: 'tenant-1',
          recipient_id: 'user-1',
          actor_name: 'Ada CISO',
          kind: 'case_assigned',
          case_id: 'case-1',
          case_title: 'Suspicious login',
          created_at: new Date().toISOString(),
        }],
        pagination: { total: 1, count: 1, limit: 20, offset: 0, nextOffset: null, prevOffset: null },
      }),
      markAllNotificationsRead: vi.fn().mockResolvedValue(undefined),
      markNotificationRead: vi.fn().mockResolvedValue(undefined),
    };
    vi.spyOn(useTenantModule, 'useTenant').mockReturnValue({
      currentTenantId: 'tenant-1',
      currentTenant: null,
      tenants: [],
      loading: false,
      error: null,
      setCurrentTenantId: vi.fn(),
      refresh: vi.fn(),
    });
    vi.spyOn(useApiClientModule, 'useApiClient').mockReturnValue(api as never);

    render(
      <MemoryRouter>
        <NotificationsBell />
      </MemoryRouter>,
    );

    expect(await screen.findByRole('button', { name: 'Notifications, 2 unread' })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Notifications, 2 unread' }));
    await user.click(await screen.findByRole('button', { name: 'Mark all read' }));

    expect(api.markAllNotificationsRead).toHaveBeenCalledWith('tenant-1');
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Notifications' })).toBeInTheDocument();
    });
  });
});
