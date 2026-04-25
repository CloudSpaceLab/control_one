import { useMemo, useState } from 'react';
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
    <section className="dashboard-section">
      <header className="dashboard-header">
        <div>
          <p className="eyebrow">Builder</p>
          <h2>Visual rule builder</h2>
          <p className="subtitle">Compose blocks, simulate against history, then promote.</p>
        </div>
        <select value={tenantId} onChange={(e) => setTenantId(e.target.value)} aria-label="Tenant">
          <option value="">Choose tenant…</option>
          {tenants.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
        </select>
      </header>

      <div style={{ display: 'flex', gap: '0.5rem', marginBottom: '1rem', flexWrap: 'wrap' }}>
        <button type="button" className="primary-button" onClick={() => addBlock('port')}>+ Port block</button>
        <button type="button" className="primary-button" onClick={() => addBlock('log')}>+ Log block</button>
        <button type="button" className="primary-button" onClick={() => addBlock('compliance')}>+ Compliance block</button>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 2fr) minmax(0, 1fr)', gap: '1rem' }}>
        <ol style={{ listStyle: 'none', padding: 0 }} onDragOver={onDragOver}>
          {blocks.length === 0 ? (
            <p className="muted">No blocks yet. Add one above.</p>
          ) : (
            blocks.map((b, i) => (
              <li
                key={b.id}
                draggable
                onDragStart={() => onDragStart(i)}
                onDragOver={onDragOver}
                onDrop={() => onDrop(i)}
                style={{
                  border: '1px solid var(--border, #333)',
                  borderRadius: 6,
                  padding: '0.75rem',
                  marginBottom: '0.5rem',
                  background: 'var(--surface, #151515)',
                  cursor: 'move',
                }}
              >
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <strong>{b.kind.toUpperCase()} block</strong>
                  <button type="button" className="secondary-button" onClick={() => removeBlock(b.id)}>×</button>
                </div>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(120px, 1fr))', gap: '0.5rem', marginTop: '0.5rem' }}>
                  {Object.entries(b.config).map(([k, v]) => (
                    <label key={k} style={{ fontSize: '0.85rem' }}>
                      {k}
                      <input
                        value={String(v)}
                        onChange={(e) => {
                          const raw = e.target.value;
                          const parsed = !isNaN(Number(raw)) && raw !== '' ? Number(raw) : raw;
                          updateConfig(b.id, k, parsed);
                        }}
                      />
                    </label>
                  ))}
                </div>
              </li>
            ))
          )}
        </ol>
        <aside style={{ background: 'var(--surface, #151515)', border: '1px solid var(--border, #333)', borderRadius: 6, padding: '0.75rem' }}>
          <h3 style={{ marginTop: 0 }}>YAML preview</h3>
          <pre style={{ fontSize: '0.8rem', overflow: 'auto' }}>{yaml || '# no blocks'}</pre>
          <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
            <button type="button" className="primary-button" onClick={simulate} disabled={!tenantId || blocks.length === 0 || simBusy}>
              {simBusy ? 'Simulating…' : 'Simulate'}
            </button>
            <button type="button" className="primary-button" onClick={promote} disabled={!tenantId || blocks.length === 0}>
              Promote
            </button>
          </div>
          {error ? <p className="error-banner">{error}</p> : null}
          {simResult ? (
            <div style={{ marginTop: '0.5rem' }}>
              <strong>Simulation</strong>
              <p className="muted" style={{ fontSize: '0.85rem' }}>{simResult.summary}</p>
              <p style={{ fontSize: '0.85rem' }}>
                Pass: {simResult.nodes_would_pass} · Fail: {simResult.nodes_would_fail}
              </p>
            </div>
          ) : null}
        </aside>
      </div>
    </section>
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
