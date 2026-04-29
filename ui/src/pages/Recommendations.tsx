import { useCallback, useEffect, useState } from 'react';
import { Lightbulb, RefreshCw } from 'lucide-react';
import { Button } from '../components/ui/button';
import {
  EmptyState,
  ExpandableCode,
  KpiTile,
  Panel,
  SectionHeader,
  SelectField,
} from '../components/kit';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { useToast } from '../providers/ToastProvider';
import type { Recommendation } from '../lib/api';

export function Recommendations(): JSX.Element {
  const client = useApiClient();
  const { data: tenants } = useTenants({ limit: 50, offset: 0 });
  const { showToast } = useToast();
  const [tenantId, setTenantId] = useState('');
  const [items, setItems] = useState<Recommendation[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!tenantId && tenants[0]?.id) setTenantId(tenants[0].id);
  }, [tenants, tenantId]);

  const refresh = useCallback(async () => {
    if (!tenantId) return;
    setLoading(true);
    try {
      const resp = await client.listRecommendations(tenantId);
      setItems(resp.data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [client, tenantId]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const promote = async (rec: Recommendation) => {
    if (rec.kind !== 'port_rule') {
      showToast('Promote only supported for port-rule drafts in this build.', 'info');
      return;
    }
    try {
      const d = rec.draft;
      await client.createPortRule({
        tenant_id: tenantId,
        name: String(rec.title),
        port: Number(d.port),
        protocol: String(d.protocol) as 'tcp' | 'udp',
        expected_state: String(d.expected_state) as 'open' | 'closed',
        severity: String(d.severity ?? 'medium'),
        action: String(d.action ?? 'notify'),
        enabled: true,
      });
      showToast('Promoted to port rule.', 'success');
      refresh();
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Promote failed', 'error');
    }
  };

  const avgConfidence =
    items.length > 0
      ? Math.round((items.reduce((s, r) => s + r.confidence, 0) / items.length) * 100)
      : 0;

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="BEHAVIORAL · RECOMMENDATIONS"
        title="Recommendations"
        description="Derived from 30 days of port observations."
        actions={
          <Button variant="secondary" size="md" onClick={refresh} disabled={loading}>
            <RefreshCw className="h-4 w-4" /> Refresh
          </Button>
        }
      />

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <KpiTile label="DRAFTS READY" value={items.length} tone={items.length > 0 ? 'brand' : 'unknown'} />
        <KpiTile label="AVG CONFIDENCE" value={`${avgConfidence}%`} tone="info" />
      </div>

      <Panel padding="md" eyebrow="FILTERS" title="Refine">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <FilterSelect
            label="Tenant"
            value={tenantId}
            onChange={(v) => setTenantId(v)}
            options={tenants.map((t) => ({ label: t.name, value: t.id }))}
          />
        </div>
      </Panel>

      {error && (
        <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Failed to load">
          <p className="text-sm text-state-critical">{error}</p>
        </Panel>
      )}

      {items.length === 0 && !loading ? (
        <EmptyState
          icon={<Lightbulb />}
          title="No recommendations yet"
          description="Data needs ≥30 days of observations before drafts are produced."
        />
      ) : (
        <div className="grid grid-cols-1 gap-3">
          {items.map((rec, i) => (
            <Panel
              key={i}
              padding="md"
              eyebrow={rec.kind.toUpperCase()}
              title={rec.title}
              actions={
                <Button variant="primary" size="sm" onClick={() => promote(rec)}>
                  Promote
                </Button>
              }
            >
              <p className="text-sm text-text-secondary">{rec.rationale}</p>
              <div className="text-xs font-mono text-text-muted">
                Confidence: {(rec.confidence * 100).toFixed(1)}%
              </div>
              <ExpandableCode label="Draft" content={JSON.stringify(rec.draft, null, 2)} />
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
    <SelectField
      label={label}
      value={value}
      onChange={(e) => onChange(e.target.value)}
    >
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </SelectField>
  );
}
