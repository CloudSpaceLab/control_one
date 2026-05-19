import { useState } from 'react';
import { Send, Sparkles } from 'lucide-react';
import {
  Alert,
  Eyebrow,
  Loader,
  Panel,
  SectionHeader,
} from '@/components/kit';
import { Button } from '@/components/ui/button';
import { useApiClient } from '@/hooks/useApiClient';
import { useTenant } from '@/providers/TenantProvider';

const SAMPLE_QUESTIONS = [
  'What new services appeared this week?',
  'Which nodes have public-facing HTTP on non-standard ports?',
  'Summarize this fleet’s posture for a board update.',
  'Which nodes are reporting calibrating health right now?',
];

interface Turn {
  role: 'user' | 'assistant';
  content: string;
  citations?: string[];
}

export function Ask(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [question, setQuestion] = useState('');
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
        { role: 'assistant', content: resp.answer, citations: resp.citations },
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
        title="Ask CISO"
        description="Natural-language questions grounded in this tenant’s knowledge graph (nodes, services, packages, firewall, recent activity). Configure the LLM provider in Settings → AI."
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
            Ask anything about this fleet. The model only sees the knowledge graph
            for the current tenant.
          </p>
        ) : (
          <ul className="flex flex-col gap-3">
            {turns.map((t, i) => (
              <li key={i} className="flex flex-col gap-1">
                <Eyebrow tone={t.role === 'user' ? 'brand' : 'muted'}>
                  {t.role === 'user' ? 'You' : 'CISO'}
                </Eyebrow>
                <div
                  className={
                    t.role === 'user'
                      ? 'rounded-md bg-brand-500/10 px-3 py-2 text-sm text-foreground'
                      : 'rounded-md border border-border-subtle bg-surface px-3 py-2 text-sm text-text-secondary'
                  }
                >
                  <p className="whitespace-pre-wrap">{t.content}</p>
                  {t.citations && t.citations.length > 0 && (
                    <p className="mt-1 font-mono text-[0.65rem] text-text-muted">
                      sources: {t.citations.join(', ')}
                    </p>
                  )}
                </div>
              </li>
            ))}
            {busy && <Loader size="sm" label="Thinking…" />}
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
          placeholder="Ask about this fleet’s posture, services, threats…"
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
        Ctrl/⌘+Enter sends. The model sees the per-tenant knowledge_graph.md
        as grounded context (cached 5 min); it does not see raw events or
        secrets.
      </p>
    </div>
  );
}
