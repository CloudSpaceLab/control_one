import { useQuery } from '@tanstack/react-query';
import { Activity, Plus, Server, Users } from 'lucide-react';
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import {
  DEFAULT_TIME_RANGES,
  Chart,
  DataTable,
  EmptyState,
  KpiTile,
  OnboardingChecklist,
  Panel,
  PostureBar,
  SectionHeader,
  StatusDot,
  TimeRangePills,
} from '@/components/kit';
import { DashboardGrid, DashboardGridItem } from '@/components/shell';
import { useApiClient } from '@/hooks/useApiClient';
import { useLiveSubscribe } from '@/hooks/useLiveSubscribe';
import { useOnboardingState } from '@/hooks/useOnboardingState';
import { useTenant } from '@/providers/TenantProvider';
import type {
  AdminCapacity,
  AdminIngestThroughput,
  AdminSelfHealth,
  AdminSLO,
  AdminTenantsActivity,
  AdminTenantActivity,
  DashboardOverview,
} from '@/lib/api';
import type { ColumnDef } from '@tanstack/react-table';

export function AdminDashboard(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [period, setPeriod] = useState('24h');

  const overviewQ = useQuery<DashboardOverview>({
    queryKey: ['dashboard.overview', currentTenantId],
    queryFn: () => client.getDashboardOverview(currentTenantId ?? undefined),
    refetchInterval: 30_000,
  });
  const healthQ = useQuery<AdminSelfHealth>({
    queryKey: ['admin.self-health'],
    queryFn: () => client.getAdminSelfHealth(),
    refetchInterval: 15_000,
  });
  const ingestQ = useQuery<AdminIngestThroughput>({
    queryKey: ['admin.ingest', period],
    queryFn: () =>
      client.getAdminIngestThroughput('events', period === '1h' ? '1m' : '5m', period),
    refetchInterval: 30_000,
  });
  const tenantsQ = useQuery<AdminTenantsActivity>({
    queryKey: ['admin.tenants.activity', period],
    queryFn: () => client.getAdminTenantsActivity(period),
    refetchInterval: 60_000,
  });
  const sloQ = useQuery<AdminSLO>({
    queryKey: ['admin.slo'],
    queryFn: () => client.getAdminSLO(),
    refetchInterval: 60_000,
  });
  const capacityQ = useQuery<AdminCapacity>({
    queryKey: ['admin.capacity'],
    queryFn: () => client.getAdminCapacity(),
    refetchInterval: 60_000,
  });

  const live = useLiveSubscribe(currentTenantId ?? undefined, [
    { topic: 'health.incident', invalidate: [['admin.self-health'], ['dashboard.overview']] },
    { topic: 'security.event', invalidate: [['dashboard.overview']] },
  ]);

  const tenantColumns: ColumnDef<AdminTenantActivity>[] = [
    {
      accessorKey: 'name',
      header: 'Tenant',
      cell: ({ row }) => (
        <div className="flex flex-col">
          <span className="text-sm text-foreground">{row.original.name}</span>
          <span className="font-mono text-[0.65rem] text-text-muted">{row.original.tenant_id}</span>
        </div>
      ),
    },
    {
      accessorKey: 'events_24h',
      header: 'Events',
      cell: ({ getValue }) => (
        <span className="font-mono text-sm tabular-nums">{(getValue() as number).toLocaleString()}</span>
      ),
    },
    { accessorKey: 'nodes', header: 'Nodes', cell: ({ getValue }) => <span className="font-mono">{getValue() as number}</span> },
    { accessorKey: 'users_active', header: 'Users', cell: ({ getValue }) => <span className="font-mono">{getValue() as number}</span> },
    {
      accessorKey: 'last_seen',
      header: 'Last seen',
      cell: ({ getValue }) => (
        <span className="text-xs text-text-secondary">
          {getValue() ? new Date(getValue() as string).toLocaleString() : '—'}
        </span>
      ),
    },
  ];

  const sloRows = sloQ.data?.slos ?? [];
  const onboarding = useOnboardingState();

  if (onboarding.isReady && onboarding.isEmpty) {
    return (
      <div className="flex flex-col gap-6">
        <SectionHeader
          eyebrow="WELCOME · CISO ONBOARDING"
          title="Let's get Control One protecting your fleet"
          description="A few quick steps to wire up your first tenant, server, and detection rules. You can return to this checklist anytime via /onboard."
        />
        <OnboardingChecklist steps={onboarding.steps} />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      {!onboarding.steps.every((s) => s.done || !s.required) && (
        <OnboardingChecklist
          eyebrow="GET STARTED"
          title="Finish setting up Control One"
          description="A handful of required steps remain — finish them to unlock realtime detection and posture scans."
          steps={onboarding.steps}
        />
      )}
      <SectionHeader
        eyebrow="PLATFORM CONTROL"
        title="Admin Console"
        description="Fleet, capacity and tenant health across the entire control plane."
        actions={
          <>
            <TimeRangePills value={period} options={DEFAULT_TIME_RANGES} onChange={setPeriod} />
            <Button asChild variant="primary" shimmer>
              <Link to="/fleet-enroll">
                <Plus className="h-4 w-4" /> Enroll node
              </Link>
            </Button>
          </>
        }
      />

      <DashboardGrid>
        <DashboardGridItem span={{ base: 6, md: 3 }}>
          <KpiTile
            label="NODES ONLINE"
            value={overviewQ.data?.node_counts.healthy ?? '—'}
            tone="healthy"
            icon={<Server />}
            hint={`${overviewQ.data?.node_counts.total ?? 0} total · ${overviewQ.data?.node_counts.offline ?? 0} offline`}
            loading={overviewQ.isLoading}
          />
        </DashboardGridItem>
        <DashboardGridItem span={{ base: 6, md: 3 }}>
          <KpiTile
            label="CONTROL PLANE"
            value={
              healthQ.data ? (
                <span className="inline-flex items-center gap-2">
                  <StatusDot
                    tone={
                      healthQ.data.status === 'ok' ? 'healthy' :
                      healthQ.data.status === 'degraded' ? 'warning' : 'critical'
                    }
                    pulse={healthQ.data.status !== 'ok'}
                  />
                  <span className="uppercase">{healthQ.data.status}</span>
                </span>
              ) : '—'
            }
            tone={
              healthQ.data?.status === 'ok' ? 'healthy' :
              healthQ.data?.status === 'degraded' ? 'warning' : 'critical'
            }
            hint={`api p95 ${healthQ.data?.api_p95_ms ?? 0}ms · queue ${healthQ.data?.queue_depth ?? 0}`}
            loading={healthQ.isLoading}
          />
        </DashboardGridItem>
        <DashboardGridItem span={{ base: 6, md: 3 }}>
          <KpiTile
            label="INGEST"
            value={`${(ingestQ.data?.totals.events ?? 0).toLocaleString()}`}
            tone="brand"
            sparkline={(ingestQ.data?.series ?? []).slice(-30).map((p) => p.events_per_sec)}
            hint={
              (ingestQ.data?.totals.bytes ?? 0) > 0
                ? `${(ingestQ.data?.totals.bytes ?? 0).toLocaleString()} bytes · ${period}`
                : `events · ${period}`
            }
            loading={ingestQ.isLoading}
          />
        </DashboardGridItem>
        <DashboardGridItem span={{ base: 6, md: 3 }}>
          <KpiTile
            label="ACTIVE TENANTS"
            value={tenantsQ.data?.active_count ?? '—'}
            tone="accent"
            icon={<Users />}
            hint={`${tenantsQ.data?.total_count ?? 0} total`}
            loading={tenantsQ.isLoading}
          />
        </DashboardGridItem>

        <DashboardGridItem span={{ base: 12, lg: 8 }}>
          <Panel padding="md" eyebrow="INGEST" title="Throughput" toneAccent="brand">
            {ingestQ.data && ingestQ.data.series.length > 0 ? (
              <Chart
                kind="line"
                height={240}
                ariaLabel="Ingest throughput"
                data={{
                  labels: ingestQ.data.series.map((p) => new Date(p.ts).toLocaleTimeString()),
                  datasets: [
                    {
                      label: 'events/sec',
                      data: ingestQ.data.series.map((p) => p.events_per_sec),
                      borderColor: 'var(--brand-500)',
                      backgroundColor: 'rgba(99,102,241,0.15)',
                      fill: true,
                    },
                  ],
                }}
              />
            ) : (
              <EmptyState icon={<Activity />} title="No ingest data yet" description="Throughput metrics will appear once events stream." />
            )}
          </Panel>
        </DashboardGridItem>
        <DashboardGridItem span={{ base: 12, lg: 4 }}>
          <Panel padding="md" eyebrow="PLATFORM SLOs" title="Burn rate">
            {sloQ.isLoading ? (
              <div className="flex flex-col gap-2">
                {[0, 1, 2].map((i) => <Skeleton key={i} className="h-10 w-full" />)}
              </div>
            ) : sloRows.length === 0 ? (
              <EmptyState title="No SLOs configured" description="SLOs will surface here once defined." />
            ) : (
              <ul className="flex flex-col gap-3">
                {sloRows.map((slo) => {
                  const pct = slo.target > 0 ? (slo.actual / slo.target) * 100 : 0;
                  return (
                    <li key={slo.name} className="flex flex-col gap-1">
                      <div className="flex items-center justify-between text-sm">
                        <span className="text-foreground">{slo.name}</span>
                        <span className="font-mono text-xs text-text-secondary">{slo.window}</span>
                      </div>
                      <PostureBar score={Math.min(100, pct)} ariaLabel={`${slo.name} attainment`} />
                      <div className="flex items-center justify-between font-mono text-[0.65rem] text-text-muted">
                        <span>actual {slo.actual.toFixed(2)}</span>
                        <span>target {slo.target.toFixed(2)}</span>
                        <span>burn {slo.burn_rate.toFixed(2)}×</span>
                      </div>
                    </li>
                  );
                })}
              </ul>
            )}
          </Panel>
        </DashboardGridItem>

        <DashboardGridItem span={{ base: 12, lg: 7 }}>
          <Panel padding="md" eyebrow="TENANTS" title="Activity" toneAccent="accent">
            <DataTable
              columns={tenantColumns}
              rows={tenantsQ.data?.top ?? []}
              rowKey={(r) => r.tenant_id}
              loading={tenantsQ.isLoading}
              compact
              empty={<EmptyState title="No tenants yet" description="Create a tenant to see activity here." />}
            />
          </Panel>
        </DashboardGridItem>
        <DashboardGridItem span={{ base: 12, lg: 5 }}>
          <Panel padding="md" eyebrow="CAPACITY" title="Storage & retention">
            {capacityQ.isLoading ? (
              <Skeleton className="h-32 w-full" />
            ) : capacityQ.data ? (
              <div className="flex flex-col gap-4">
                <div>
                  <div className="flex items-center justify-between text-sm">
                    <span>Disk</span>
                    <span className="font-mono text-xs text-text-secondary">
                      {humanBytes(capacityQ.data.disk_used)} / {humanBytes(capacityQ.data.disk_total)}
                    </span>
                  </div>
                  <PostureBar
                    score={
                      capacityQ.data.disk_total > 0
                        ? (capacityQ.data.disk_used / capacityQ.data.disk_total) * 100
                        : 0
                    }
                    ariaLabel="Disk usage"
                  />
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div className="rounded-md border border-border-subtle bg-surface px-3 py-2">
                    <div className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">Doris</div>
                    <div className="inline-flex items-center gap-2 text-sm">
                      <StatusDot tone={capacityQ.data.doris_status === 'ok' ? 'healthy' : 'degraded'} />
                      {capacityQ.data.doris_status || 'unknown'}
                    </div>
                  </div>
                  <div className="rounded-md border border-border-subtle bg-surface px-3 py-2">
                    <div className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">Postgres</div>
                    <div className="inline-flex items-center gap-2 text-sm">
                      <StatusDot tone={capacityQ.data.postgres_status === 'ok' ? 'healthy' : 'degraded'} />
                      {capacityQ.data.postgres_status || 'unknown'}
                    </div>
                  </div>
                </div>
                <div className="rounded-md border border-border-subtle bg-surface px-3 py-2 text-sm">
                  <span className="text-text-muted">Retention left</span>
                  <span className="ml-2 font-mono">{capacityQ.data.retention_days_remaining}d</span>
                </div>
              </div>
            ) : null}
          </Panel>
        </DashboardGridItem>
      </DashboardGrid>

      <div className="flex items-center justify-end text-xs text-text-muted">
        <span className="font-mono uppercase">live · {live.state}</span>
      </div>
    </div>
  );
}

function humanBytes(n: number): string {
  if (!n || n <= 0) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(1)} ${u[i]}`;
}
