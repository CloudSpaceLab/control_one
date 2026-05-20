import { useState } from 'react';
import { AlertTriangle, CheckCircle2, ClipboardList, Send, Sparkles } from 'lucide-react';
import { useSearchParams } from 'react-router-dom';
import {
  Alert,
  Eyebrow,
  Loader,
  Panel,
  SectionHeader,
} from '@/components/kit';
import { Button } from '@/components/ui/button';
import { useApiClient } from '@/hooks/useApiClient';
import type { AIAskToolTraceEntry } from '@/lib/api';
import { useTenant } from '@/providers/TenantProvider';

const SAMPLE_QUESTIONS = [
  'What new services appeared this week?',
  'Which nodes have public-facing HTTP on non-standard ports?',
  "Summarize this fleet's posture for a board update.",
  'Which nodes need health attention right now?',
];

interface Turn {
  role: 'user' | 'assistant';
  content: string;
  citations?: string[];
  sourceCitations?: string[];
  toolTrace?: AIAskToolTraceEntry[];
  confidence?: string;
}

export function Ask(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [searchParams] = useSearchParams();
  const [question, setQuestion] = useState(() => searchParams.get('q') ?? '');
  const [turns, setTurns] = useState<Turn[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (q: string) => {
    if (!currentTenantId) {
      setError('Select a tenant first.');
      return;
    }
    const trimmed = q.trim();
    if (!trimmed) return;
    setError(null);
    setQuestion('');
    setTurns((prev) => [...prev, { role: 'user', content: trimmed }]);
    setBusy(true);
    try {
      const resp = await client.askAI(currentTenantId, trimmed);
      setTurns((prev) => [
        ...prev,
        {
          role: 'assistant',
          content: resp.answer,
          citations: resp.citations,
          sourceCitations: resp.source_citations,
          toolTrace: resp.tool_trace,
          confidence: resp.confidence,
        },
      ]);
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'ask failed';
      setError(msg);
      setTurns((prev) => [...prev, { role: 'assistant', content: `[error] ${msg}` }]);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="ASK"
        title="Ask AI"
        description="Natural-language investigation over this tenant's knowledge graph, normalized events, evidence, posture, and case tools. Configure the LLM provider in Settings > AI."
      />

      {turns.length === 0 && (
        <Panel padding="md" eyebrow="TRY" title="Sample questions">
          <div className="flex flex-wrap gap-2">
            {SAMPLE_QUESTIONS.map((q) => (
              <button
                key={q}
                type="button"
                className="rounded-full border border-border-subtle bg-surface px-3 py-1.5 text-sm text-text-secondary transition-colors hover:border-border-strong hover:bg-hover hover:text-foreground"
                onClick={() => submit(q)}
              >
                {q}
              </button>
            ))}
          </div>
        </Panel>
      )}

      <Panel padding="md" eyebrow="TRANSCRIPT" title="Conversation">
        {turns.length === 0 ? (
          <p className="py-4 text-sm text-text-muted">
            Ask anything about this fleet. When a provider is configured, answers
            are grounded in tenant-scoped tools and cited evidence.
          </p>
        ) : (
          <ul className="flex flex-col gap-3">
            {turns.map((t, i) => (
              <li key={i} className="flex flex-col gap-1">
                <Eyebrow tone={t.role === 'user' ? 'brand' : 'muted'}>
                  {t.role === 'user' ? 'You' : 'AI'}
                </Eyebrow>
                <div
                  className={
                    t.role === 'user'
                      ? 'rounded-md bg-brand-500/10 px-3 py-2 text-sm text-foreground'
                      : 'rounded-md border border-border-subtle bg-surface px-3 py-2 text-sm text-text-secondary'
                  }
                >
                  <p className="whitespace-pre-wrap">{t.content}</p>
                  {t.role === 'assistant' && (
                    <GroundingDetails
                      citations={t.citations}
                      sourceCitations={t.sourceCitations}
                      toolTrace={t.toolTrace}
                      confidence={t.confidence}
                    />
                  )}
                </div>
              </li>
            ))}
            {busy && <Loader size="sm" label="Thinking..." />}
          </ul>
        )}
      </Panel>

      {error && <Alert variant="critical">{error}</Alert>}

      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          submit(question);
        }}
      >
        <textarea
          rows={3}
          value={question}
          onChange={(e) => setQuestion(e.target.value)}
          placeholder="Ask about this fleet's posture, services, threats..."
          className="flex-1 rounded-md border border-border-subtle bg-surface px-3 py-2 text-sm text-foreground focus:border-brand-500 focus:outline-none"
          onKeyDown={(e) => {
            if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
              e.preventDefault();
              submit(question);
            }
          }}
        />
        <Button type="submit" variant="primary" disabled={busy || !question.trim()}>
          {busy ? <Loader size="xs" /> : <Send className="h-4 w-4" />}
          Ask
        </Button>
      </form>

      <p className="text-xs text-text-muted">
        <Sparkles className="mr-1 inline h-3.5 w-3.5 align-text-bottom" />
        Ctrl/Cmd+Enter sends. Answers use tenant-scoped context and approved
        investigation tools; raw event rows are citation-bound and secrets stay
        out of scope.
      </p>
    </div>
  );
}

