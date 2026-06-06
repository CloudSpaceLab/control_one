import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  AlertTriangle,
  ArrowRight,
  BookOpenText,
  CheckCircle2,
  Clipboard,
  Copy,
  KeyRound,
  Network,
  Play,
  ShieldCheck,
  Terminal,
  TimerReset,
  Wrench,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet';
import {
  KpiTile,
  Panel,
  SectionHeader,
  StatusTag,
  type StateTone,
} from '@/components/kit';
import { useApiClient } from '@/hooks/useApiClient';
import { useCoverageMatrix } from '@/hooks/useCoverageMatrix';
import { useNodes } from '@/hooks/useNodes';
import type {
  ContentPackSourceHealth,
  CoverageMatrixRow,
  NodeSummary,
  WebserverInstance,
} from '@/lib/api';
import { cn } from '@/lib/utils';
import { useTenant } from '@/providers/TenantProvider';

type ObservabilityState =
  | 'healthy'
  | 'partial'
  | 'needs_access'
  | 'fallback_active'
  | 'detected_only'
  | 'raw_only'
  | 'unsupported'
  | 'stale'
  | 'failed';

interface ObservabilityService {
  id: string;
  name: string;
  kind: string;
  state: ObservabilityState;
  evidence: string[];
  missing: string[];
  why: string;
  nextAction: string;
  cta: string;
  setup: string[];
  verification: string[];
  snippet?: string;
  href?: string;
}

interface ActionItem {
  id: string;
  title: string;
  impact: string;
  serviceId: string;
  effort: string;
  risk: StateTone;
}

interface KnowledgeChunk {
  id: string;
  source: string;
  topic: string;
  state: 'fresh' | 'stale' | 'failed';
  summary: string;
  citations: string[];
  openedFrom: string[];
}

const REFERENCE_SERVICES: ObservabilityService[] = [
  {
    id: 'svc-nginx',
    name: 'nginx edge',
    kind: 'webserver',
    state: 'healthy',
    evidence: ['access log parsed', 'error log parsed', 'vhost inventory'],
    missing: [],
    why: 'HTTP evidence is ready for timelines, source-row citations, and webserver control receipts.',
    nextAction: 'Keep parser version and retention visible in investigations.',
    cta: 'Open webserver controls',
    setup: ['Confirm access log path', 'Confirm error log path', 'Verify parser version'],
    verification: ['logs parsed', 'source rows cited', 'receipt path linked'],
  },
  {
    id: 'svc-fastapi',
    name: 'Example FastAPI service',
    kind: 'app framework',
    state: 'partial',
    evidence: ['application logs', 'request IDs'],
    missing: ['native middleware', 'stack traces'],
    why: 'AI can cite request logs, but incident timelines lose stack traces and handler spans.',
    nextAction: 'Add middleware instrumentation and verify request correlation.',
    cta: 'Copy middleware snippet',
    setup: ['Install package', 'Register middleware', 'Deploy one canary instance'],
    verification: ['trace ID appears', 'error span appears', 'fallback state clears'],
    snippet: 'pip install controlone-fastapi\napp.add_middleware(ControlOneMiddleware, redact="strict")',
  },
  {
    id: 'svc-postgres',
    name: 'PostgreSQL core',
    kind: 'DBMS',
    state: 'needs_access',
    evidence: ['port 5432', 'postgres process', 'app connection fingerprint'],
    missing: ['audit logs', 'slow query logs', 'role metadata'],
    why: 'Investigations cannot prove database-level changes without audit evidence.',
    nextAction: 'Create a read-only audit user and test access without storing raw secrets.',
    cta: 'Copy SQL grant',
    setup: ['Create read-only audit role', 'Store credential reference', 'Run access test'],
    verification: ['audit source reachable', 'role metadata visible', 'query evidence cited'],
    snippet:
      'CREATE ROLE controlone_audit LOGIN;\nGRANT pg_read_all_stats TO controlone_audit;\nGRANT SELECT ON pg_catalog.pg_authid TO controlone_audit;',
  },
  {
    id: 'svc-redis',
    name: 'Redis cache',
    kind: 'cache',
    state: 'detected_only',
    evidence: ['process inventory', 'port 6379'],
    missing: ['slowlog collection', 'parser pack'],
    why: 'Latency incidents will be inferred from side channels until slowlog evidence is enabled.',
    nextAction: 'Enable slowlog collection or mark Redis not applicable for this tenant.',
    cta: 'Show slowlog command',
    setup: ['Enable slowlog threshold', 'Register log path', 'Verify parser pack'],
    verification: ['slowlog event appears', 'parser state normalized', 'AI citation uses Redis source'],
    snippet: 'CONFIG SET slowlog-log-slower-than 10000\nSLOWLOG GET 128',
  },
  {
    id: 'svc-celery',
    name: 'Celery worker',
    kind: 'worker',
    state: 'raw_only',
    evidence: ['raw log path'],
    missing: ['typed parser', 'task correlation'],
    why: 'Worker failures can be searched, but Control One cannot yet group task IDs into cases.',
    nextAction: 'Attach parser pack or route raw-only status into the investigation limitation.',
    cta: 'Open parser gap',
    setup: ['Confirm log format', 'Map task ID field', 'Register parser version'],
    verification: ['task ID normalized', 'retry count visible', 'case evidence linked'],
  },
  {
    id: 'svc-legacy',
    name: 'Legacy SOAP gateway',
    kind: 'custom app',
    state: 'unsupported',
    evidence: ['node inventory'],
    missing: ['supported parser', 'instrumentation package'],
    why: 'Unsupported sources must stay visible but cannot count as healthy coverage.',
    nextAction: 'Request a custom connector contract from ai-logfixer or mark not applicable.',
    cta: 'Open adapter tracker',
    setup: ['Capture sample transcript', 'Define redaction expectations', 'Create fixture contract'],
    verification: ['fixture passes contract', 'state no longer unsupported', 'operator copy updated'],
  },
];

