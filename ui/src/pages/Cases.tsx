import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  AlertTriangle,
  ArrowRight,
  BookOpenText,
  CheckCircle2,
  ClipboardList,
  Download,
  ExternalLink,
  FileText,
  Fingerprint,
  Link2,
  MessageSquarePlus,
  Network,
  RefreshCw,
  Server,
  Shield,
  ShieldCheck,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
  EmptyState,
  IpActionMenu,
  Panel,
  SectionHeader,
  StatusTag,
  type StateTone,
} from '@/components/kit';
import { useApiClient } from '@/hooks/useApiClient';
import { useTenant } from '@/providers/TenantProvider';
import { cn } from '@/lib/utils';
import type { SOCCase, SOCCaseExport, SOCCaseEvidenceRef, SOCCaseTimelineItem } from '@/lib/api';
import { entityRoute } from '@/lib/entity';

export function Cases(): JSX.Element {
  const api = useApiClient();
  const { currentTenantId, currentTenant } = useTenant();
  const [cases, setCases] = useState<SOCCase[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [selectedCase, setSelectedCase] = useState<SOCCase | null>(null);
  const [exportPreview, setExportPreview] = useState<SOCCaseExport | null>(null);
  const [loading, setLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [noteDraft, setNoteDraft] = useState('');
  const [noteStatus, setNoteStatus] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!currentTenantId) {
      setCases([]);
      setSelectedCase(null);
      setLoading(false);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const response = await api.listSOCCases({ tenantId: currentTenantId, limit: 50 });
      setCases(response.data);
      setSelectedId((current) => current ?? response.data[0]?.case_id ?? null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load SOC cases.');
    } finally {
      setLoading(false);
    }
  }, [api, currentTenantId]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    if (!selectedId || !currentTenantId) {
      setSelectedCase(null);
      setExportPreview(null);
      return;
    }
    let cancelled = false;
    setDetailLoading(true);
    setExportPreview(null);
    api
      .getSOCCase(selectedId, currentTenantId)
      .then((row) => {
        if (!cancelled) setSelectedCase(row);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load case detail.');
      })
      .finally(() => {
        if (!cancelled) setDetailLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [api, currentTenantId, selectedId]);

  const statusCounts = useMemo(() => summarizeCases(cases), [cases]);

  const addNote = async () => {
    if (!selectedCase || !currentTenantId || !noteDraft.trim()) return;
    setNoteStatus('Saving note...');
    try {
      const citations = selectedCase.evidence_refs?.map((ref) => ref.id).slice(0, 5);
      await api.addSOCCaseNote(selectedCase.case_id, currentTenantId, {
        note: noteDraft,
        citations,
      });
      setNoteDraft('');
      setNoteStatus('Note added with audit guardrails.');
      setSelectedCase(await api.getSOCCase(selectedCase.case_id, currentTenantId));
    } catch (err) {
      setNoteStatus(err instanceof Error ? err.message : 'Note failed.');
    }
  };

  const previewExport = async () => {
    if (!selectedCase || !currentTenantId) return;
    setExportPreview(await api.exportSOCCase(selectedCase.case_id, currentTenantId));
  };

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="SOC CASES"
        title="Cases"
        description={`${currentTenant?.name ?? 'Current tenant'} incident packets with timeline, evidence, notes, receipts, and export guardrails.`}
        actions={
          <div className="flex flex-wrap gap-2">
            <Button asChild variant="outline" size="sm">
              <Link to="/investigate">
                Investigate
                <ArrowRight />
              </Link>
            </Button>
            <Button type="button" variant="secondary" size="sm" onClick={() => void refresh()} loading={loading}>
              <RefreshCw />
              Refresh
            </Button>
          </div>
        }
      />

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <CaseMetric label="Open" value={statusCounts.open} tone={statusCounts.open > 0 ? 'warning' : 'healthy'} />
        <CaseMetric label="Investigating" value={statusCounts.investigating} tone="info" />
        <CaseMetric label="Export ready" value={statusCounts.exportReady} tone="healthy" />
        <CaseMetric label="Evidence gaps" value={statusCounts.evidenceGaps} tone={statusCounts.evidenceGaps > 0 ? 'degraded' : 'healthy'} />
      </div>

      {error ? (
        <Panel padding="md" toneAccent="critical" title="Case data unavailable">
          <p className="text-sm text-state-critical">{error}</p>
        </Panel>
      ) : null}

      <div className="grid grid-cols-1 gap-5 xl:grid-cols-[minmax(20rem,0.85fr)_minmax(0,1.4fr)]">
        <Panel padding="md" eyebrow="QUEUE" title="Incident packets">
          {loading ? (
            <p className="text-sm text-text-muted">Loading cases...</p>
          ) : cases.length > 0 ? (
            <div className="flex flex-col gap-2">
              {cases.map((row) => (
                <CaseQueueRow
                  key={row.case_id}
                  row={row}
                  active={row.case_id === selectedId}
                  onSelect={() => setSelectedId(row.case_id)}
                />
              ))}
            </div>
          ) : (
            <EmptyState
              icon={<ShieldCheck />}
              title="No SOC cases yet"
              description="Cases appear after AI investigations, alerts, posture gaps, or DB audit gaps are promoted into an incident packet."
            />
          )}
        </Panel>

        <Panel
          padding="md"
          eyebrow={selectedCase?.source?.toUpperCase() ?? 'DETAIL'}
          title={selectedCase?.title ?? 'Case detail'}
          toneAccent={selectedCase ? severityAccent(selectedCase.severity) : 'brand'}
          loading={detailLoading}
          actions={
            selectedCase ? (
              <div className="flex flex-wrap gap-2">
                <StatusTag tone={severityTone(selectedCase.severity)}>{selectedCase.severity}</StatusTag>
                <StatusTag tone={caseStatusTone(selectedCase.status)}>{selectedCase.status}</StatusTag>
              </div>
            ) : null
          }
        >
          {selectedCase ? (
            <div className="grid gap-5">
              <div className="rounded-md border border-border-subtle bg-surface p-3">
                <p className="text-sm leading-6 text-text-secondary">{caseSummaryText(selectedCase)}</p>
                <div className="mt-3 flex flex-wrap gap-1.5">
                  {selectedCase.coverage_badges.map((badge) => (
                    <StatusTag key={badge.id} tone={normalizeTone(badge.tone)}>
                      {badge.label}
                    </StatusTag>
                  ))}
                </div>
              </div>

              <div className="grid grid-cols-1 gap-4 lg:grid-cols-[1.2fr_0.8fr]">
                <CaseFactsPanel row={selectedCase} />
                <CaseResponsePanel row={selectedCase} onActionTaken={() => void refresh()} />
              </div>

              <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                <EvidencePanel refs={selectedCase.evidence_refs ?? []} citations={selectedCase.citations ?? []} />
                <TimelinePanel items={selectedCase.timeline} />
              </div>

              <div className="grid grid-cols-1 gap-4 lg:grid-cols-[1fr_20rem]">
                <NotesPanel
                  notes={selectedCase.notes ?? []}
                  draft={noteDraft}
                  status={noteStatus}
                  onDraftChange={setNoteDraft}
                  onSubmit={() => void addNote()}
                />
                <ExportPanel
                  row={selectedCase}
                  preview={exportPreview}
                  onPreview={() => void previewExport()}
                />
              </div>
            </div>
          ) : (
            <EmptyState
              icon={<ClipboardList />}
              title="Select a case"
              description="Open a case packet to inspect citations, notes, timeline, guardrails, and export readiness."
            />
          )}
        </Panel>
      </div>
    </div>
  );
}

function CaseMetric({ label, value, tone }: { label: string; value: number; tone: StateTone }): JSX.Element {
  return (
    <div className="rounded-lg border border-border-subtle bg-elevated p-4 shadow-[var(--shadow-panel)]">
      <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">{label}</p>
      <p className={cn('mt-2 font-mono text-2xl font-semibold tabular-nums', toneText(tone))}>{value}</p>
    </div>
  );
}

function CaseQueueRow({
  row,
  active,
  onSelect,
}: {
  row: SOCCase;
  active: boolean;
  onSelect: () => void;
}): JSX.Element {
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        'rounded-md border border-border-subtle bg-surface p-3 text-left transition hover:border-border-strong hover:bg-hover',
        active && 'border-brand-500/60 bg-brand-500/10',
      )}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate text-sm font-medium text-foreground">{row.title}</p>
          <p className="mt-1 line-clamp-2 text-xs leading-5 text-text-secondary">
            {caseSummaryText(row)}
          </p>
        </div>
        <StatusTag tone={severityTone(row.severity)}>{row.severity}</StatusTag>
      </div>
      <div className="mt-3 flex flex-wrap gap-1.5">
        <StatusTag tone={caseStatusTone(row.status)} variant="outline">{row.status}</StatusTag>
        <StatusTag tone={caseEvidenceCount(row) > 0 ? 'healthy' : 'warning'} variant="outline">
          {caseEvidenceCount(row)} refs
        </StatusTag>
        <StatusTag tone="info" variant="outline">{formatShortDate(row.updated_at)}</StatusTag>
      </div>
    </button>
  );
}

