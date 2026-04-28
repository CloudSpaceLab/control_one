import { useEffect, useState } from 'react';
import { Download, FileBarChart } from 'lucide-react';
import { Button } from '../components/ui/button';
import { Label } from '../components/ui/label';
import {
  EmptyState,
  Panel,
  SectionHeader,
} from '../components/kit';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import type { ReportDesc } from '../lib/api';

const RANGE_PRESETS: { label: string; days: number }[] = [
  { label: 'Last 24h', days: 1 },
  { label: 'Last 7 days', days: 7 },
  { label: 'Last 30 days', days: 30 },
  { label: 'Last 90 days', days: 90 },
];

export function Reports(): JSX.Element {
  const client = useApiClient();
  const { data: tenants } = useTenants({ limit: 50, offset: 0 });
  const [tenantId, setTenantId] = useState('');
  const [reports, setReports] = useState<ReportDesc[]>([]);
  const [range, setRange] = useState(30);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!tenantId && tenants[0]?.id) setTenantId(tenants[0].id);
  }, [tenants, tenantId]);

  useEffect(() => {
    (async () => {
      try {
        const resp = await client.listReports();
        setReports(resp.data);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'load failed');
      }
    })();
  }, [client]);

  const download = (slug: string) => {
    const since = new Date(Date.now() - range * 24 * 60 * 60 * 1000).toISOString();
    const url = client.buildReportExportUrl(slug, { tenantId, since });
    window.open(url, '_blank');
  };

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="POSTURE · REPORTS"
        title="Reports"
        description="Download CSV extracts for compliance, audit, alerts, and access."
      />

      <Panel padding="md" eyebrow="FILTERS" title="Refine">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <FilterSelect
            label="Tenant"
            value={tenantId}
            onChange={(v) => setTenantId(v)}
            options={tenants.map((t) => ({ label: t.name, value: t.id }))}
          />
          <FilterSelect
            label="Range"
            value={String(range)}
            onChange={(v) => setRange(Number(v))}
            options={RANGE_PRESETS.map((p) => ({ label: p.label, value: String(p.days) }))}
          />
        </div>
      </Panel>

      {error && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Failed to load">
          <p className="text-sm text-state-critical">{error}</p>
        </Panel>
      )}

      {reports.length === 0 ? (
        <EmptyState
          icon={<FileBarChart />}
          title="No reports available"
          description="The catalog is empty for this build."
        />
      ) : (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {reports.map((rep) => (
            <Panel key={rep.slug} padding="md" eyebrow={rep.slug.toUpperCase()} title={rep.title}>
              <p className="min-h-[3em] text-sm text-text-secondary">{rep.description}</p>
              <div className="flex flex-wrap gap-2">
                {rep.formats.map((fmt) => (
                  <Button
                    key={fmt}
                    variant="primary"
                    size="sm"
                    onClick={() => download(rep.slug)}
                  >
                    <Download className="h-4 w-4" /> {fmt.toUpperCase()}
                  </Button>
                ))}
              </div>
            </Panel>
          ))}
        </div>
      )}
    </div>
  );
}

function FilterSelect({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: { label: string; value: string }[];
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label>{label}</Label>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="flex h-9 w-full rounded-md border border-border-subtle bg-surface px-3 py-1 text-sm text-foreground focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30"
      >
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </div>
  );
}
