import { useEffect, useState, type FormEvent } from 'react';
import { Sparkles } from 'lucide-react';
import { Alert, Eyebrow, Loader, Panel } from '@/components/kit';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { useApiClient } from '@/hooks/useApiClient';
import { useTenant } from '@/providers/TenantProvider';
import type { AIConfigResponse } from '@/lib/api';

const PROVIDERS = [
  {
    value: 'anthropic',
    label: 'Anthropic (Claude)',
    defaultModel: 'claude-sonnet-4-6',
    basePlaceholder: 'https://api.anthropic.com',
    keyPlaceholder: 'sk-ant-...',
  },
  {
    value: 'openai',
    label: 'OpenAI',
    defaultModel: 'gpt-4o-mini',
    basePlaceholder: 'https://api.openai.com',
    keyPlaceholder: 'sk-...',
  },
  {
    value: 'google',
    label: 'Google Gemini',
    defaultModel: 'gemini-2.5-flash',
    basePlaceholder: 'https://generativelanguage.googleapis.com',
    keyPlaceholder: 'AIza...',
  },
];

export function AISettingsTab(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [cfg, setCfg] = useState<AIConfigResponse | null>(null);
  const [provider, setProvider] = useState('anthropic');
  const [model, setModel] = useState('claude-sonnet-4-6');
  const [baseUrl, setBaseUrl] = useState('');
  const [apiKey, setApiKey] = useState('');
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [feedback, setFeedback] = useState<{ kind: 'ok' | 'err'; message: string } | null>(null);
  const providerMeta = PROVIDERS.find((p) => p.value === provider) ?? PROVIDERS[0];

  useEffect(() => {
    if (!currentTenantId) return;
    let cancelled = false;
    setLoading(true);
    client
      .getAIConfig(currentTenantId)
      .then((c) => {
        if (cancelled) return;
        setCfg(c);
        setProvider(c.provider || 'anthropic');
        setModel(c.model || 'claude-sonnet-4-6');
        setBaseUrl(c.base_url || '');
      })
      .catch(() => {
        if (!cancelled) setCfg(null);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [client, currentTenantId]);

  const save = async (e: FormEvent) => {
    e.preventDefault();
    if (!currentTenantId) return;
    setSaving(true);
    setFeedback(null);
    try {
      await client.updateAIConfig(currentTenantId, {
        provider,
        model,
        base_url: baseUrl,
        api_key: apiKey, // empty string preserves existing
      });
      const refreshed = await client.getAIConfig(currentTenantId);
      setCfg(refreshed);
      setApiKey('');
      setFeedback({ kind: 'ok', message: 'Saved' });
    } catch (err) {
      setFeedback({ kind: 'err', message: err instanceof Error ? err.message : 'save failed' });
    } finally {
      setSaving(false);
    }
  };

  const test = async () => {
    if (!currentTenantId) return;
    setTesting(true);
    setFeedback(null);
    try {
      const r = await client.testAIConfig(currentTenantId);
      setFeedback(
        r.ok
          ? { kind: 'ok', message: `Provider responded: "${r.reply ?? ''}"` }
          : { kind: 'err', message: r.error ?? 'test failed' },
      );
    } catch (err) {
      setFeedback({ kind: 'err', message: err instanceof Error ? err.message : 'test failed' });
    } finally {
      setTesting(false);
    }
  };

  if (!currentTenantId) {
    return (
      <Panel padding="md" eyebrow="AI" title="LLM provider">
        <Alert variant="info">Select a tenant first.</Alert>
      </Panel>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <Panel padding="md" eyebrow="ASK CISO" title="LLM provider config">
        <p className="text-sm text-text-secondary">
          Configure the LLM that powers the natural-language Ask surface. The
          provider sees the per-tenant knowledge_graph.md as grounded context;
          it does not see secrets, raw events, or anything outside this tenant.
        </p>
        {loading ? (
          <div className="mt-4">
            <Loader label="Loading config…" />
          </div>
        ) : (
          <form onSubmit={save} className="mt-4 flex flex-col gap-3">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Field label="Provider">
                <select
                  className="h-9 rounded-md border border-border-subtle bg-surface px-3 text-sm text-foreground"
                  value={provider}
                  onChange={(e) => {
                    const next = PROVIDERS.find((p) => p.value === e.target.value) ?? PROVIDERS[0];
                    setProvider(next.value);
                    setModel(next.defaultModel);
                  }}
                >
                  {PROVIDERS.map((p) => (
                    <option key={p.value} value={p.value}>{p.label}</option>
                  ))}
                </select>
              </Field>
              <Field label="Model">
                <Input value={model} onChange={(e) => setModel(e.target.value)} />
              </Field>
              <Field label="Base URL (optional)">
                <Input
                  value={baseUrl}
                  onChange={(e) => setBaseUrl(e.target.value)}
                  placeholder={providerMeta.basePlaceholder}
                />
              </Field>
              <Field label={cfg?.has_api_key ? 'API key (set — leave blank to keep)' : 'API key'}>
                <Input
                  type="password"
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                  placeholder={cfg?.has_api_key ? '••••••••' : providerMeta.keyPlaceholder}
                  autoComplete="off"
                />
              </Field>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Button type="submit" variant="primary" disabled={saving}>
                {saving ? 'Saving…' : 'Save config'}
              </Button>
              <Button
                type="button"
                variant="secondary"
                disabled={!cfg?.has_api_key || testing}
                onClick={test}
              >
                <Sparkles className="h-4 w-4" />
                {testing ? 'Testing…' : 'Test connection'}
              </Button>
              {cfg?.updated_at && (
                <Eyebrow>Updated {new Date(cfg.updated_at).toLocaleString()}</Eyebrow>
              )}
            </div>
          </form>
        )}
        {feedback && (
          <div className="mt-3">
            <Alert variant={feedback.kind === 'ok' ? 'success' : 'critical'}>
              {feedback.message}
            </Alert>
          </div>
        )}
      </Panel>

      <Alert variant="info" title="Ask AI availability">
        Ask AI is always available in the investigation sidebar. Answers require
        a tenant-scoped provider key; without one, the page shows a configuration
        state instead of a hidden or broken route.
      </Alert>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <Label className="text-xs text-text-muted">{label}</Label>
      {children}
    </div>
  );
}