function CaseFactsPanel({ row }: { row: SOCCase }): JSX.Element {
  const facts = caseFacts(row);
  return (
    <div className="rounded-md border border-border-subtle bg-surface p-3">
      <p className="mb-3 flex items-center gap-2 text-sm font-medium text-foreground">
        <Fingerprint className="h-4 w-4 text-brand-400" />
        Case facts
      </p>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
        {facts.map((fact) => (
          <CaseFact key={fact.label} {...fact} />
        ))}
      </div>
    </div>
  );
}

function CaseFact({
  label,
  value,
  tone = 'info',
  to,
}: {
  label: string;
  value: string;
  tone?: StateTone;
  to?: string;
}): JSX.Element {
  const content = (
    <>
      <span className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">{label}</span>
      <span className="mt-1 break-all font-mono text-xs text-text-secondary">{value}</span>
      <StatusTag tone={tone} variant="outline" className="mt-2 w-fit">
        {to ? 'openable' : 'evidence'}
      </StatusTag>
    </>
  );
  const className = 'flex min-h-24 flex-col rounded-sm border border-border-subtle bg-elevated px-2.5 py-2';
  if (to) {
    return (
      <Link to={to} className={cn(className, 'transition hover:border-border-strong hover:bg-hover')}>
        {content}
      </Link>
    );
  }
  return <div className={className}>{content}</div>;
}

