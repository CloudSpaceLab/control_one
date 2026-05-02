// Misconduct — investigator console for UC7 cases.
//
// Surface:
//   - 4 KPI tiles (open / investigating / closed / mean risk)
//   - simple SVG histogram of risk score buckets
//   - DataTable of cases (click → slide-over detail)
//   - slide-over with three tabs: signals timeline, evidence locker,
//     workflow buttons (status transitions)
//
// Empty data wraps EmptyState; tones are constrained to the StatusTag
// palette (healthy|warning|degraded|critical|info|unknown — never `brand`).
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenant } from '../providers/TenantProvider';
import {
  DataTable,
  EmptyState,
  KpiTile,
  SectionHeader,
  StatusTag,
  type StateTone,
} from '../components/kit';
import { Button } from '../components/ui/button';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../components/ui/tabs';
import type { ColumnDef } from '@tanstack/react-table';
import type { CaseEvidenceLink, MisconductCase, RiskSignal } from '../lib/api';

const STATUS_TONE: Record<MisconductCase['status'], StateTone> = {
  open: 'warning',
  investigating: 'info',
  closed: 'healthy',
};

const SEVERITY_TONE: Record<RiskSignal['severity'], StateTone> = {
  critical: 'critical',
  high: 'degraded',
  medium: 'warning',
  low: 'info',
};

