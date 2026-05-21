import { useMemo, useState } from 'react';
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
  EmptyState,
  KpiTile,
  Panel,
  SectionHeader,
  StatusTag,
  type StateTone,
} from '@/components/kit';
import { cn } from '@/lib/utils';

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

const SERVICES: ObservabilityService[] = [
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
    name: 'payments FastAPI',
    kind: 'app framework',
    state: 'partial',
    evidence: ['application logs', 'request IDs'],
    missing: ['native middleware', 'stack traces'],
    why: 'AI can cite request logs, but incident timelines lose stack traces and handler spans.',
    nextAction: 'Add middleware instrumentation and verify request correlation.',
    cta: 'Copy middleware snippet',
    setup: ['Install package', 'Register middleware', 'Deploy one canary instance'],
    verification: ['trace ID appears', 'error span appears', 'fallback state clears'],
    snippet: 'pip install controlone-fastapi\napp.add_middleware(ControlOneMiddleware, redact=\"strict\")',
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

const ACTIONS: ActionItem[] = [
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

const KNOWLEDGE_CHUNKS: KnowledgeChunk[] = [
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
    source: 'payments FastAPI',
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
  const [selectedId, setSelectedId] = useState('svc-postgres');
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const [selectedChunkId, setSelectedChunkId] = useState(KNOWLEDGE_CHUNKS[0].id);
  const [debug, setDebug] = useState({
    scope: '',
    ttl: '',
    reason: '',
    redaction: 'strict',
    quota: '',
    rollback: '',
    approval: false,
  });
  const selected = SERVICES.find((service) => service.id === selectedId) ?? SERVICES[0];
  const selectedChunk =
    KNOWLEDGE_CHUNKS.find((chunk) => chunk.id === selectedChunkId) ?? KNOWLEDGE_CHUNKS[0];
  const summary = useMemo(() => summarizeServices(SERVICES), []);
  const debugReady = Boolean(
    debug.scope.trim() &&
      Number(debug.ttl) > 0 &&
      debug.reason.trim() &&
      debug.redaction &&
      debug.quota.trim() &&
      debug.rollback.trim() &&
      debug.approval,
  );

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
        description="Connector, instrumentation, debug, and knowledge states translated into operator decisions."
        actions={
          <div className="flex flex-wrap gap-2">
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

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
        <KpiTile label="Detected services" value={summary.total.toString()} tone="info" icon={<Network />} />
        <KpiTile label="Healthy" value={summary.healthy.toString()} tone="healthy" icon={<ShieldCheck />} />
        <KpiTile label="Partial" value={summary.partial.toString()} tone="warning" icon={<AlertTriangle />} />
        <KpiTile label="Needs access" value={summary.needsAccess.toString()} tone="degraded" icon={<KeyRound />} />
        <KpiTile label="Unsupported" value={summary.unsupported.toString()} tone="critical" icon={<Terminal />} />
      </div>

      <div className="grid grid-cols-1 gap-5 xl:grid-cols-[minmax(0,1fr)_24rem]">
        <Panel padding="md" eyebrow="STACK MAP" title="payments-api">
          <div className="overflow-x-auto rounded-lg border border-border-subtle">
            <table className="w-full min-w-[720px] text-sm">
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
                {SERVICES.map((service) => {
                  const meta = STATE_META[service.state];
                  return (
                    <tr key={service.id} className="border-t border-border-subtle">
                      <td className="px-3 py-3">
                        <div className="font-medium text-foreground">{service.name}</div>
                        <div className="text-xs text-text-muted">{service.kind}</div>
                      </td>
                      <td className="px-3 py-3">
                        <StatusTag tone={meta.tone}>{meta.label}</StatusTag>
                        <p className="mt-1 max-w-[14rem] text-xs text-text-muted">{meta.plain}</p>
                      </td>
                      <td className="px-3 py-3 text-xs text-text-secondary">
                        {service.evidence.join(', ') || 'none'}
                      </td>
                      <td className="px-3 py-3 text-xs text-text-secondary">{service.nextAction}</td>
                      <td className="px-3 py-3 text-right">
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
        </Panel>
      </div>

      <div className="grid grid-cols-1 gap-5 xl:grid-cols-[minmax(0,1fr)_26rem]">
        <Panel padding="md" eyebrow="NEXT ACTIONS" title="Ranked setup queue">
          <div className="grid gap-3">
            {ACTIONS.map((action, index) => (
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
            {[
              ['Detect engine', 'PostgreSQL found from process, port, and app fingerprint.'],
              ['Explain access', 'Audit logs, slow query logs, and role metadata are missing.'],
              ['Generate grant', 'Grant statements are copied without collecting raw secrets.'],
              ['Test access', 'Coverage moves only after the audit source verifies.'],
            ].map(([label, detail], index) => (
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
            {SERVICES.filter((service) => service.kind === 'app framework' || service.kind === 'worker').map((service) => (
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
              {KNOWLEDGE_CHUNKS.map((chunk) => (
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
          <DebugInput label="Scope" value={debug.scope} onChange={(value) => onChange('scope', value)} placeholder="payments-api / postgres" />
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