function CaseResponsePanel({
  row,
  onActionTaken,
}: {
  row: SOCCase;
  onActionTaken: () => void;
}): JSX.Element {
  const ip = caseSourceIP(row);
  return (
    <div className="rounded-md border border-border-subtle bg-surface p-3">
      <p className="mb-3 flex items-center gap-2 text-sm font-medium text-foreground">
        <Shield className="h-4 w-4 text-brand-400" />
        Response actions
      </p>
      {ip ? (
        <div className="flex flex-col gap-2">
          <IpActionMenu
            ip={ip}
            onActionTaken={onActionTaken}
            trigger={(
              <Button type="button" variant="danger" size="sm" className="w-full justify-between">
                Review IP block
                <Shield />
              </Button>
            )}
          />
          <Button asChild variant="outline" size="sm" className="w-full justify-between">
            <Link to={entityRoute('ip', ip)}>
              Open IP lifecycle
              <ExternalLink />
            </Link>
          </Button>
          {row.node_id ? (
            <Button asChild variant="outline" size="sm" className="w-full justify-between">
              <Link to={`/nodes/${row.node_id}`}>
                Open node
                <Server />
              </Link>
            </Button>
          ) : null}
          <Button asChild variant="ghost" size="sm" className="w-full justify-between">
            <Link to="/security/network?tab=blocks">
              Active block receipts
              <Network />
            </Link>
          </Button>
        </div>
      ) : (
        <EmptyState
          icon={<ShieldCheck />}
          title="No observed IP in case evidence"
          description="Use the linked evidence rows or node view before choosing a containment action."
        />
      )}
    </div>
  );
}