export function Misconduct(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [cases, setCases] = useState<MisconductCase[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<MisconductCase | null>(null);

  const refresh = useCallback(async () => {
    if (!currentTenantId) return;
    setLoading(true);
    setError(null);
    try {
      const resp = await client.listMisconductCases({ tenantId: currentTenantId, limit: 200 });
      setCases(resp.data ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [client, currentTenantId]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const stats = useMemo(() => {
    const open = cases.filter((c) => c.status === 'open').length;
    const investigating = cases.filter((c) => c.status === 'investigating').length;
    const closed = cases.filter((c) => c.status === 'closed').length;
    const meanRisk = cases.length === 0
      ? 0
      : Math.round(cases.reduce((acc, c) => acc + c.risk_score, 0) / cases.length);
    return { open, investigating, closed, meanRisk };
  }, [cases]);

  const histogram = useMemo(() => buildRiskHistogram(cases), [cases]);

  const columns = useMemo<ColumnDef<MisconductCase, unknown>[]>(() => ([
    {
      accessorKey: 'opened_at',
      header: 'Opened',
      cell: ({ row }) => new Date(row.original.opened_at).toLocaleDateString(),
    },
    {
      accessorKey: 'summary',
      header: 'Summary',
      cell: ({ row }) => (
        <span className="line-clamp-1 max-w-md">{row.original.summary || '—'}</span>
      ),
    },
    {
      accessorKey: 'subject_label',
      header: 'Subject',
      cell: ({ row }) => row.original.subject_label ?? row.original.subject_user_id ?? '—',
    },
    {
      accessorKey: 'risk_score',
      header: 'Risk',
      cell: ({ row }) => (
        <StatusTag tone={riskTone(row.original.risk_score)}>{row.original.risk_score}</StatusTag>
      ),
    },
    {
      accessorKey: 'status',
      header: 'Status',
      cell: ({ row }) => (
        <StatusTag tone={STATUS_TONE[row.original.status]}>{row.original.status}</StatusTag>
      ),
    },
  ]), []);

  if (!currentTenantId) {
    return (
      <div className="p-6">
        <EmptyState
          title="Select a tenant"
          description="Choose a tenant from the header to view misconduct cases."
        />
      </div>
    );
  }

  return (
    <div className="space-y-6 p-6">
      <SectionHeader
        title="Misconduct & whistleblowing"
        description="Investigator console for cases. Public intake URL: /intake."
      />
      <div className="grid grid-cols-1 gap-3 md:grid-cols-4">
        <KpiTile label="Open" value={String(stats.open)} tone="warning" />
        <KpiTile label="Investigating" value={String(stats.investigating)} tone="info" />
        <KpiTile label="Closed" value={String(stats.closed)} tone="healthy" />
        <KpiTile
          label="Mean risk score"
          value={String(stats.meanRisk)}
          tone={riskTone(stats.meanRisk)}
        />
      </div>
      <RiskHistogram buckets={histogram} />
      {error && (
        <div className="rounded border border-state-critical/40 bg-state-critical/10 p-3 text-sm text-state-critical">
          {error}
        </div>
      )}
      {cases.length === 0 && !loading ? (
        <EmptyState
          title="No cases yet"
          description="When investigators open a case or a whistleblower submission is promoted, it will appear here."
        />
      ) : (
        <DataTable
          columns={columns}
          rows={cases}
          loading={loading}
          rowKey={(c) => c.id}
          onRowClick={(c) => setSelected(c)}
        />
      )}
      {selected && (
        <CaseSlideover
          caseRow={selected}
          onClose={() => setSelected(null)}
          onUpdate={(c) => {
            setCases((prev) => prev.map((row) => (row.id === c.id ? c : row)));
            setSelected(c);
          }}
        />
      )}
    </div>
  );
}

function riskTone(score: number): StateTone {
  if (score >= 75) return 'critical';
  if (score >= 50) return 'degraded';
  if (score >= 25) return 'warning';
  if (score > 0) return 'info';
  return 'unknown';
}

interface HistogramBucket {
  label: string;
  count: number;
  tone: StateTone;
}

function buildRiskHistogram(cases: MisconductCase[]): HistogramBucket[] {
  const buckets: HistogramBucket[] = [
    { label: '0–24', count: 0, tone: 'info' },
    { label: '25–49', count: 0, tone: 'warning' },
    { label: '50–74', count: 0, tone: 'degraded' },
    { label: '75–100', count: 0, tone: 'critical' },
  ];
  for (const c of cases) {
    if (c.risk_score < 25) buckets[0].count++;
    else if (c.risk_score < 50) buckets[1].count++;
    else if (c.risk_score < 75) buckets[2].count++;
    else buckets[3].count++;
  }
  return buckets;
}

function RiskHistogram({ buckets }: { buckets: HistogramBucket[] }): JSX.Element {
  const max = Math.max(...buckets.map((b) => b.count), 1);
  const total = buckets.reduce((acc, b) => acc + b.count, 0);
  if (total === 0) {
    return (
      <EmptyState
        title="No risk score data"
        description="Run the misconduct.score job after seeding signals to populate the histogram."
      />
    );
  }
  const width = 480;
  const height = 120;
  const barWidth = width / buckets.length - 16;
  return (
    <div className="rounded-md border border-border-subtle bg-surface p-4">
      <div className="mb-2 text-sm font-medium">Risk score distribution</div>
      <svg width={width} height={height} role="img" aria-label="Risk score histogram">
        {buckets.map((b, i) => {
          const x = i * (width / buckets.length) + 8;
          const h = (b.count / max) * (height - 30);
          const y = height - 20 - h;
          return (
            <g key={b.label}>
              <rect
                x={x}
                y={y}
                width={barWidth}
                height={h}
                className={`fill-state-${b.tone}/60`}
              />
              <text
                x={x + barWidth / 2}
                y={y - 4}
                textAnchor="middle"
                className="fill-foreground text-[10px] font-medium"
              >
                {b.count}
              </text>
              <text
                x={x + barWidth / 2}
                y={height - 4}
                textAnchor="middle"
                className="fill-text-secondary text-[10px]"
              >
                {b.label}
              </text>
            </g>
          );
        })}
      </svg>
    </div>
  );
}

function CaseSlideover({
  caseRow,
  onClose,
  onUpdate,
}: {
  caseRow: MisconductCase;
  onClose: () => void;
  onUpdate: (next: MisconductCase) => void;
}): JSX.Element {
  const client = useApiClient();
  const [tab, setTab] = useState<'signals' | 'evidence' | 'workflow'>('signals');
  const [signals, setSignals] = useState<RiskSignal[]>([]);
  const [evidence, setEvidence] = useState<CaseEvidenceLink[]>([]);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const [s, e] = await Promise.all([
          client.listCaseSignals(caseRow.id),
          client.listCaseEvidence(caseRow.id),
        ]);
        if (!cancelled) {
          setSignals(s.data ?? []);
          setEvidence(e.data ?? []);
        }
      } catch {
        // surface inline below
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [client, caseRow.id]);

  async function transition(status: MisconductCase['status']) {
    setBusy(true);
    try {
      const updated = await client.updateMisconductCase(caseRow.id, { status });
      onUpdate(updated);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex" role="dialog" aria-modal="true">
      <button type="button" className="flex-1 bg-black/40" onClick={onClose} aria-label="Close" />
      <aside className="flex w-[520px] flex-col gap-4 overflow-y-auto border-l border-border-subtle bg-surface p-6">
        <header className="flex items-start justify-between">
          <div>
            <div className="text-xs uppercase tracking-wider text-text-secondary">
              Case {caseRow.id.slice(0, 8)}
            </div>
            <h2 className="mt-1 font-display text-lg font-semibold">
              {caseRow.summary || 'Untitled case'}
            </h2>
            <div className="mt-1 flex items-center gap-2 text-xs text-text-secondary">
              <StatusTag tone={STATUS_TONE[caseRow.status]}>{caseRow.status}</StatusTag>
              <span>Opened {new Date(caseRow.opened_at).toLocaleDateString()}</span>
              <span>·</span>
              <span>Risk {caseRow.risk_score}</span>
            </div>
          </div>
          <Button variant="ghost" size="sm" onClick={onClose}>
            Close
          </Button>
        </header>
        <Tabs value={tab} onValueChange={(v) => setTab(v as typeof tab)}>
          <TabsList>
            <TabsTrigger value="signals">Signals</TabsTrigger>
            <TabsTrigger value="evidence">Evidence</TabsTrigger>
            <TabsTrigger value="workflow">Workflow</TabsTrigger>
          </TabsList>
          <TabsContent value="signals" className="pt-4">
            {signals.length === 0 ? (
              <EmptyState
                title="No risk signals yet"
                description="Run the misconduct.score job to recompute signals from audit logs, security events, and compliance results."
              />
            ) : (
              <ul className="flex flex-col gap-2">
                {signals.map((s) => (
                  <li
                    key={s.id}
                    className="flex items-center justify-between rounded-md border border-border-subtle px-3 py-2 text-sm"
                  >
                    <span>
                      <span className="font-medium">{s.signal_type}</span>{' '}
                      <span className="text-text-secondary">· {new Date(s.occurred_at).toLocaleString()}</span>
                    </span>
                    <span className="flex items-center gap-2">
                      <StatusTag tone={SEVERITY_TONE[s.severity]}>{s.severity}</StatusTag>
                      <span className="text-xs text-text-secondary">+{s.weight}</span>
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </TabsContent>
          <TabsContent value="evidence" className="pt-4">
            {evidence.length === 0 ? (
              <EmptyState
                title="No evidence attached"
                description="Upload via /compliance-evidence then attach the resulting evidence id to this case."
              />
            ) : (
              <ul className="flex flex-col gap-2">
                {evidence.map((e) => (
                  <li
                    key={`${e.case_id}:${e.evidence_id}`}
                    className="rounded-md border border-border-subtle px-3 py-2 text-sm"
                  >
                    <code className="font-mono text-xs">{e.evidence_id}</code>{' '}
                    <span className="text-text-secondary">
                      attached {new Date(e.attached_at).toLocaleString()}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </TabsContent>
          <TabsContent value="workflow" className="pt-4">
            <div className="flex flex-col gap-3 text-sm">
              <p className="text-text-secondary">Move the case through its workflow.</p>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={busy || caseRow.status === 'open'}
                  onClick={() => void transition('open')}
                >
                  Mark open
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={busy || caseRow.status === 'investigating'}
                  onClick={() => void transition('investigating')}
                >
                  Investigating
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={busy || caseRow.status === 'closed'}
                  onClick={() => void transition('closed')}
                >
                  Close
                </Button>
              </div>
            </div>
          </TabsContent>
        </Tabs>
      </aside>
    </div>
  );
}