function GroundingDetails({
  citations,
  sourceCitations,
  toolTrace,
  confidence,
}: {
  citations?: string[];
  sourceCitations?: string[];
  toolTrace?: AIAskToolTraceEntry[];
  confidence?: string;
}): JSX.Element | null {
  const normalizedCitations = (citations ?? []).filter(Boolean);
  const normalizedSourceCitations = (sourceCitations ?? []).filter(Boolean);
  const normalizedTrace = (toolTrace ?? []).filter((trace) => trace.name);
  if (normalizedCitations.length === 0 && normalizedSourceCitations.length === 0 && normalizedTrace.length === 0 && !confidence) {
    return null;
  }

  return (
    <div className="mt-3 border-t border-border-subtle pt-3">
      <div className="flex flex-wrap items-center gap-2 text-[0.68rem] text-text-muted">
        {confidence && (
          <span className="inline-flex items-center gap-1 rounded-sm border border-border-subtle bg-surface-2 px-2 py-1 font-mono uppercase">
            <ClipboardList className="h-3.5 w-3.5" />
            Confidence: {confidence.replaceAll('_', ' ')}
          </span>
        )}
        {normalizedCitations.map((citation) => (
          <span
            key={citation}
            className="inline-flex max-w-full items-center gap-1 rounded-sm border border-border-subtle bg-surface-2 px-2 py-1 font-mono text-[0.65rem] text-text-secondary"
            title={citation}
          >
            <ClipboardList className="h-3.5 w-3.5 flex-none" />
            <span className="truncate">{citation}</span>
          </span>
        ))}
        {normalizedSourceCitations.map((citation) => (
          <span
            key={citation}
            className="inline-flex max-w-full items-center gap-1 rounded-sm border border-state-healthy/30 bg-state-healthy/10 px-2 py-1 font-mono text-[0.65rem] text-state-healthy"
            title={citation}
          >
            <ClipboardList className="h-3.5 w-3.5 flex-none" />
            <span className="truncate">Evidence: {citation}</span>
          </span>
        ))}
      </div>
      {normalizedTrace.length > 0 && (
        <ul className="mt-2 grid gap-1.5">
          {normalizedTrace.map((trace, index) => (
            <li
              key={`${trace.name}:${trace.citation_id ?? index}`}
              className="flex flex-wrap items-center gap-x-2 gap-y-1 rounded-sm bg-surface-2 px-2 py-1.5 text-xs text-text-secondary"
            >
              {trace.ok ? (
                <CheckCircle2 className="h-3.5 w-3.5 text-state-healthy" />
              ) : (
                <AlertTriangle className="h-3.5 w-3.5 text-state-warning" />
              )}
              <span className="font-medium">{toolTraceLabel(trace.name)}</span>
              {trace.citation_id && (
                <span className="font-mono text-[0.65rem] text-text-muted">{trace.citation_id}</span>
              )}
              <span className="font-mono text-[0.65rem] text-text-muted">{trace.duration_ms}ms</span>
              {trace.error && <span className="text-state-warning">{trace.error}</span>}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function toolTraceLabel(name: string): string {
  return name
    .split('_')
    .filter(Boolean)
    .map((part) => part.slice(0, 1).toUpperCase() + part.slice(1))
    .join(' ');
}