const REFERENCE_ACTIONS: ActionItem[] = [
  {
    id: 'postgres-audit',
    title: 'Create PostgreSQL read-only audit user',
    impact: 'Unlocks DB-level AI citations, compliance evidence, and case exports.',
    serviceId: 'svc-postgres',
    effort: 'Medium',
    risk: 'warning',
  },
  {
    id: 'fastapi-middleware',
    title: 'Add FastAPI instrumentation middleware',
    impact: 'Adds stack traces and request-span evidence to incident timelines.',
    serviceId: 'svc-fastapi',
    effort: 'Low',
    risk: 'info',
  },
  {
    id: 'redis-slowlog',
    title: 'Enable Redis slowlog collection',
    impact: 'Turns cache latency from inferred side-channel signal into cited evidence.',
    serviceId: 'svc-redis',
    effort: 'Low',
    risk: 'info',
  },
  {
    id: 'legacy-contract',
    title: 'Create custom connector fixture',
    impact: 'Moves the SOAP gateway from unsupported to contract-reviewed.',
    serviceId: 'svc-legacy',
    effort: 'High',
    risk: 'degraded',
  },
];

const REFERENCE_KNOWLEDGE_CHUNKS: KnowledgeChunk[] = [
  {
    id: 'kt-postgres-audit-001',
    source: 'PostgreSQL core',
    topic: 'DB audit gap',
    state: 'fresh',
    summary: 'Port and process evidence confirm PostgreSQL, but audit log access is missing.',
    citations: ['coverage:db_audit:postgres', 'db_audit_discovery:postgres-core'],
    openedFrom: ['Ask AI', 'Timeline', 'Case'],
  },
  {
    id: 'kt-fastapi-trace-007',
    source: 'Example FastAPI service',
    topic: 'Instrumentation fallback',
    state: 'stale',
    summary: 'Application logs are present; middleware verification has not refreshed after the latest deploy.',
    citations: ['events:fastapi:error-rate', 'coverage:parser:fastapi'],
    openedFrom: ['Ask AI', 'Timeline'],
  },
  {
    id: 'kt-celery-parser-003',
    source: 'Celery worker',
    topic: 'Raw-only parser state',
    state: 'failed',
    summary: 'Chunk job retained the raw source but skipped summary generation because the sample was low signal.',
    citations: ['raw_logs:celery:task-retry', 'knowledge_job:celery-parser'],
    openedFrom: ['Case'],
  },
];

const STATE_META: Record<ObservabilityState, { label: string; tone: StateTone; plain: string }> = {
  healthy: {
    label: 'Healthy',
    tone: 'healthy',
    plain: 'Control One can cite this source in investigations.',
  },
  partial: {
    label: 'Partial',
    tone: 'warning',
    plain: 'Some evidence is usable, but a stronger signal is missing.',
  },
  needs_access: {
    label: 'Needs access',
    tone: 'warning',
    plain: 'The service was found, but audit access is missing.',
  },
  fallback_active: {
    label: 'Fallback active',
    tone: 'degraded',
    plain: 'Control One is using lower-confidence evidence.',
  },
  detected_only: {
    label: 'Detected only',
    tone: 'info',
    plain: 'Inventory found the service before telemetry was enabled.',
  },
  raw_only: {
    label: 'Raw only',
    tone: 'degraded',
    plain: 'Raw logs exist but typed parser coverage is missing.',
  },
  unsupported: {
    label: 'Unsupported',
    tone: 'critical',
    plain: 'This cannot count as healthy coverage.',
  },
  stale: {
    label: 'Stale',
    tone: 'degraded',
    plain: 'The last verification is outside the freshness window.',
  },
  failed: {
    label: 'Failed',
    tone: 'critical',
    plain: 'The last verification job failed.',
  },
};

const CHUNK_TONE: Record<KnowledgeChunk['state'], StateTone> = {
  fresh: 'healthy',
  stale: 'degraded',
  failed: 'critical',
};

