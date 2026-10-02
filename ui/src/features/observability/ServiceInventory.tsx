import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Panel, StatusTag } from '@/components/kit';
import { useApiClient } from '@/hooks/useApiClient';
import type { PaginationMeta, TenantNodeService } from '@/lib/api';

type DeviceScope = 'all' | 'server' | 'endpoint' | 'unknown';

const EMPTY_PAGINATION: PaginationMeta = {
  total: 0,
  count: 0,
  limit: 50,
  offset: 0,
  nextOffset: null,
  prevOffset: null,
};

export function ServiceInventory({
  tenantId,
  tenantLabel,
}: {
  tenantId?: string;
  tenantLabel: string;
}): JSX.Element {
  const api = useApiClient();
  const [query, setQuery] = useState('');
  const [scope, setScope] = useState<DeviceScope>('all');
  const [offset, setOffset] = useState(0);
  const [state, setState] = useState<{
    rows: TenantNodeService[];
    pagination: PaginationMeta;
    loading: boolean;
    error: string | null;
  }>({
    rows: [],
    pagination: EMPTY_PAGINATION,
    loading: false,
    error: null,
  });

  useEffect(() => {
    setOffset(0);
  }, [tenantId]);

  useEffect(() => {
    if (!tenantId) {
      setState({
        rows: [],
        pagination: EMPTY_PAGINATION,
        loading: false,
        error: null,
      });
      return;
    }

    let cancelled = false;
    const timer = window.setTimeout(() => {
      setState((current) => ({ ...current, loading: true, error: null }));
      void api
        .listTenantNodeServices({
          tenantId,
          query,
          targetScope: scope,
          limit: 50,
          offset,
        })
        .then((response) => {
          if (cancelled) return;
          setState({
            rows: Array.isArray(response.data) ? response.data : [],
            pagination: response.pagination ?? EMPTY_PAGINATION,
            loading: false,
            error: null,
          });
        })
        .catch((error: unknown) => {
          if (cancelled) return;
          setState((current) => ({
            ...current,
            rows: [],
            loading: false,
            error: error instanceof Error ? error.message : 'Service inventory unavailable',
          }));
        });
    }, query.trim() ? 250 : 0);

    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [api, offset, query, scope, tenantId]);

  const resetVisibleRows = () => {
    setState((current) => ({
      ...current,
      rows: [],
      pagination: { ...current.pagination, count: 0, offset: 0 },
    }));
  };
  const updateQuery = (value: string) => {
    setQuery(value);
    setOffset(0);
    resetVisibleRows();
  };
  const updateScope = (value: DeviceScope) => {
    setScope(value);
    setOffset(0);
    resetVisibleRows();
  };

  return (
    <Panel
      padding="md"
      eyebrow="SERVICE INVENTORY"
      title={tenantId ? `${tenantLabel} services` : 'Service inventory'}
      actions={
        tenantId ? (
          <StatusTag tone="info">
            {state.loading ? 'loading' : `${state.pagination.total} discovered`}
          </StatusTag>
        ) : null
      }
    >
      <p className="text-sm text-text-secondary">
        Latest listening applications and services reported by node agents. Discovery shows presence, not application health.
      </p>

      {!tenantId ? (
        <p className="rounded-md border border-border-subtle bg-surface p-3 text-sm text-text-muted">
          Select a tenant to view services across its nodes.
        </p>
      ) : (
        <>
          <div className="flex flex-col gap-2 md:flex-row md:items-center">
            <input
              type="search"
              aria-label="Search services"
              value={query}
              onChange={(event) => updateQuery(event.target.value)}
              placeholder="Search app, process, port, or node"
              className="h-9 min-w-0 flex-1 rounded-md border border-border-subtle bg-surface px-3 text-sm text-foreground"
            />
            <select
              aria-label="Device scope"
              value={scope}
              onChange={(event) => updateScope(event.target.value as DeviceScope)}
              className="h-9 rounded-md border border-border-subtle bg-surface px-3 text-sm text-foreground"
            >
              <option value="all">All devices</option>
              <option value="server">Server nodes</option>
              <option value="endpoint">Employee endpoints</option>
              <option value="unknown">Unclassified devices</option>
            </select>
          </div>

          {state.error ? (
            <p role="alert" className="rounded-md border border-border-subtle bg-surface p-3 text-sm text-text-secondary">
              {state.error}
            </p>
          ) : null}

          <div className="overflow-x-auto rounded-lg border border-border-subtle">
            <table className="w-full min-w-[760px] text-sm">
              <thead className="bg-surface-2 text-left text-xs uppercase tracking-wide text-text-secondary">
                <tr>
                  <th className="px-3 py-2">Application / service</th>
                  <th className="px-3 py-2">Node</th>
                  <th className="px-3 py-2">Device</th>
                  <th className="px-3 py-2">Listener</th>
                  <th className="px-3 py-2">Observed</th>
                </tr>
              </thead>
              <tbody>
                {state.rows.map((service) => (
                  <ServiceRow key={service.id} service={service} />
                ))}
                {!state.loading && state.rows.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="px-3 py-8 text-center text-sm text-text-muted">
                      No listening services match this scope.
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>

          <div className="flex flex-wrap items-center justify-between gap-3 text-xs text-text-muted">
            <span>
              {pageRange(state.pagination)}
            </span>
            <div className="flex gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={state.loading || state.pagination.prevOffset === null}
                onClick={() => setOffset(state.pagination.prevOffset ?? 0)}
              >
                Previous
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={state.loading || state.pagination.nextOffset === null}
                onClick={() => setOffset(state.pagination.nextOffset ?? offset)}
              >
                Next
              </Button>
            </div>
          </div>
        </>
      )}
    </Panel>
  );
}

function ServiceRow({ service }: { service: TenantNodeService }): JSX.Element {
  const name = service.app_name?.trim() || service.service_kind?.trim() || service.process?.trim() || `Port ${service.port}`;
  const detail = [service.process?.trim(), service.app_profile_id?.trim()]
    .filter((value, index, values) => Boolean(value) && values.indexOf(value) === index)
    .join(' · ');

  return (
    <tr className="border-t border-border-subtle">
      <td className="px-3 py-3 align-top">
        <div className="font-medium text-foreground">{name}</div>
        {detail ? <div className="mt-0.5 text-xs text-text-muted">{detail}</div> : null}
      </td>
      <td className="px-3 py-3 align-top">
        <Link to={`/nodes/${service.node_id}`} className="font-medium text-foreground hover:underline">
          {service.node_hostname || service.node_id.slice(0, 8)}
        </Link>
        <div className="mt-0.5 text-xs text-text-muted">
          {service.node_last_seen_at ? `Node seen ${relativeAge(service.node_last_seen_at)}` : service.node_state}
        </div>
      </td>
      <td className="px-3 py-3 align-top">
        <div className="text-foreground">{targetTypeLabel(service.node_target_type)}</div>
        <div className="mt-0.5 text-xs text-text-muted">{deviceGroupLabel(service.node_target_type)}</div>
      </td>
      <td className="px-3 py-3 align-top font-mono text-xs text-text-secondary">
        {listenerLabel(service.listen_addr, service.port)}
      </td>
      <td className="px-3 py-3 align-top">
        <div className="text-foreground">{relativeAge(service.observed_at)}</div>
        <div className="mt-0.5 text-xs text-text-muted">{formatTimestamp(service.observed_at)}</div>
      </td>
    </tr>
  );
}

function listenerLabel(address: string, port: number): string {
  const host = address?.trim();
  if (!host || host === '0.0.0.0' || host === '::' || host === '[::]') {
    return `all interfaces:${port}`;
  }
  return `${host}:${port}`;
}

function targetTypeLabel(value: string): string {
  const normalized = value?.trim().toLowerCase() || 'unknown';
  const labels: Record<string, string> = {
    personal_pc: 'Personal PC',
    workstation: 'Workstation',
    laptop: 'Laptop',
    server: 'Server',
    vm: 'Virtual machine',
    cloud_instance: 'Cloud instance',
    domain_controller: 'Domain controller',
    kiosk: 'Kiosk',
    unknown: 'Unclassified',
  };
  return labels[normalized] ?? normalized.replaceAll('_', ' ');
}

function deviceGroupLabel(value: string): string {
  const normalized = value?.trim().toLowerCase();
  if (['personal_pc', 'workstation', 'laptop', 'kiosk'].includes(normalized)) return 'Employee endpoint';
  if (['server', 'vm', 'cloud_instance', 'domain_controller'].includes(normalized)) return 'Server / infrastructure';
  return 'Device type not classified';
}

function relativeAge(value: string): string {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) return 'unknown';
  const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
  if (seconds < 60) return 'just now';
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

function formatTimestamp(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString([], {
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function pageRange(pagination: PaginationMeta): string {
  if (pagination.total === 0) return '0 services';
  const start = pagination.offset + 1;
  const end = pagination.offset + pagination.count;
  return `${start}–${end} of ${pagination.total}`;
}

export default ServiceInventory;
