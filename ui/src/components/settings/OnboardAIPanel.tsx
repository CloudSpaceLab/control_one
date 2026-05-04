import { useEffect, useState, type FormEvent } from 'react';
import { Check, ChevronDown, Sparkles } from 'lucide-react';
import { Alert, Eyebrow, Loader, Panel, StatusTag } from '@/components/kit';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { useApiClient } from '@/hooks/useApiClient';
import { useTenant } from '@/providers/TenantProvider';
import type { AIConfigResponse } from '@/lib/api';

// OnboardAIPanel surfaces the same Settings → AI config inline at the top
// of the onboarding flow so a fresh tenant can drop in its Anthropic key
// once and unlock /ask immediately. Auto-collapses once a key is on file
// so it doesn't nag returning operators.
export function OnboardAIPanel(): JSX.Element | null {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [cfg, setCfg] = useState<AIConfigResponse | null>(null);
  const [open, setOpen] = useState(true);
  const [loaded, setLoaded] = useState(false);
  const [model, setModel] = useState('claude-sonnet-4-6');
  const [apiKey, setApiKey] = useState('');
  const [saving, setSaving] = useState(false);
  const [feedback, setFeedback] = useState<{ kind: 'ok' | 'err'; message: string } | null>(null);

  useEffect(() => {
    if (!currentTenantId) {
      setLoaded(true);
      return;
    }
    let cancelled = false;
    client
      .getAIConfig(currentTenantId)
      .then((c) => {
        if (cancelled) return;
        setCfg(c);
        setModel(c.model || 'claude-sonnet-4-6');
        // Collapse if a key is already configured — admins onboarding more
        // hosts later don't need this nagging them.
        if (c.has_api_key) setOpen(false);
      })
      .catch(() => {
        if (!cancelled) setCfg(null);
      })
      .finally(() => {
        if (!cancelled) setLoaded(true);
      });
    return () => {
      cancelled = true;
    };
  }, [client, currentTenantId]);

  if (!currentTenantId || !loaded) return null;

  const save = async (e: FormEvent) => {
    e.preventDefault();
    if (!currentTenantId) return;
    setSaving(true);
    setFeedback(null);
    try {
      await client.updateAIConfig(currentTenantId, {
        provider: 'anthropic',
        model,
        base_url: '',
        api_key: apiKey,
      });
      const refreshed = await client.getAIConfig(currentTenantId);
      setCfg(refreshed);
      setApiKey('');
      setFeedback({ kind: 'ok', message: 'Saved · /ask is now wired up for this tenant.' });
      // Briefly show success then auto-collapse.
      setTimeout(() => setOpen(false), 1500);
    } catch (err) {
      setFeedback({
        kind: 'err',
        message: err instanceof Error ? err.message : 'save failed',
      });
    } finally {
      setSaving(false);
    }
  };

  if (!open) {
    return (
      <Panel
        padding="md"
        eyebrow="ASK CISO"
        title="LLM provider"
        actions={
          <div className="flex items-center gap-2">
            {cfg?.has_api_key ? (
              <StatusTag tone="healthy">
                <Check className="h-3 w-3" /> Key on file
              </StatusTag>
            ) : (
              <StatusTag tone="warning">Not configured</StatusTag>
            )}
            <Button variant="ghost" size="sm" onClick={() => setOpen(true)}>
              <ChevronDown className="h-4 w-4" /> {cfg?.has_api_key ? 'Update' : 'Configure'}
            </Button>
          </div>
        }
      >
        <p className="text-sm text-text-muted">
          {cfg?.has_api_key
            ? `Anthropic key is on file (model ${cfg.model || 'claude-sonnet-4-6'}). Manage in Settings → AI.`
            : 'Skip for now — Ask CISO is feature-flagged off until a provider is configured.'}
        </p>
      </Panel>
    );
  }

  return (
    <Panel
      padding="md"
      eyebrow="ASK CISO · OPTIONAL"
      title="Configure the LLM that powers natural-language Ask"
      toneAccent="brand"
    >
      <p className="text-sm text-text-secondary">
        Drop in an Anthropic key now and operators can ask questions
        grounded in this tenant&apos;s knowledge graph the moment the first node
        reports in. The key is stored per-tenant; you can update or rotate
        it later from Settings → AI.
      </p>
      <form onSubmit={save} className="mt-3 flex flex-col gap-3">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-[1fr_2fr]">
          <Field label="Model">
            <Input value={model} onChange={(e) => setModel(e.target.value)} />
          </Field>
          <Field label="Anthropic API key">
            <Input
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder="sk-ant-..."
              autoComplete="off"
            />
          </Field>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" variant="primary" size="md" disabled={saving || !apiKey.trim()}>
            {saving ? <Loader size="xs" /> : <Sparkles className="h-4 w-4" />}
            {saving ? 'Saving…' : 'Save & enable'}
          </Button>
          <Button type="button" variant="ghost" size="md" onClick={() => setOpen(false)}>
            Skip for now
          </Button>
          <Eyebrow>
            Ask is gated by FEATURE_AI_ASK on the controlplane and __C1_FLAGS__.ai_ask in the UI.
          </Eyebrow>
        </div>
      </form>
      {feedback && (
        <div className="mt-3">
          <Alert variant={feedback.kind === 'ok' ? 'success' : 'critical'}>
            {feedback.message}
          </Alert>
        </div>
      )}
    </Panel>
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
