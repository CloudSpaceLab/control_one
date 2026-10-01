import type { ControlRoomExecutiveOverview } from '@/lib/api';
import { Panel, Sparkline } from '@/components/kit';

export function SecurityActivity({ overview }: { overview: ControlRoomExecutiveOverview }) {
  const available = overview.availability.activity;
  const data = available ? overview.activity.map((point) => point.total) : [];

  return (
    <Panel eyebrow="SECURITY ACTIVITY" title={available ? 'Selected period' : 'Data unavailable'}>
      <Sparkline
        data={data}
        tone={overview.attention.critical > 0 ? 'critical' : overview.attention.total > 0 ? 'warning' : 'accent'}
        height={72}
        ariaLabel="Security activity trend"
        emptyLabel={available ? 'No security activity' : 'Activity unavailable'}
      />
      <div className="grid grid-cols-3 divide-x divide-border-subtle rounded-md border border-border-subtle bg-surface">
        <ActivityFact label="Violations" value={overview.availability.violations ? overview.violations.total : 'N/A'} />
        <ActivityFact label="Auto handled" value={overview.availability.response ? overview.response.handled_automatically : 'N/A'} />
        <ActivityFact label="Needs attention" value={overview.availability.attention ? overview.attention.total : 'N/A'} />
      </div>
    </Panel>
  );
}

function ActivityFact({ label, value }: { label: string; value: number | string }) {
  return (
    <div className="min-w-0 px-3 py-3">
      <p className="truncate text-[0.68rem] uppercase tracking-wide text-text-muted">{label}</p>
      <p className="mt-1 font-mono text-lg font-semibold tabular-nums text-foreground">{value}</p>
    </div>
  );
}
