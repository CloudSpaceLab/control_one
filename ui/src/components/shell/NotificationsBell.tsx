import { useCallback, useEffect, useState } from 'react';
import { Bell } from 'lucide-react';
import { Link } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { useApiClient } from '@/hooks/useApiClient';
import { useTenant } from '@/providers/TenantProvider';
import type { Notification } from '@/lib/api';

export function NotificationsBell(): JSX.Element {
  const api = useApiClient();
  const { currentTenantId } = useTenant();
  const [open, setOpen] = useState(false);
  const [unread, setUnread] = useState(0);
  const [items, setItems] = useState<Notification[]>([]);
  const [loading, setLoading] = useState(false);

  const refreshCount = useCallback(async () => {
    if (!currentTenantId) {
      setUnread(0);
      return;
    }
    try {
      setUnread(await api.getUnreadNotificationsCount(currentTenantId));
    } catch {
      // Preserve the last known count during transient failures.
    }
  }, [api, currentTenantId]);

  useEffect(() => {
    void refreshCount();
    const poll = window.setInterval(() => void refreshCount(), 30_000);
    const onFocus = () => void refreshCount();
    window.addEventListener('focus', onFocus);
    return () => {
      window.clearInterval(poll);
      window.removeEventListener('focus', onFocus);
    };
  }, [refreshCount]);

  const loadItems = async () => {
    if (!currentTenantId) return;
    setLoading(true);
    try {
      const response = await api.listNotifications(currentTenantId, { unreadOnly: true, limit: 20 });
      setItems(response.data);
    } catch {
      setItems([]);
    } finally {
      setLoading(false);
    }
  };

  const markAllRead = async () => {
    if (!currentTenantId) return;
    await api.markAllNotificationsRead(currentTenantId);
    setItems([]);
    setUnread(0);
  };

  const markRead = async (notification: Notification) => {
    if (!currentTenantId) return;
    await api.markNotificationRead(notification.id, currentTenantId);
    setItems((current) => current.filter((item) => item.id !== notification.id));
    setUnread((current) => Math.max(0, current - 1));
  };

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (next) void loadItems();
      }}
    >
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="relative"
          aria-label={unread > 0 ? `Notifications, ${unread} unread` : 'Notifications'}
        >
          <Bell className="h-4 w-4" aria-hidden />
          {unread > 0 ? (
            <span className="absolute -right-0.5 -top-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-state-critical px-1 text-[10px] font-semibold text-white">
              {unread > 99 ? '99+' : unread}
            </span>
          ) : null}
        </Button>
      </PopoverTrigger>
      <PopoverContent aria-label="Notifications" align="end" className="w-80 p-0">
        <div className="flex items-center justify-between border-b border-border-subtle px-3 py-2">
          <p className="text-sm font-medium text-foreground">Notifications</p>
          {items.length > 0 ? (
            <Button type="button" size="sm" variant="ghost" onClick={() => void markAllRead()}>
              Mark all read
            </Button>
          ) : null}
        </div>
        {loading ? (
          <p className="px-4 py-6 text-center text-sm text-text-secondary">Loading notifications…</p>
        ) : items.length === 0 ? (
          <p className="px-4 py-6 text-center text-sm text-text-secondary">No unread notifications</p>
        ) : (
          <ul className="max-h-80 overflow-y-auto">
            {items.map((notification) => (
              <li key={notification.id}>
                <Link
                  to="/cases"
                  className="flex flex-col gap-0.5 px-3 py-2 hover:bg-elevated"
                  onClick={() => void markRead(notification)}
                >
                  <span className="text-xs font-medium text-foreground">
                    {notification.kind === 'case_assigned'
                      ? `${notification.actor_name || 'A teammate'} assigned ${notification.case_title}`
                      : `${notification.actor_name || 'A teammate'} mentioned you in ${notification.case_title}`}
                  </span>
                  <span className="text-xs text-text-secondary">{notificationAge(notification.created_at)}</span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </PopoverContent>
    </Popover>
  );
}

function notificationAge(value: string): string {
  const elapsed = Date.now() - new Date(value).getTime();
  if (!Number.isFinite(elapsed) || elapsed < 60_000) return 'just now';
  const minutes = Math.floor(elapsed / 60_000);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  return hours < 24 ? `${hours}h ago` : `${Math.floor(hours / 24)}d ago`;
}