function EvidencePanel({
  refs,
  citations,
}: {
  refs: SOCCaseEvidenceRef[];
  citations: SOCCase['citations'];
}): JSX.Element {
  const citationRefs = (citations ?? [])
    .map((citation) => citation.source_record_id || citation.id || citation.detail || '')
    .filter(Boolean)
    .map((id) => ({ id, kind: 'source_row' }));
  const visibleRefs = refs.length > 0 ? refs : citationRefs;
  return (
    <div className="rounded-md border border-border-subtle bg-surface p-3">
      <p className="mb-3 flex items-center gap-2 text-sm font-medium text-foreground">
        <BookOpenText className="h-4 w-4 text-brand-400" />
        Evidence drawer
      </p>
      {visibleRefs.length > 0 ? (
        <div className="flex flex-col gap-2">
          {visibleRefs.map((ref) => (
            <div key={`${ref.kind}:${ref.id}`} className="rounded-sm border border-border-subtle bg-elevated px-2.5 py-2">
              <div className="flex items-center justify-between gap-2">
                <StatusTag tone="info" variant="outline">{ref.kind}</StatusTag>
                <span className="font-mono text-[0.65rem] text-text-muted">
                  {ref.kind === 'source_row' ? 'citation' : 'source row'}
                </span>
              </div>
              <p className="mt-2 break-all font-mono text-xs text-text-secondary">{ref.id}</p>
            </div>
          ))}
          {refs.length > 0 && citationRefs.length > 0 ? (
            <div className="rounded-sm border border-border-subtle bg-elevated px-2.5 py-2">
              <div className="mb-2 flex items-center gap-2">
                <Link2 className="h-3.5 w-3.5 text-brand-400" />
                <span className="text-xs font-medium text-foreground">Case citations</span>
              </div>
              <div className="flex flex-wrap gap-1.5">
                {citationRefs.map((ref) => (
                  <StatusTag key={ref.id} tone="healthy" variant="outline">{ref.id}</StatusTag>
                ))}
              </div>
            </div>
          ) : null}
        </div>
      ) : (
        <EmptyState
          icon={<AlertTriangle />}
          title="Evidence references missing"
          description="The case can stay open, but export should not be treated as fully evidenced."
        />
      )}
    </div>
  );
}

function TimelinePanel({ items }: { items: SOCCaseTimelineItem[] }): JSX.Element {
  return (
    <div className="rounded-md border border-border-subtle bg-surface p-3">
      <p className="mb-3 flex items-center gap-2 text-sm font-medium text-foreground">
        <FileText className="h-4 w-4 text-brand-400" />
        Timeline
      </p>
      {items.length > 0 ? (
        <ol className="relative flex flex-col gap-3 border-l border-border-subtle pl-4">
          {items.map((item, index) => (
            <li key={`${item.event}:${item.timestamp}:${item.citation_id}:${index}`} className="relative">
              <span className="absolute -left-[1.35rem] top-1.5 h-2.5 w-2.5 rounded-full bg-brand-400" />
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-sm font-medium text-foreground">{item.event}</span>
                <StatusTag tone="info" variant="outline">{item.source}</StatusTag>
              </div>
              <p className="mt-1 text-xs leading-5 text-text-secondary">{item.description}</p>
              <p className="mt-1 font-mono text-[0.65rem] text-text-muted">
                {formatShortDate(item.timestamp)} / {item.citation_id}
              </p>
            </li>
          ))}
        </ol>
      ) : (
        <EmptyState title="No timeline entries" />
      )}
    </div>
  );
}

function NotesPanel({
  notes,
  draft,
  status,
  onDraftChange,
  onSubmit,
}: {
  notes: SOCCase['notes'];
  draft: string;
  status: string | null;
  onDraftChange: (value: string) => void;
  onSubmit: () => void;
}): JSX.Element {
  return (
    <div className="rounded-md border border-border-subtle bg-surface p-3">
      <p className="mb-3 flex items-center gap-2 text-sm font-medium text-foreground">
        <MessageSquarePlus className="h-4 w-4 text-brand-400" />
        Analyst notes
      </p>
      <div className="flex flex-col gap-2">
        {(notes ?? []).map((note) => (
          <div key={note.id} className="rounded-sm border border-border-subtle bg-elevated px-2.5 py-2">
            <p className="text-sm text-text-secondary">{note.note}</p>
            <div className="mt-2 flex flex-wrap gap-1.5">
              <StatusTag tone="healthy" variant="outline">audit {note.audit_id.slice(0, 8)}</StatusTag>
              {note.guardrails.map((guardrail) => (
                <StatusTag key={guardrail} tone="info" variant="outline">{guardrail}</StatusTag>
              ))}
            </div>
          </div>
        ))}
        <textarea
          value={draft}
          onChange={(event) => onDraftChange(event.target.value)}
          rows={3}
          placeholder="Add analyst decision, owner, or closure note"
          className="rounded-md border border-border-subtle bg-elevated px-3 py-2 text-sm text-foreground focus:border-brand-500 focus:outline-none"
        />
        <Button type="button" variant="secondary" size="sm" onClick={onSubmit} disabled={!draft.trim()}>
          <MessageSquarePlus />
          Add note
        </Button>
        {status ? <p className="text-xs text-text-muted">{status}</p> : null}
      </div>
    </div>
  );
}

