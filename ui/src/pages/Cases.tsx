import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import {
  AlertTriangle,
  AtSign,
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
  Search,
  Server,
  Shield,
  ShieldCheck,
  X,
} from 'lucide-react';
import type { ColumnDef, SortingState } from '@tanstack/react-table';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  DataTable,
  EmptyState,
  IpActionMenu,
  Pagination,
  Panel,
  SectionHeader,
  SelectField,
  StatusTag,
  type StateTone,
} from '@/components/kit';
import { useApiClient } from '@/hooks/useApiClient';
import { useTenant } from '@/providers/TenantProvider';
import { cn } from '@/lib/utils';
import type { SOCCase, SOCCaseExport, SOCCaseEvidenceRef, SOCCaseTimelineItem } from '@/lib/api';
import { entityRoute } from '@/lib/entity';
import { AssigneePicker } from '@/components/team/AssigneePicker';
import type { TeamUser } from '@/lib/api';

const CASE_SEVERITIES = [
  { label: 'Critical', value: 'critical' },
  { label: 'High', value: 'high' },
  { label: 'Medium', value: 'medium' },
  { label: 'Low', value: 'low' },
  { label: 'Info', value: 'info' },
];

export function Cases(): JSX.Element {
  const [searchParams] = useSearchParams();
  const requestedCaseId = searchParams.get('case_id');
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
  const [noteSaving, setNoteSaving] = useState(false);
  const [noteMentions, setNoteMentions] = useState<string[]>([]);
  const [teamUsers, setTeamUsers] = useState<TeamUser[]>([]);
  const [assignmentSaving, setAssignmentSaving] = useState(false);
  const [exportLoading, setExportLoading] = useState(false);
  const [exportError, setExportError] = useState<string | null>(null);
  const [statusFilter, setStatusFilter] = useState('');
  const [severityFilter, setSeverityFilter] = useState('');
  const [search, setSearch] = useState('');
  const [debouncedSearch, setDebouncedSearch] = useState('');
  const [sorting, setSorting] = useState<SortingState>([{ id: 'updated_at', desc: true }]);
  const [page, setPage] = useState(0);
  const [total, setTotal] = useState(0);
  const pageSize = 12;
  const searchTimer = useRef<number | null>(null);
  const requestSeq = useRef(0);

  useEffect(() => {
    if (searchTimer.current !== null) window.clearTimeout(searchTimer.current);
    searchTimer.current = window.setTimeout(() => {
      setDebouncedSearch(search.trim());
    }, 300);
    return () => {
      if (searchTimer.current !== null) window.clearTimeout(searchTimer.current);
    };
  }, [search]);

  useEffect(() => {
    setPage(0);
  }, [currentTenantId, statusFilter, severityFilter, debouncedSearch, sorting]);

  const refresh = useCallback(async () => {
    if (!currentTenantId) {
      setCases([]);
      setSelectedId(null);
      setSelectedCase(null);
      setExportPreview(null);
      setError(null);
      setNoteStatus(null);
      setLoading(false);
      return;
    }
    const seq = ++requestSeq.current;
    setLoading(true);
    setError(null);
    try {
      const response = await api.listSOCCases({
        tenantId: currentTenantId,
        limit: pageSize,
        offset: page * pageSize,
        status: statusFilter || undefined,
        severity: severityFilter || undefined,
        search: debouncedSearch || undefined,
        sortBy: sorting[0]?.id,
        sortOrder: sorting[0]?.desc ? 'desc' : 'asc',
      });
      if (seq !== requestSeq.current) return;
      const nextTotal = response.pagination?.total ?? response.data.length;
      setTotal(nextTotal);
      if (response.data.length === 0 && nextTotal > 0 && page > 0) {
        setPage(Math.max(0, Math.ceil(nextTotal / pageSize) - 1));
        return;
      }
      setCases(response.data);
      setSelectedId((current) => (
        current && (current === requestedCaseId || response.data.some((row) => row.case_id === current))
          ? current
          : requestedCaseId ?? response.data[0]?.case_id ?? null
      ));
    } catch (err) {
      if (seq !== requestSeq.current) return;
      setError(errorMessage(err, 'Failed to load SOC cases.'));
      setCases([]);
      setTotal(0);
      setSelectedId(null);
      setSelectedCase(null);
      setExportPreview(null);
      setNoteStatus(null);
    } finally {
      if (seq === requestSeq.current) setLoading(false);
    }
  }, [api, currentTenantId, requestedCaseId, page, statusFilter, severityFilter, debouncedSearch, sorting]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    let cancelled = false;
    if (!currentTenantId) {
      setTeamUsers([]);
      return;
    }
    api.getTeamUsers(currentTenantId).then((users) => {
      if (!cancelled) setTeamUsers(users);
    }).catch(() => {
      if (!cancelled) setTeamUsers([]);
    });
    return () => {
      cancelled = true;
    };
  }, [api, currentTenantId]);

  useEffect(() => {
    if (!selectedId || !currentTenantId) {
      setSelectedCase(null);
      setExportPreview(null);
      return;
    }
    let cancelled = false;
    setDetailLoading(true);
    setSelectedCase(null);
    setError(null);
    setExportPreview(null);
    setExportError(null);
    setNoteDraft('');
    setNoteMentions([]);
    setNoteStatus(null);
    api
      .getSOCCase(selectedId, currentTenantId)
      .then((row) => {
        if (!cancelled) setSelectedCase(row);
      })
      .catch((err) => {
        if (!cancelled) {
          setSelectedCase(null);
          setError(errorMessage(err, 'Failed to load case detail.'));
        }
      })
      .finally(() => {
        if (!cancelled) setDetailLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [api, currentTenantId, selectedId]);

  const columns = useMemo<ColumnDef<SOCCase>[]>(() => [
    {
      accessorKey: 'severity',
      header: 'Severity',
      cell: ({ row }) => (
        <StatusTag tone={severityTone(row.original.severity)} className="font-mono uppercase">
          {row.original.severity || 'unknown'}
        </StatusTag>
      ),
    },
    {
      accessorKey: 'title',
      header: 'Title',
      cell: ({ row }) => (
        <div className="flex flex-col gap-0.5">
          <span className="font-medium text-foreground">{row.original.title}</span>
          <span className="truncate text-xs text-text-muted">{caseSummaryText(row.original)}</span>
        </div>
      ),
    },
    {
      accessorKey: 'status',
      header: 'Status',
      cell: ({ getValue }) => (
        <StatusTag tone={caseStatusTone(getValue() as string)} variant="outline">
          {String(getValue())}
        </StatusTag>
      ),
    },
    {
      id: 'refs',
      header: 'Refs',
      enableSorting: false,
      cell: ({ row }) => (
        <span className="tabular-nums text-xs text-text-secondary">{caseEvidenceCount(row.original)}</span>
      ),
    },
    {
      id: 'notes',
      header: 'Notes',
      enableSorting: false,
      cell: ({ row }) => (
        <span className="tabular-nums text-xs text-text-secondary">{row.original.notes?.length ?? 0}</span>
      ),
    },
    {
      accessorKey: 'updated_at',
      header: 'Updated',
      cell: ({ getValue }) => (
        <span className="font-mono text-xs tabular-nums text-text-secondary">
          {timeAgo(getValue() as string)}
        </span>
      ),
    },
  ], []);

  const statusCounts = useMemo(() => summarizeCases(cases), [cases]);

  const addNote = async () => {
    if (!selectedCase || !currentTenantId || !noteDraft.trim() || noteSaving) return;
    setNoteStatus('Saving note...');
    setNoteSaving(true);
    try {
      const citations = selectedCase.evidence_refs?.map((ref) => ref.id).slice(0, 5);
      await api.addSOCCaseNote(selectedCase.case_id, currentTenantId, {
        note: noteDraft,
        citations,
        mentions: noteMentions,
      });
      setNoteDraft('');
      setNoteMentions([]);
      setNoteStatus('Note added with audit guardrails.');
      setSelectedCase(await api.getSOCCase(selectedCase.case_id, currentTenantId));
    } catch (err) {
      setNoteStatus(`Note failed: ${errorMessage(err, 'Unable to add note.')}`);
    } finally {
      setNoteSaving(false);
    }
  };

  const assignOwner = async (user: TeamUser | null) => {
    if (!selectedCase || !currentTenantId || assignmentSaving) return;
    setAssignmentSaving(true);
    setError(null);
    try {
      const updated = await api.assignSOCCase(selectedCase.case_id, currentTenantId, {
        assignee_id: user?.id ?? null,
      });
      setSelectedCase(updated);
      setCases((current) => current.map((row) => row.case_id === updated.case_id ? updated : row));
    } catch (err) {
      setError(errorMessage(err, 'Unable to update assignee.'));
    } finally {
      setAssignmentSaving(false);
    }
  };

  const previewExport = async () => {
    if (!selectedCase || !currentTenantId || exportLoading) return;
    setExportLoading(true);
    setExportError(null);
    try {
      setExportPreview(await api.exportSOCCase(selectedCase.case_id, currentTenantId));
    } catch (err) {
      setExportPreview(null);
      setExportError(`Export preview failed: ${errorMessage(err, 'Unable to generate export preview.')}`);
    } finally {
      setExportLoading(false);
    }
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
              <RefreshCw className={loading ? 'animate-spin' : ''} />
              {loading ? 'Refreshing…' : 'Refresh'}
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
          <p className="text-sm text-state-critical" role="alert">{error}</p>
        </Panel>
      ) : null}

      <div className="grid grid-cols-1 gap-5 xl:grid-cols-[minmax(20rem,0.85fr)_minmax(0,1.4fr)]">
        <Panel padding="md" eyebrow="QUEUE" title="Incident packets">
          <div className="flex flex-col gap-3">
            <div className="relative max-w-full">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-text-muted" />
              <Input
                id="case-search"
                type="search"
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder="Search title, summary, or trigger..."
                className="pl-9"
              />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <FilterSelect
                label="Status"
                value={statusFilter}
                onChange={(v) => setStatusFilter(v)}
                options={[
                  { label: 'All statuses', value: '' },
                  { label: 'Open', value: 'open' },
                  { label: 'Investigating', value: 'investigating' },
                  { label: 'Closed', value: 'closed' },
                ]}
              />
              <FilterSelect
                label="Severity"
                value={severityFilter}
                onChange={(v) => setSeverityFilter(v)}
                options={[
                  { label: 'All severities', value: '' },
                  ...CASE_SEVERITIES,
                ]}
              />
            </div>
          </div>
          <div className="mt-3">
            <DataTable
              columns={columns}
              rows={cases}
              rowKey={(row) => row.case_id}
              loading={loading}
              compact
              sorting={sorting}
              onSortingChange={setSorting}
              onRowClick={(row) => setSelectedId(row.case_id)}
              empty={
                error ? (
                  <EmptyState
                    icon={<ClipboardList />}
                    title="Case queue could not be loaded"
                    description="Resolve the error above and refresh."
                  />
                ) : (
                  <EmptyState
                    icon={<ShieldCheck />}
                    title="No SOC cases yet"
                    description="Cases appear after AI investigations, alerts, posture gaps, or DB audit gaps are promoted into an incident packet."
                  />
                )
              }
            />
            <Pagination
              page={page}
              pageSize={pageSize}
              total={total}
              onPageChange={setPage}
              className="mt-3"
            />
          </div>
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
                <div className="mt-3 flex flex-wrap items-center gap-2">
                  <AssigneePicker
                    value={selectedCase.assignee ?? null}
                    options={teamUsers}
                    disabled={assignmentSaving}
                    onChange={(user) => void assignOwner(user)}
                  />
                  {(selectedCase.mentioned_users ?? []).map((member) => (
                    <span
                      key={member.id}
                      title="Mentioned in case"
                      className="inline-flex items-center gap-1 rounded-full border border-border-subtle bg-elevated px-2 py-1 text-xs text-text-secondary"
                    >
                      <AtSign className="h-3 w-3 text-brand-400" aria-hidden />
                      {member.name}
                    </span>
                  ))}
                </div>
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
                  saving={noteSaving}
                  teamUsers={teamUsers}
                  mentions={noteMentions}
                  onDraftChange={setNoteDraft}
                  onMentionsChange={setNoteMentions}
                  onSubmit={() => void addNote()}
                />
                <ExportPanel
                  row={selectedCase}
                  preview={exportPreview}
                  loading={exportLoading}
                  error={exportError}
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

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

function CaseMetric({ label, value, tone }: { label: string; value: number; tone: StateTone }): JSX.Element {
  return (
    <div className="rounded-lg border border-border-subtle bg-elevated p-4 shadow-[var(--shadow-panel)]">
      <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">{label}</p>
      <p className={cn('mt-2 font-mono text-2xl font-semibold tabular-nums', toneText(tone))}>{value}</p>
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
}): JSX.Element {
  return (
    <SelectField label={label} value={value} onChange={(e) => onChange(e.target.value)}>
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </SelectField>
  );
}

function timeAgo(dateStr: string): string {
  const ms = Date.now() - new Date(dateStr).getTime();
  if (!Number.isFinite(ms) || ms < 0) return 'recently';
  const mins = Math.floor(ms / 60_000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
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
  saving,
  teamUsers,
  mentions,
  onDraftChange,
  onMentionsChange,
  onSubmit,
}: {
  notes: SOCCase['notes'];
  draft: string;
  status: string | null;
  saving: boolean;
  teamUsers: TeamUser[];
  mentions: string[];
  onDraftChange: (value: string) => void;
  onMentionsChange: (value: string[]) => void;
  onSubmit: () => void;
}): JSX.Element {
  const statusIsError = status?.toLowerCase().startsWith('note failed');
  const mentionQuery = draft.match(/(?:^|\s)@([^\s@]*)$/)?.[1]?.toLowerCase();
  const suggestions = mentionQuery === undefined || mentions.length >= 8
    ? []
    : teamUsers.filter((user) => (
      !mentions.includes(user.id)
      && `${user.name} ${user.email ?? ''}`.toLowerCase().includes(mentionQuery)
    )).slice(0, 5);
  const mentionedUsers = mentions
    .map((id) => teamUsers.find((user) => user.id === id))
    .filter((user): user is TeamUser => Boolean(user));

  const selectMention = (user: TeamUser) => {
    onMentionsChange([...mentions, user.id].slice(0, 8));
    onDraftChange(draft.replace(/(?:^|\s)@[^\s@]*$/, (match) => `${match.startsWith(' ') ? ' ' : ''}@${user.name} `));
  };
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
        {suggestions.length > 0 ? (
          <div className="rounded-md border border-border-subtle bg-elevated p-1" aria-label="Mention suggestions">
            {suggestions.map((user) => (
              <button
                key={user.id}
                type="button"
                aria-label={`Mention ${user.name}`}
                className="flex w-full items-center rounded px-2 py-1.5 text-left text-sm text-foreground hover:bg-hover"
                onClick={() => selectMention(user)}
              >
                {user.name}
              </button>
            ))}
          </div>
        ) : null}
        {mentionedUsers.length > 0 ? (
          <div className="flex flex-wrap gap-1.5" aria-label="Mention recipients">
            {mentionedUsers.map((user) => (
              <span key={user.id} className="inline-flex items-center gap-1 rounded-full border border-border-subtle bg-elevated px-2 py-1 text-xs text-text-secondary">
                <AtSign className="h-3 w-3" aria-hidden />
                {user.name}
                <button
                  type="button"
                  aria-label={`Remove ${user.name}`}
                  onClick={() => onMentionsChange(mentions.filter((id) => id !== user.id))}
                  className="rounded text-text-muted hover:text-foreground"
                >
                  <X className="h-3 w-3" aria-hidden />
                </button>
              </span>
            ))}
          </div>
        ) : null}
        {mentions.length >= 8 ? <p className="text-xs text-text-muted">Maximum 8 mentions.</p> : null}
        <Button type="button" variant="secondary" size="sm" onClick={onSubmit} disabled={!draft.trim() || saving} loading={saving}>
          <MessageSquarePlus />
          {saving ? 'Adding note...' : 'Add note'}
        </Button>
        {status ? (
          <p
            className={cn('text-xs', statusIsError ? 'text-state-critical' : 'text-text-muted')}
            role={statusIsError ? 'alert' : undefined}
          >
            {status}
          </p>
        ) : null}
      </div>
    </div>
  );
}

function ExportPanel({
  row,
  preview,
  loading,
  error,
  onPreview,
}: {
  row: SOCCase;
  preview: SOCCaseExport | null;
  loading: boolean;
  error: string | null;
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
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="mt-3 w-full justify-between"
        onClick={onPreview}
        loading={loading}
        disabled={loading}
      >
        {loading ? 'Previewing export...' : 'Preview export'}
        <ArrowRight />
      </Button>
      {error ? <p className="mt-2 text-xs text-state-critical" role="alert">{error}</p> : null}
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
