import { useMemo, useState } from 'react';
import { Plus, Play, ArrowUp, X } from 'lucide-react';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { Panel, SectionHeader } from '../components/kit';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { useToast } from '../providers/ToastProvider';
import type { CreatePortRulePayload, SimulateResult } from '../lib/api';

// Visual rule builder: the operator arranges "blocks" (condition + action)
// and we translate them into a draft rule payload + optional simulation.
// This is drag-and-drop via the HTML5 API — no runtime deps.

type BlockKind = 'port' | 'log' | 'compliance';

interface Block {
  id: string;
  kind: BlockKind;
  config: Record<string, string | number>;
}

const BLOCK_TEMPLATES: Record<BlockKind, Block> = {
  port: {
    id: '',
    kind: 'port',
    config: { port: 22, protocol: 'tcp', expected_state: 'closed', severity: 'medium' },
  },
  log: {
    id: '',
    kind: 'log',
    config: { log_source: 'auth', pattern: 'failed login', window_seconds: 60, threshold: 3, severity: 'high' },
  },
  compliance: {
    id: '',
    kind: 'compliance',
    config: { rule_expr: 'service("sshd").enabled == true', severity: 'high' },
  },
};

export function RuleBuilder(): JSX.Element {
  const client = useApiClient();
  const { data: tenants } = useTenants({ limit: 50, offset: 0 });
  const { showToast } = useToast();
  const [tenantId, setTenantId] = useState('');
  const [blocks, setBlocks] = useState<Block[]>([]);
  const [simResult, setSimResult] = useState<SimulateResult | null>(null);
  const [simBusy, setSimBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [dragIdx, setDragIdx] = useState<number | null>(null);

  const addBlock = (kind: BlockKind) => {
    const template = BLOCK_TEMPLATES[kind];
    setBlocks([...blocks, { ...template, id: crypto.randomUUID(), config: { ...template.config } }]);
  };

  const updateConfig = (id: string, key: string, value: string | number) => {
    setBlocks(blocks.map((b) => (b.id === id ? { ...b, config: { ...b.config, [key]: value } } : b)));
  };

  const removeBlock = (id: string) => setBlocks(blocks.filter((b) => b.id !== id));

  const onDragStart = (i: number) => setDragIdx(i);
  const onDragOver = (e: React.DragEvent) => e.preventDefault();
  const onDrop = (i: number) => {
    if (dragIdx === null || dragIdx === i) return;
    const next = [...blocks];
    const [moved] = next.splice(dragIdx, 1);
    next.splice(i, 0, moved);
    setBlocks(next);
    setDragIdx(null);
  };

  const yaml = useMemo(() => blocksToYAML(blocks), [blocks]);

  const simulate = async () => {
    if (!tenantId || blocks.length === 0) return;
    const block = blocks[blocks.length - 1];
    setSimBusy(true);
    setError(null);
    try {
      const result = await client.simulateRule({
        tenant_id: tenantId,
        rule_type: block.kind,
        window_days: 30,
        rule: block.config as Record<string, unknown>,
      });
      setSimResult(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'simulate failed');
    } finally {
      setSimBusy(false);
    }
  };

  const promote = async () => {
    if (!tenantId || blocks.length === 0) return;
    const block = blocks[blocks.length - 1];
    if (block.kind !== 'port') {
      showToast('Promote supported for port rules in this build.', 'info');
      return;
    }
    try {
      const cfg = block.config;
      const payload: CreatePortRulePayload = {
        tenant_id: tenantId,
        name: String(cfg.name ?? `port-${cfg.port}-${cfg.expected_state}`),
        port: Number(cfg.port),
        protocol: String(cfg.protocol) as 'tcp' | 'udp',
        expected_state: String(cfg.expected_state) as 'open' | 'closed',
        severity: String(cfg.severity ?? 'medium'),
        enabled: true,
      };
      await client.createPortRule(payload);
      showToast('Rule promoted.', 'success');
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'Promote failed', 'error');
    }
  };

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="POSTURE · BUILDER"
        title="Visual rule builder"
        description="Compose blocks, simulate against history, then promote."
        actions={
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="rb-tenant" className="sr-only">
              Tenant
            </Label>
            <select
              id="rb-tenant"
              value={tenantId}
              onChange={(e) => setTenantId(e.target.value)}
              aria-label="Tenant"
              className="flex h-9 rounded-md border border-border-subtle bg-surface px-3 py-1 text-sm text-foreground focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30"
            >
              <option value="">Choose tenant…</option>
              {tenants.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          </div>
        }
      />

      <div className="flex flex-wrap gap-2">
        <Button type="button" variant="secondary" onClick={() => addBlock('port')}>
          <Plus className="h-4 w-4" /> Port block
        </Button>
        <Button type="button" variant="secondary" onClick={() => addBlock('log')}>
          <Plus className="h-4 w-4" /> Log block
        </Button>
        <Button type="button" variant="secondary" onClick={() => addBlock('compliance')}>
          <Plus className="h-4 w-4" /> Compliance block
        </Button>
      </div>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
        <Panel padding="md" eyebrow="BLOCKS" title="Composition">
          {blocks.length === 0 ? (
            <p className="text-sm text-text-muted">No blocks yet. Add one above.</p>
          ) : (
            <ol className="m-0 flex list-none flex-col gap-2 p-0" onDragOver={onDragOver}>
              {blocks.map((b, i) => (
                <li
                  key={b.id}
                  draggable
                  onDragStart={() => onDragStart(i)}
                  onDragOver={onDragOver}
                  onDrop={() => onDrop(i)}
                  className="cursor-move rounded-md border border-border-subtle bg-surface p-3"
                >
                  <div className="flex items-center justify-between">
                    <strong className="text-sm font-semibold text-foreground">
                      {b.kind.toUpperCase()} block
                    </strong>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => removeBlock(b.id)}
                      aria-label="Remove block"
                    >
                      <X className="h-4 w-4" />
                    </Button>
                  </div>
                  <div className="mt-2 grid grid-cols-[repeat(auto-fit,minmax(140px,1fr))] gap-2">
                    {Object.entries(b.config).map(([k, v]) => (
                      <div key={k} className="flex flex-col gap-1.5">
                        <Label htmlFor={`${b.id}-${k}`} className="text-xs">
                          {k}
                        </Label>
                        <Input
                          id={`${b.id}-${k}`}
                          value={String(v)}
                          onChange={(e) => {
                            const raw = e.target.value;
                            const parsed =
                              !isNaN(Number(raw)) && raw !== '' ? Number(raw) : raw;
                            updateConfig(b.id, k, parsed);
                          }}
                        />
                      </div>
                    ))}
                  </div>
                </li>
              ))}
            </ol>
          )}
        </Panel>

        <Panel padding="md" tone="inset" eyebrow="PREVIEW" title="YAML">
          <pre className="overflow-auto rounded-md border border-border-subtle bg-surface-2 p-3 font-mono text-[0.75rem] leading-relaxed text-text-secondary">
            {yaml || '# no blocks'}
          </pre>
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant="primary"
              onClick={simulate}
              disabled={!tenantId || blocks.length === 0 || simBusy}
            >
              <Play className="h-4 w-4" /> {simBusy ? 'Simulating…' : 'Simulate'}
            </Button>
            <Button
              type="button"
              variant="secondary"
              onClick={promote}
              disabled={!tenantId || blocks.length === 0}
            >
              <ArrowUp className="h-4 w-4" /> Promote
            </Button>
          </div>
          {error && <p className="text-sm text-state-critical">{error}</p>}
          {simResult && (
            <div className="rounded-md border border-border-subtle bg-surface p-3">
              <strong className="text-sm font-semibold text-foreground">Simulation</strong>
              <p className="mt-1 text-xs text-text-muted">{simResult.summary}</p>
              <p className="mt-1 text-xs text-text-secondary">
                Pass: {simResult.nodes_would_pass} · Fail: {simResult.nodes_would_fail}
              </p>
            </div>
          )}
        </Panel>
      </div>
    </div>
  );
}

function blocksToYAML(blocks: Block[]): string {
  if (blocks.length === 0) return '';
  return blocks
    .map((b, i) => {
      const lines = [`- name: block-${i}`, `  kind: ${b.kind}`, '  config:'];
      for (const [k, v] of Object.entries(b.config)) {
        lines.push(`    ${k}: ${JSON.stringify(v)}`);
      }
      return lines.join('\n');
    })
    .join('\n');
}