export function Observability(): JSX.Element {
  const api = useApiClient();
  const { currentTenantId, currentTenant } = useTenant();
  const [selectedId, setSelectedId] = useState<string>('');
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const [selectedChunkId, setSelectedChunkId] = useState<string>('');
  const [liveState, setLiveState] = useState<{
    webservers: WebserverInstance[];
    sourceHealth: ContentPackSourceHealth[];
    loading: boolean;
    error: string | null;
  }>({
    webservers: [],
    sourceHealth: [],
    loading: false,
    error: null,
  });
  const [debug, setDebug] = useState({
    scope: '',
    ttl: '',
    reason: '',
    redaction: 'strict',
    quota: '',
    rollback: '',
    approval: false,
  });

  const tenantId = currentTenantId ?? undefined;
  const tenantLabel = currentTenant?.name ?? 'Current tenant';
  const {
    data: nodes,
    loading: nodesLoading,
    error: nodesError,
  } = useNodes({ tenantId, limit: 200, offset: 0 });
  const coverage = useCoverageMatrix({ tenantId, enabled: Boolean(tenantId) });

  useEffect(() => {
    if (!tenantId) {
      setLiveState({ webservers: [], sourceHealth: [], loading: false, error: null });
      return;
    }

    let cancelled = false;
    setLiveState((current) => ({ ...current, loading: true, error: null }));

    Promise.allSettled([
      api.listWebserverInstances({ tenantId, limit: 100 }),
      api.getContentPackSourceHealth(tenantId, { limit: 100 }),
    ]).then(([webserverResult, sourceHealthResult]) => {
      if (cancelled) return;

      const errors: string[] = [];
      const webservers =
        webserverResult.status === 'fulfilled'
          ? webserverResult.value.data
          : [];
      const sourceHealth =
        sourceHealthResult.status === 'fulfilled'
          ? sourceHealthResult.value.items
          : [];

      if (webserverResult.status === 'rejected') {
        errors.push(errorMessage(webserverResult.reason, 'webserver inventory unavailable'));
      }
      if (sourceHealthResult.status === 'rejected') {
        errors.push(errorMessage(sourceHealthResult.reason, 'source health unavailable'));
      }

      setLiveState({
        webservers,
        sourceHealth,
        loading: false,
        error: errors.length ? errors.join('; ') : null,
      });
    });

    return () => {
      cancelled = true;
    };
  }, [api, tenantId]);

  const liveServices = useMemo(
    () =>
      buildLiveObservabilityServices({
        nodes,
        webservers: liveState.webservers,
        sourceHealth: liveState.sourceHealth,
        coverageRows: coverage.data?.rows ?? [],
      }),
    [coverage.data?.rows, liveState.sourceHealth, liveState.webservers, nodes],
  );
  const referenceMode = liveServices.length === 0;
  const services = referenceMode ? REFERENCE_SERVICES : liveServices;
  const selected = services.find((service) => service.id === selectedId) ?? services[0];
  const actions = useMemo(
    () => (referenceMode ? REFERENCE_ACTIONS : deriveActions(services)),
    [referenceMode, services],
  );
  const knowledgeChunks = useMemo(
    () =>
      referenceMode
        ? REFERENCE_KNOWLEDGE_CHUNKS
        : deriveKnowledgeChunks(services, coverage.data?.rows ?? []),
    [coverage.data?.rows, referenceMode, services],
  );
  const selectedChunk =
    knowledgeChunks.find((chunk) => chunk.id === selectedChunkId) ?? knowledgeChunks[0];
  const summary = useMemo(() => summarizeServices(services), [services]);
  const dbService =
    services.find((service) => /db|postgres|mysql|mssql|database/i.test(`${service.kind} ${service.name}`)) ??
    services.find((service) => service.state === 'needs_access') ??
    selected;
  const loading = nodesLoading || liveState.loading || coverage.loading;
  const loadErrors = [nodesError, liveState.error, coverage.error].filter(Boolean);
  const debugReady = Boolean(
    debug.scope.trim() &&
      Number(debug.ttl) > 0 &&
      debug.reason.trim() &&
      debug.redaction &&
      debug.quota.trim() &&
      debug.rollback.trim() &&
      debug.approval,
  );

  useEffect(() => {
    if (services.length === 0) return;
    if (!selectedId || !services.some((service) => service.id === selectedId)) {
      setSelectedId(preferredServiceId(services));
    }
  }, [selectedId, services]);

  useEffect(() => {
    if (knowledgeChunks.length === 0) return;
    if (!selectedChunkId || !knowledgeChunks.some((chunk) => chunk.id === selectedChunkId)) {
      setSelectedChunkId(knowledgeChunks[0].id);
    }
  }, [knowledgeChunks, selectedChunkId]);

  const copySnippet = async (service: ObservabilityService) => {
    if (!service.snippet) return;
    await navigator.clipboard?.writeText(service.snippet);
    setCopiedId(service.id);
  };

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="OBSERVABILITY"
        title="Guided setup"
        description={`${tenantLabel} connector, instrumentation, debug, and knowledge states translated into operator decisions.`}
        actions={
          <div className="flex flex-wrap gap-2">
            <StatusTag tone={referenceMode ? 'warning' : 'healthy'}>
              {referenceMode ? 'reference' : 'live data'}
            </StatusTag>
            {loading ? <StatusTag tone="info">loading</StatusTag> : null}
            <Button asChild variant="outline" size="sm">
              <Link to="/coverage?domain=db_audit">
                Coverage
                <ArrowRight />
              </Link>
            </Button>
            <DebugSessionSheet
              debug={debug}
              ready={debugReady}
              onChange={(field, value) => setDebug((current) => ({ ...current, [field]: value }))}
            />
          </div>
        }
      />

      {loadErrors.length > 0 ? (
        <Panel padding="sm" toneAccent="warning">
          <p className="text-sm text-text-secondary" role="alert">
            Some live observability signals are partial: {loadErrors.join('; ')}
          </p>
        </Panel>
      ) : null}

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
        <KpiTile label="Detected services" value={summary.total.toString()} tone="info" icon={<Network />} />
        <KpiTile label="Healthy" value={summary.healthy.toString()} tone="healthy" icon={<ShieldCheck />} />
        <KpiTile label="Partial" value={summary.partial.toString()} tone="warning" icon={<AlertTriangle />} />
        <KpiTile label="Needs access" value={summary.needsAccess.toString()} tone="degraded" icon={<KeyRound />} />
        <KpiTile label="Unsupported" value={summary.unsupported.toString()} tone="critical" icon={<Terminal />} />
      </div>

      <div className="grid grid-cols-1 gap-5 xl:grid-cols-[minmax(0,1fr)_24rem]">
        <Panel padding="md" eyebrow="STACK MAP" title={referenceMode ? 'Reference blueprint' : `${tenantLabel} live stack`}>
          <div className="overflow-x-auto rounded-lg border border-border-subtle">
            <table className="w-full min-w-[640px] table-fixed text-sm xl:min-w-0">
              <colgroup>
                <col className="w-[22%]" />
                <col className="w-[17%]" />
                <col className="w-[28%]" />
                <col className="w-[23%]" />
                <col className="w-[10%]" />
              </colgroup>
              <thead className="bg-surface-2 text-left text-xs uppercase tracking-wide text-text-secondary">
                <tr>
                  <th className="px-3 py-2">Service</th>
                  <th className="px-3 py-2">State</th>
                  <th className="px-3 py-2">Evidence</th>
                  <th className="px-3 py-2">Next action</th>
                  <th className="px-3 py-2 text-right">Open</th>
                </tr>
              </thead>
              <tbody>
                {services.map((service) => {
                  const meta = STATE_META[service.state];
                  return (
                    <tr key={service.id} className="border-t border-border-subtle">
                      <td className="min-w-0 px-3 py-3 align-top">
                        <div className="break-words font-medium text-foreground">{service.name}</div>
                        <div className="break-words text-xs text-text-muted">{service.kind}</div>
                      </td>
                      <td className="min-w-0 px-3 py-3 align-top">
                        <StatusTag tone={meta.tone}>{meta.label}</StatusTag>
                        <p className="mt-1 text-xs leading-5 text-text-muted">{meta.plain}</p>
                      </td>
                      <td className="min-w-0 px-3 py-3 align-top text-xs text-text-secondary">
                        <span className="block break-words leading-5">{service.evidence.join(', ') || 'none'}</span>
                      </td>
                      <td className="min-w-0 px-3 py-3 align-top text-xs text-text-secondary">
                        <span className="block break-words leading-5">{service.nextAction}</span>
                      </td>
                      <td className="px-3 py-3 text-right align-top">
                        <Button
                          type="button"
                          variant={selected.id === service.id ? 'secondary' : 'ghost'}
                          size="sm"
                          onClick={() => setSelectedId(service.id)}
                        >
                          Detail
                        </Button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </Panel>

        <Panel
          padding="md"
          eyebrow={selected.kind.toUpperCase()}
          title={selected.name}
          toneAccent={STATE_META[selected.state].tone === 'critical' ? 'critical' : 'warning'}
          actions={<StatusTag tone={STATE_META[selected.state].tone}>{STATE_META[selected.state].label}</StatusTag>}
        >
          <p className="text-sm leading-6 text-text-secondary">{selected.why}</p>
          <ConnectorFacts title="Detected from" values={selected.evidence} icon={<CheckCircle2 />} />
          <ConnectorFacts title="Missing data" values={selected.missing} icon={<AlertTriangle />} empty="No missing evidence" />
          <ConnectorFacts title="Setup steps" values={selected.setup} icon={<Wrench />} />
          <ConnectorFacts title="Verification" values={selected.verification} icon={<Clipboard />} />
          {selected.snippet ? (
            <div className="rounded-md border border-border-subtle bg-surface p-3">
              <div className="mb-2 flex items-center justify-between gap-3">
                <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                  {selected.cta}
                </p>
                <Button type="button" variant="outline" size="sm" onClick={() => void copySnippet(selected)}>
                  <Copy />
                  {copiedId === selected.id ? 'Copied' : 'Copy'}
                </Button>
              </div>
              <pre className="overflow-x-auto whitespace-pre-wrap font-mono text-xs leading-5 text-text-secondary">
                {selected.snippet}
              </pre>
            </div>
          ) : null}
          {!selected.snippet && selected.href ? (
            <Button asChild variant="outline" size="sm">
              <Link to={selected.href}>
                {selected.cta}
                <ArrowRight />
              </Link>
            </Button>
          ) : null}
        </Panel>
      </div>

      <div className="grid grid-cols-1 gap-5 xl:grid-cols-[minmax(0,1fr)_26rem]">
        <Panel padding="md" eyebrow="NEXT ACTIONS" title="Ranked setup queue">
          <div className="grid gap-3">
            {actions.map((action, index) => (
              <ActionRow
                key={action.id}
                action={action}
                rank={index + 1}
                active={selected.id === action.serviceId}
                onOpen={() => setSelectedId(action.serviceId)}
              />
            ))}
          </div>
        </Panel>

        <Panel padding="md" eyebrow="DBMS ONBOARDING" title="Least-privilege path">
          <div className="grid gap-3">
            {dbOnboardingSteps(dbService).map(([label, detail], index) => (
              <div key={label} className="grid grid-cols-[auto_1fr] gap-3 rounded-md border border-border-subtle bg-surface p-3">
                <span className="grid h-7 w-7 place-items-center rounded-md bg-brand-500/15 font-mono text-xs text-brand-400">
                  {index + 1}
                </span>
                <span>
                  <span className="block text-sm font-medium text-foreground">{label}</span>
                  <span className="block text-xs leading-5 text-text-secondary">{detail}</span>
                </span>
              </div>
            ))}
          </div>
        </Panel>
      </div>

      <div className="grid grid-cols-1 gap-5 xl:grid-cols-2">
        <Panel padding="md" eyebrow="INSTRUMENTATION" title="Framework setup cards">
          <div className="grid gap-3">
            {setupCardServices(services).map((service) => (
              <div key={service.id} className="rounded-md border border-border-subtle bg-surface p-3">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div>
                    <p className="text-sm font-medium text-foreground">{service.name}</p>
                    <p className="text-xs text-text-muted">{service.nextAction}</p>
                  </div>
                  <StatusTag tone={STATE_META[service.state].tone}>{STATE_META[service.state].label}</StatusTag>
                </div>
                <div className="mt-3 flex flex-wrap gap-2">
                  <Button type="button" variant="outline" size="sm" onClick={() => setSelectedId(service.id)}>
                    Setup
                  </Button>
                  <Button type="button" variant="ghost" size="sm" onClick={() => setSelectedId(service.id)}>
                    Verify
                  </Button>
                </div>
              </div>
            ))}
          </div>
        </Panel>

        <Panel padding="md" eyebrow="KNOWLEDGE TREE" title="Citations and vault chunks">
          <div className="grid gap-3 lg:grid-cols-[15rem_1fr]">
            <div className="flex flex-col gap-2">
              {knowledgeChunks.map((chunk) => (
                <button
                  key={chunk.id}
                  type="button"
                  onClick={() => setSelectedChunkId(chunk.id)}
                  className={cn(
                    'rounded-md border border-border-subtle bg-surface p-3 text-left transition hover:border-border-strong hover:bg-hover',
                    selectedChunk.id === chunk.id && 'border-brand-500/60 bg-brand-500/10',
                  )}
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="truncate text-sm font-medium text-foreground">{chunk.topic}</span>
                    <StatusTag tone={CHUNK_TONE[chunk.state]}>{chunk.state}</StatusTag>
                  </div>
                  <p className="mt-1 truncate text-xs text-text-muted">{chunk.source}</p>
                </button>
              ))}
            </div>
            <div className="rounded-md border border-border-subtle bg-surface p-3">
              <div className="flex flex-wrap items-center gap-2">
                <StatusTag tone={CHUNK_TONE[selectedChunk.state]}>{selectedChunk.state}</StatusTag>
                {selectedChunk.openedFrom.map((source) => (
                  <StatusTag key={source} tone="info" variant="outline">
                    {source}
                  </StatusTag>
                ))}
              </div>
              <p className="mt-3 text-sm leading-6 text-text-secondary">{selectedChunk.summary}</p>
              <div className="mt-3 flex flex-wrap gap-2">
                {selectedChunk.citations.map((citation) => (
                  <span
                    key={citation}
                    className="inline-flex max-w-full items-center gap-1 rounded-sm border border-border-subtle bg-elevated px-2 py-1 font-mono text-[0.65rem] text-text-secondary"
                  >
                    <BookOpenText className="h-3.5 w-3.5 shrink-0" />
                    <span className="truncate">{citation}</span>
                  </span>
                ))}
              </div>
              <Button asChild variant="outline" size="sm" className="mt-4">
                <Link to="/ask">
                  Open in Ask AI
                  <ArrowRight />
                </Link>
              </Button>
            </div>
          </div>
        </Panel>
      </div>
    </div>
  );
}

function buildLiveObservabilityServices({
  nodes,
  webservers,
  sourceHealth,
  coverageRows,
}: {
  nodes: NodeSummary[];
  webservers: WebserverInstance[];
  sourceHealth: ContentPackSourceHealth[];
  coverageRows: CoverageMatrixRow[];
}): ObservabilityService[] {
  const services: ObservabilityService[] = [];

  services.push(...webservers.slice(0, 8).map(serviceFromWebserver));
  services.push(...sourceHealth.slice(0, 10).map(serviceFromSourceHealth));

  const attentionRows = coverageRows
    .filter((row) => isAttentionCoverageState(row.coverage_state ?? row.state))
    .slice(0, 8)
    .map(serviceFromCoverageRow);
  services.push(...attentionRows);

  const nodeRows = nodes
    .slice(0, 6)
    .map(serviceFromNode)
    .filter((service) => !services.some((candidate) => candidate.id === service.id));
  services.push(...nodeRows);

  return dedupeServices(services).slice(0, 24);
}

function serviceFromWebserver(instance: WebserverInstance): ObservabilityService {
  const name = webserverDisplayName(instance);
  const vhostCount = Array.isArray(instance.VHosts) ? instance.VHosts.length : 0;
  const hasAccess = Boolean(instance.AccessLogPath);
  const hasError = Boolean(instance.ErrorLogPath);
  const state: ObservabilityState = hasAccess && hasError ? 'healthy' : 'partial';

  return {
    id: `webserver:${instance.ID}`,
    name,
    kind: 'webserver',
    state,
    evidence: compact([
      versionEvidence(instance.Version),
      instance.ConfigPath,
      hasAccess ? 'access log path' : '',
      hasError ? 'error log path' : '',
      vhostCount ? `${vhostCount} vhosts` : '',
      instance.ObservedAt ? `observed ${formatDateLabel(instance.ObservedAt)}` : '',
    ]),
    missing: compact([!hasAccess ? 'access log path' : '', !hasError ? 'error log path' : '']),
    why:
      state === 'healthy'
        ? 'Webserver inventory includes config and log paths that can back investigations and receipts.'
        : 'Webserver inventory exists, but capture evidence is not complete enough for full citation coverage.',
    nextAction:
      state === 'healthy'
        ? 'Keep parser version, vhost, and retention evidence fresh.'
        : 'Run capture setup for missing webserver log paths.',
    cta: 'Open webserver controls',
    setup: ['Review discovered config', 'Confirm managed capture policy', 'Keep parser evidence fresh'],
    verification: ['inventory current', 'log path cited', 'receipt path linked'],
    href: '/security/webservers',
  };
}

function webserverDisplayName(instance: WebserverInstance): string {
  const kind = instance.Kind?.trim();
  const serviceName = instance.ServiceName?.trim();
  if (kind && serviceName && kind.toLowerCase() !== serviceName.toLowerCase()) {
    return `${kind} ${serviceName}`;
  }
  return kind || serviceName || 'webserver';
}

function versionEvidence(version: string | undefined): string {
  const clean = version?.trim();
  if (!clean) return '';
  return /\bversion\b/i.test(clean) ? clean : `version ${clean}`;
}

function serviceFromSourceHealth(item: ContentPackSourceHealth): ObservabilityService {
  const state = stateFromSourceHealth(item.coverage_state);
  const name = item.display_name || item.source_id || item.source_instance_id || 'source health';
  const metrics = item.metrics ?? {};
  const evidence = compact([
    item.collector_id ? `collector ${item.collector_id}` : '',
    item.parser_id ? `parser ${item.parser_id}` : '',
    item.collector_mode ? `mode ${item.collector_mode}` : '',
    typeof metrics.events_received === 'number' ? `${metrics.events_received} events` : '',
    typeof metrics.events_parsed === 'number' ? `${metrics.events_parsed} parsed` : '',
    item.last_event_at ? `last event ${formatDateLabel(item.last_event_at)}` : '',
  ]);
  const missing = compact([
    item.coverage_state && state !== 'healthy' ? item.coverage_state : '',
    item.last_error,
    item.approval_required ? 'approval required' : '',
  ]);

  return {
    id: `source-health:${item.runtime_state_id || item.source_instance_id || item.source_id}`,
    name,
    kind: item.parser_id ? 'parser source' : 'telemetry source',
    state,
    evidence,
    missing,
    why:
      state === 'healthy'
        ? 'Source runtime health is reporting enough evidence to cite this source.'
        : 'Source runtime health needs attention before it can count as complete observability coverage.',
    nextAction: firstRecommendedAction(item) ?? actionForState(state, name),
    cta: 'Open SIEM source health',
    setup: ['Review source identity', 'Check parser/runtime state', 'Confirm approval and retention labels'],
    verification: ['events received', 'parser state current', 'source health cited'],
    href: '/security/siem',
  };
}

function serviceFromCoverageRow(row: CoverageMatrixRow): ObservabilityService {
  const state = stateFromCoverage(row.coverage_state ?? row.state);
  const title = row.title || row.name || row.subject || row.domain || 'coverage row';
  const domain = String(row.domain || 'coverage').replaceAll('_', ' ');
  const evidence = compact([
    ...(row.evidence ?? []),
    row.source,
    row.last_seen_at ? `last seen ${formatDateLabel(row.last_seen_at)}` : '',
    typeof row.evidence_count === 'number' ? `${row.evidence_count} evidence refs` : '',
  ]);
  const gaps = compact([...(row.gaps ?? []), ...(row.required_sources ?? [])]);

  return {
    id: `coverage:${row.domain}:${sanitizeKey(title)}`,
    name: title,
    kind: domain,
    state,
    evidence,
    missing: gaps,
    why: row.reason || row.details || row.description || 'Coverage matrix marks this row as needing attention.',
    nextAction: gaps[0] ? `Close coverage gap: ${gaps[0]}` : actionForState(state, title),
    cta: 'Open coverage',
    setup: ['Review coverage row', 'Attach source evidence', 'Refresh tenant overlay'],
    verification: ['coverage state updated', 'evidence count current', 'gaps cleared'],
    href: `/coverage?domain=${encodeURIComponent(String(row.domain || ''))}`,
  };
}

function serviceFromNode(node: NodeSummary): ObservabilityService {
  const fresh = isFresh(node.last_seen_at);
  return {
    id: `node:${node.id}`,
    name: node.hostname || shortId(node.id),
    kind: 'node agent',
    state: fresh ? 'healthy' : 'stale',
    evidence: compact([
      node.os,
      node.agent_version ? `agent ${node.agent_version}` : '',
      node.public_ip,
      node.last_seen_at ? `last seen ${formatDateLabel(node.last_seen_at)}` : '',
    ]),
    missing: fresh ? [] : ['fresh heartbeat'],
    why: fresh
      ? 'Node agent is reporting current inventory and can anchor observability evidence.'
      : 'Node agent heartbeat is outside the freshness window for live observability proof.',
    nextAction: fresh ? 'Keep node telemetry policy current.' : 'Repair or re-enroll the stale node agent.',
    cta: 'Open node',
    setup: ['Confirm agent service', 'Review telemetry profile', 'Check source labels'],
    verification: ['heartbeat current', 'services discovered', 'coverage rows linked'],
    href: `/nodes/${node.id}`,
  };
}

function deriveActions(services: ObservabilityService[]): ActionItem[] {
  const attention = services.filter((service) => service.state !== 'healthy').slice(0, 6);
  if (attention.length === 0 && services.length > 0) {
    return [
      {
        id: 'maintain-evidence-freshness',
        title: 'Keep observability evidence fresh',
        impact: 'Current live signals are healthy; keep parsers, source labels, and receipts current.',
        serviceId: services[0].id,
        effort: 'Low',
        risk: 'healthy',
      },
    ];
  }

  return attention.map((service) => ({
    id: `action:${service.id}`,
    title: service.nextAction,
    impact: service.why,
    serviceId: service.id,
    effort: effortForState(service.state),
    risk: STATE_META[service.state].tone,
  }));
}

function deriveKnowledgeChunks(
  services: ObservabilityService[],
  coverageRows: CoverageMatrixRow[],
): KnowledgeChunk[] {
  const serviceChunks = services.slice(0, 5).map((service) => ({
    id: `chunk:${service.id}`,
    source: service.name,
    topic: `${STATE_META[service.state].label} evidence`,
    state: chunkStateForService(service.state),
    summary: service.why,
    citations: compact([
      `observability:${service.id}`,
      ...service.evidence.slice(0, 2),
      ...service.missing.slice(0, 1),
    ]),
    openedFrom: ['Ask AI', 'Timeline', 'Case'],
  }));

  const coverageChunks = coverageRows
    .filter((row) => isAttentionCoverageState(row.coverage_state ?? row.state))
    .slice(0, 3)
    .map((row) => ({
      id: `chunk:coverage:${row.domain}:${sanitizeKey(row.title || row.name || row.subject || 'row')}`,
      source: row.title || row.name || row.subject || String(row.domain || 'coverage'),
      topic: 'Coverage gap',
      state: 'stale' as const,
      summary: row.reason || row.details || row.description || 'Coverage matrix row needs attention.',
      citations: compact([`coverage:${row.domain}`, ...(row.evidence ?? []).slice(0, 2), ...(row.gaps ?? []).slice(0, 1)]),
      openedFrom: ['Coverage', 'Ask AI'],
    }));

  return dedupeChunks([...serviceChunks, ...coverageChunks]).slice(0, 8);
}

function setupCardServices(services: ObservabilityService[]): ObservabilityService[] {
  const attention = services.filter((service) => service.state !== 'healthy');
  return (attention.length ? attention : services).slice(0, 4);
}

function dbOnboardingSteps(service: ObservabilityService): Array<[string, string]> {
  return [
    ['Detect source', `${service.name} is visible from ${service.evidence.slice(0, 2).join(', ') || 'live inventory'}.`],
    ['Explain access', service.missing.length ? `${service.missing.slice(0, 3).join(', ')} still needs evidence.` : 'No missing evidence is currently reported.'],
    ['Generate least privilege path', service.snippet ? 'A copyable least-privilege snippet is available.' : service.nextAction],
    ['Verify coverage', 'Coverage moves only after the source verifies and can be cited.'],
  ];
}

function preferredServiceId(services: ObservabilityService[]): string {
  return (
    services.find((service) => service.state === 'needs_access') ??
    services.find((service) => service.state !== 'healthy') ??
    services[0]
  ).id;
}

function stateFromSourceHealth(state: string | undefined): ObservabilityState {
  switch ((state ?? '').toLowerCase()) {
    case 'healthy':
    case 'parser_healthy':
    case 'collecting':
    case 'deployed':
      return 'healthy';
    case 'raw_only':
      return 'raw_only';
    case 'parser_failed':
    case 'failed':
    case 'collection_conflict':
      return 'failed';
    case 'silent':
    case 'stale':
    case 'backpressured':
      return 'stale';
    case 'approval_required':
    case 'approved':
    case 'proposed':
      return 'needs_access';
    case 'unsupported':
    case 'privacy_blocked':
      return 'unsupported';
    default:
      return 'detected_only';
  }
}

function stateFromCoverage(state: string | undefined): ObservabilityState {
  switch ((state ?? '').toLowerCase()) {
    case 'supported':
    case 'healthy':
    case 'passing':
    case 'collecting':
    case 'parser_healthy':
      return 'healthy';
    case 'partial':
    case 'manual_evidence':
      return 'partial';
    case 'raw_only':
      return 'raw_only';
    case 'unsupported':
      return 'unsupported';
    case 'stale':
      return 'stale';
    case 'failed':
    case 'parser_failed':
      return 'failed';
    case 'approval_required':
      return 'needs_access';
    default:
      return 'detected_only';
  }
}

function isAttentionCoverageState(state: string | undefined): boolean {
  const normalized = (state ?? '').toLowerCase();
  return Boolean(
    normalized &&
      !['supported', 'healthy', 'passing', 'not_applicable', 'exception'].includes(normalized),
  );
}

function actionForState(state: ObservabilityState, name: string): string {
  switch (state) {
    case 'needs_access':
      return `Grant least-privilege access for ${name}`;
    case 'raw_only':
      return `Attach parser coverage for ${name}`;
    case 'unsupported':
      return `Create connector contract for ${name}`;
    case 'stale':
      return `Refresh stale observability evidence for ${name}`;
    case 'failed':
      return `Investigate failed observability state for ${name}`;
    case 'partial':
    case 'detected_only':
    case 'fallback_active':
      return `Complete observability setup for ${name}`;
    case 'healthy':
    default:
      return `Keep ${name} evidence fresh`;
  }
}

function firstRecommendedAction(item: ContentPackSourceHealth): string | undefined {
  return item.recommended_actions?.find((action) => action.enabled)?.label;
}

function effortForState(state: ObservabilityState): string {
  if (state === 'unsupported' || state === 'failed') return 'High';
  if (state === 'needs_access' || state === 'stale') return 'Medium';
  return 'Low';
}

function chunkStateForService(state: ObservabilityState): KnowledgeChunk['state'] {
  if (state === 'healthy') return 'fresh';
  if (state === 'failed' || state === 'unsupported') return 'failed';
  return 'stale';
}

function isFresh(value?: string, hours = 24): boolean {
  if (!value) return false;
  const ts = Date.parse(value);
  if (!Number.isFinite(ts)) return false;
  return Date.now() - ts <= hours * 60 * 60 * 1000;
}

function formatDateLabel(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString([], { month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit' });
}

function compact(values: Array<string | null | undefined | false>): string[] {
  return values.map((value) => (typeof value === 'string' ? value.trim() : '')).filter(Boolean);
}

function shortId(id: string): string {
  return id.length > 8 ? id.slice(0, 8) : id;
}

function sanitizeKey(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '') || 'item';
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

function dedupeServices(services: ObservabilityService[]): ObservabilityService[] {
  const seen = new Set<string>();
  return services.filter((service) => {
    if (seen.has(service.id)) return false;
    seen.add(service.id);
    return true;
  });
}

function dedupeChunks(chunks: KnowledgeChunk[]): KnowledgeChunk[] {
  const seen = new Set<string>();
  return chunks.filter((chunk) => {
    if (seen.has(chunk.id)) return false;
    seen.add(chunk.id);
    return true;
  });
}

function ConnectorFacts({
  title,
  values,
  icon,
  empty = 'None',
}: {
  title: string;
  values: string[];
  icon: JSX.Element;
  empty?: string;
}): JSX.Element {
  return (
    <div>
      <p className="mb-2 flex items-center gap-2 font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
        {icon}
        {title}
      </p>
      {values.length > 0 ? (
        <div className="flex flex-wrap gap-1.5">
          {values.map((value) => (
            <StatusTag key={value} tone="info" variant="outline">
              {value}
            </StatusTag>
          ))}
        </div>
      ) : (
        <p className="text-xs text-text-muted">{empty}</p>
      )}
    </div>
  );
}

function ActionRow({
  action,
  rank,
  active,
  onOpen,
}: {
  action: ActionItem;
  rank: number;
  active: boolean;
  onOpen: () => void;
}): JSX.Element {
  return (
    <button
      type="button"
      onClick={onOpen}
      className={cn(
        'grid gap-3 rounded-md border border-border-subtle bg-surface p-3 text-left transition hover:border-border-strong hover:bg-hover md:grid-cols-[auto_1fr_auto]',
        active && 'border-brand-500/60 bg-brand-500/10',
      )}
    >
      <span className="grid h-8 w-8 place-items-center rounded-md bg-elevated font-mono text-xs text-text-secondary">
        {rank}
      </span>
      <span className="min-w-0">
        <span className="block text-sm font-medium text-foreground">{action.title}</span>
        <span className="block text-xs leading-5 text-text-secondary">{action.impact}</span>
      </span>
      <span className="flex flex-wrap items-center gap-2 md:justify-end">
        <StatusTag tone={action.risk}>{action.effort}</StatusTag>
        <ArrowRight className="h-4 w-4 text-text-muted" />
      </span>
    </button>
  );
}

function DebugSessionSheet({
  debug,
  ready,
  onChange,
}: {
  debug: {
    scope: string;
    ttl: string;
    reason: string;
    redaction: string;
    quota: string;
    rollback: string;
    approval: boolean;
  };
  ready: boolean;
  onChange: (field: keyof typeof debug, value: string | boolean) => void;
}): JSX.Element {
  return (
    <Sheet>
      <SheetTrigger asChild>
        <Button type="button" variant="secondary" size="sm">
          <TimerReset />
          Debug session
        </Button>
      </SheetTrigger>
      <SheetContent className="flex max-h-screen flex-col overflow-y-auto sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>Debug session guardrails</SheetTitle>
          <SheetDescription>
            TTL, scope, redaction, quota, rollback, and approval must be explicit.
          </SheetDescription>
        </SheetHeader>
        <div className="grid gap-4">
          <DebugInput label="Scope" value={debug.scope} onChange={(value) => onChange('scope', value)} placeholder="service / postgres" />
          <DebugInput label="TTL minutes" type="number" value={debug.ttl} onChange={(value) => onChange('ttl', value)} placeholder="30" />
          <DebugInput label="Reason" value={debug.reason} onChange={(value) => onChange('reason', value)} placeholder="Investigate latency spike" />
          <label className="grid gap-1 text-sm">
            <span className="font-medium text-foreground">Redaction policy</span>
            <select
              value={debug.redaction}
              onChange={(event) => onChange('redaction', event.target.value)}
              className="h-9 rounded-md border border-border-subtle bg-surface px-3 text-sm text-foreground"
            >
              <option value="strict">strict</option>
              <option value="standard">standard</option>
              <option value="custom">custom</option>
            </select>
          </label>
          <DebugInput label="Quota" value={debug.quota} onChange={(value) => onChange('quota', value)} placeholder="100 MB or 15k events" />
          <DebugInput label="Rollback plan" value={debug.rollback} onChange={(value) => onChange('rollback', value)} placeholder="Restore baseline log level" />
          <label className="flex items-start gap-3 rounded-md border border-border-subtle bg-surface p-3 text-sm text-text-secondary">
            <input
              type="checkbox"
              checked={debug.approval}
              onChange={(event) => onChange('approval', event.target.checked)}
              className="mt-1"
            />
            <span>
              <span className="block font-medium text-foreground">Approval state reviewed</span>
              <span className="block text-xs leading-5 text-text-secondary">
                Debug collection stays blocked until this state is recorded.
              </span>
            </span>
          </label>
          <Button type="button" variant="primary" disabled={!ready} className="justify-between">
            <span className="inline-flex items-center gap-2">
              <Play />
              Start preview
            </span>
            <StatusTag tone={ready ? 'healthy' : 'warning'}>
              {ready ? 'ready' : 'blocked'}
            </StatusTag>
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}

function DebugInput({
  label,
  value,
  onChange,
  placeholder,
  type = 'text',
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
  type?: 'text' | 'number';
}): JSX.Element {
  return (
    <label className="grid gap-1 text-sm">
      <span className="font-medium text-foreground">{label}</span>
      <input
        type={type}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        className="h-9 rounded-md border border-border-subtle bg-surface px-3 text-sm text-foreground"
      />
    </label>
  );
}

function summarizeServices(services: ObservabilityService[]) {
  return services.reduce(
    (acc, service) => {
      acc.total += 1;
      if (service.state === 'healthy') acc.healthy += 1;
      if (service.state === 'partial' || service.state === 'raw_only' || service.state === 'detected_only') acc.partial += 1;
      if (service.state === 'needs_access') acc.needsAccess += 1;
      if (service.state === 'unsupported') acc.unsupported += 1;
      return acc;
    },
    { total: 0, healthy: 0, partial: 0, needsAccess: 0, unsupported: 0 },
  );
}

export default Observability;