function ExportPanel({
  row,
  preview,
  onPreview,
}: {
  row: SOCCase;
  preview: SOCCaseExport | null;
  onPreview: () => void;
}): JSX.Element {
  return (
    <div className="rounded-md border border-border-subtle bg-surface p-3">
      <p className="mb-3 flex items-center gap-2 text-sm font-medium text-foreground">
        <Download className="h-4 w-4 text-brand-400" />
        Export packet
      </p>
      <div className="grid gap-2 text-xs">
        <Guardrail label="Tenant scoped" ok={row.tenant_id.length > 0} />
        <Guardrail label="Source-row citations" ok={(row.citations?.length ?? 0) > 0} />
        <Guardrail label="Evidence refs linked" ok={(row.evidence_refs?.length ?? 0) > 0} />
        <Guardrail label="Proposal-only actions" ok={row.coverage_badges.some((badge) => badge.id === 'actions_proposal_only')} />
        <Guardrail label="Audit export URL" ok={Boolean(row.export_url)} />
      </div>
      <Button type="button" variant="outline" size="sm" className="mt-3 w-full justify-between" onClick={onPreview}>
        Preview export
        <ArrowRight />
      </Button>
      {preview ? (
        <div className="mt-3 rounded-md border border-border-subtle bg-elevated p-3">
          <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">{preview.export_version}</p>
          <p className="mt-1 text-xs text-text-secondary">{preview.evidence.length} evidence refs / {preview.notes?.length ?? 0} notes</p>
          <div className="mt-2 flex flex-wrap gap-1.5">
            {preview.guardrails.map((guardrail) => (
              <StatusTag key={guardrail} tone="healthy" variant="outline">{guardrail}</StatusTag>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
}

function Guardrail({ label, ok }: { label: string; ok: boolean }): JSX.Element {
  return (
    <div className="flex items-center justify-between gap-3 rounded-sm bg-elevated px-2.5 py-2">
      <span className="text-text-secondary">{label}</span>
      {ok ? <CheckCircle2 className="h-4 w-4 text-state-healthy" /> : <AlertTriangle className="h-4 w-4 text-state-warning" />}
    </div>
  );
}

function summarizeCases(rows: SOCCase[]) {
  return rows.reduce(
    (acc, row) => {
      const status = row.status.toLowerCase();
      if (status === 'open') acc.open += 1;
      if (status === 'investigating' || status === 'in_progress') acc.investigating += 1;
      if (row.export_url) acc.exportReady += 1;
      if (caseEvidenceCount(row) === 0) acc.evidenceGaps += 1;
      return acc;
    },
    { open: 0, investigating: 0, exportReady: 0, evidenceGaps: 0 },
  );
}

function caseEvidenceCount(row: SOCCase): number {
  return (row.evidence_refs?.length ?? 0) || (row.citations?.length ?? 0);
}

export function caseSummaryText(row: Pick<SOCCase, 'title' | 'summary' | 'trigger_event_type' | 'dedup_key'>): string {
  const title = row.title.trim();
  const summary = (row.summary ?? '').trim();
  if (summary && summary.toLowerCase() !== title.toLowerCase()) return summary;
  return row.trigger_event_type || row.dedup_key || summary || title;
}

function caseFacts(row: SOCCase): Array<{ label: string; value: string; tone?: StateTone; to?: string }> {
  const facts: Array<{ label: string; value: string; tone?: StateTone; to?: string }> = [];
  const ip = caseSourceIP(row);
  addCaseFact(facts, 'Trigger', row.trigger_event_type || row.trigger_type, 'info');
  addCaseFact(facts, 'Source', row.source, 'info');
  addCaseFact(facts, 'Node', row.node_id, 'healthy', row.node_id ? `/nodes/${row.node_id}` : undefined);
  addCaseFact(facts, 'Observed IP', ip, 'critical', ip ? entityRoute('ip', ip) : undefined);
  addCaseFact(facts, 'App/process', caseAppOrProcess(row), 'healthy');
  addCaseFact(facts, 'Log/path', caseLogOrPath(row), 'warning');
  addCaseFact(facts, 'Confidence', caseConfidence(row), severityTone(row.severity));
  addCaseFact(facts, 'Dedup key', row.dedup_key, 'unknown');
  return facts.slice(0, 8);
}

function addCaseFact(
  facts: Array<{ label: string; value: string; tone?: StateTone; to?: string }>,
  label: string,
  value?: string | null,
  tone?: StateTone,
  to?: string,
) {
  const normalized = (value ?? '').trim();
  if (!normalized || normalized === '-') return;
  facts.push({ label, value: normalized, tone, to });
}

function caseSourceIP(row: SOCCase): string {
  return (
    evidenceString(row.evidence, ['src_ip', 'source_ip', 'remote_ip', 'client_ip', 'ip']) ||
    firstIPv4(row.dedup_key) ||
    firstIPv4(row.summary) ||
    ''
  );
}

function caseAppOrProcess(row: SOCCase): string {
  return evidenceString(row.evidence, ['application_name', 'app', 'vhost', 'process_name', 'process', 'service']) || '';
}

function caseLogOrPath(row: SOCCase): string {
  return evidenceString(row.evidence, ['source_file', 'log_file', 'raw_ref', 'path', 'request_path']) || '';
}

function caseConfidence(row: SOCCase): string {
  const value = evidenceString(row.evidence, ['confidence', 'threat_score', 'score']);
  if (!value) return '';
  const numeric = Number(value);
  if (Number.isFinite(numeric) && numeric >= 0 && numeric <= 100) return `${numeric}%`;
  return value;
}

function evidenceString(value: unknown, keys: string[], depth = 0): string {
  if (!value || depth > 5) return '';
  if (Array.isArray(value)) {
    for (const item of value) {
      const found = evidenceString(item, keys, depth + 1);
      if (found) return found;
    }
    return '';
  }
  if (typeof value !== 'object') return '';
  const record = value as Record<string, unknown>;
  for (const key of keys) {
    const raw = record[key];
    if (typeof raw === 'string' && raw.trim()) return raw.trim();
    if (typeof raw === 'number' && Number.isFinite(raw)) return String(raw);
  }
  for (const child of Object.values(record)) {
    const found = evidenceString(child, keys, depth + 1);
    if (found) return found;
  }
  return '';
}

function firstIPv4(value?: string): string {
  if (!value) return '';
  return value.match(/\b(?:\d{1,3}\.){3}\d{1,3}\b/)?.[0] ?? '';
}

function normalizeTone(tone?: string): StateTone {
  const normalized = (tone ?? '').toLowerCase();
  switch (normalized) {
    case 'healthy':
    case 'warning':
    case 'degraded':
    case 'critical':
    case 'info':
    case 'unknown':
      return normalized as StateTone;
    default:
      return 'unknown';
  }
}

function severityTone(severity?: string): StateTone {
  switch ((severity ?? '').toLowerCase()) {
    case 'critical':
      return 'critical';
    case 'high':
      return 'degraded';
    case 'medium':
      return 'warning';
    case 'low':
    case 'info':
      return 'info';
    default:
      return 'unknown';
  }
}

function caseStatusTone(status?: string): StateTone {
  switch ((status ?? '').toLowerCase()) {
    case 'closed':
    case 'resolved':
      return 'healthy';
    case 'open':
      return 'warning';
    case 'investigating':
    case 'in_progress':
      return 'info';
    default:
      return 'unknown';
  }
}

function severityAccent(severity?: string): 'brand' | 'accent' | 'healthy' | 'warning' | 'critical' {
  switch (severityTone(severity)) {
    case 'critical':
      return 'critical';
    case 'degraded':
    case 'warning':
      return 'warning';
    case 'healthy':
      return 'healthy';
    default:
      return 'brand';
  }
}

function toneText(tone: StateTone): string {
  switch (tone) {
    case 'critical':
      return 'text-state-critical';
    case 'warning':
    case 'degraded':
      return 'text-state-warning';
    case 'healthy':
      return 'text-state-healthy';
    case 'info':
      return 'text-state-info';
    default:
      return 'text-text-muted';
  }
}

function formatShortDate(value?: string): string {
  if (!value) return 'unknown';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date);
}

export default Cases;
