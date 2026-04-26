import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useMemo, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { useToast } from '../providers/ToastProvider';
const BLOCK_TEMPLATES = {
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
export function RuleBuilder() {
    const client = useApiClient();
    const { data: tenants } = useTenants({ limit: 50, offset: 0 });
    const { showToast } = useToast();
    const [tenantId, setTenantId] = useState('');
    const [blocks, setBlocks] = useState([]);
    const [simResult, setSimResult] = useState(null);
    const [simBusy, setSimBusy] = useState(false);
    const [error, setError] = useState(null);
    const [dragIdx, setDragIdx] = useState(null);
    const addBlock = (kind) => {
        const template = BLOCK_TEMPLATES[kind];
        setBlocks([...blocks, { ...template, id: crypto.randomUUID(), config: { ...template.config } }]);
    };
    const updateConfig = (id, key, value) => {
        setBlocks(blocks.map((b) => (b.id === id ? { ...b, config: { ...b.config, [key]: value } } : b)));
    };
    const removeBlock = (id) => setBlocks(blocks.filter((b) => b.id !== id));
    const onDragStart = (i) => setDragIdx(i);
    const onDragOver = (e) => e.preventDefault();
    const onDrop = (i) => {
        if (dragIdx === null || dragIdx === i)
            return;
        const next = [...blocks];
        const [moved] = next.splice(dragIdx, 1);
        next.splice(i, 0, moved);
        setBlocks(next);
        setDragIdx(null);
    };
    const yaml = useMemo(() => blocksToYAML(blocks), [blocks]);
    const simulate = async () => {
        if (!tenantId || blocks.length === 0)
            return;
        const block = blocks[blocks.length - 1];
        setSimBusy(true);
        setError(null);
        try {
            const result = await client.simulateRule({
                tenant_id: tenantId,
                rule_type: block.kind,
                window_days: 30,
                rule: block.config,
            });
            setSimResult(result);
        }
        catch (err) {
            setError(err instanceof Error ? err.message : 'simulate failed');
        }
        finally {
            setSimBusy(false);
        }
    };
    const promote = async () => {
        if (!tenantId || blocks.length === 0)
            return;
        const block = blocks[blocks.length - 1];
        if (block.kind !== 'port') {
            showToast('Promote supported for port rules in this build.', 'info');
            return;
        }
        try {
            const cfg = block.config;
            const payload = {
                tenant_id: tenantId,
                name: String(cfg.name ?? `port-${cfg.port}-${cfg.expected_state}`),
                port: Number(cfg.port),
                protocol: String(cfg.protocol),
                expected_state: String(cfg.expected_state),
                severity: String(cfg.severity ?? 'medium'),
                enabled: true,
            };
            await client.createPortRule(payload);
            showToast('Rule promoted.', 'success');
        }
        catch (err) {
            showToast(err instanceof Error ? err.message : 'Promote failed', 'error');
        }
    };
    return (_jsxs("section", { className: "dashboard-section", children: [_jsxs("header", { className: "dashboard-header", children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "Builder" }), _jsx("h2", { children: "Visual rule builder" }), _jsx("p", { className: "subtitle", children: "Compose blocks, simulate against history, then promote." })] }), _jsxs("select", { value: tenantId, onChange: (e) => setTenantId(e.target.value), "aria-label": "Tenant", children: [_jsx("option", { value: "", children: "Choose tenant\u2026" }), tenants.map((t) => _jsx("option", { value: t.id, children: t.name }, t.id))] })] }), _jsxs("div", { style: { display: 'flex', gap: '0.5rem', marginBottom: '1rem', flexWrap: 'wrap' }, children: [_jsx("button", { type: "button", className: "primary-button", onClick: () => addBlock('port'), children: "+ Port block" }), _jsx("button", { type: "button", className: "primary-button", onClick: () => addBlock('log'), children: "+ Log block" }), _jsx("button", { type: "button", className: "primary-button", onClick: () => addBlock('compliance'), children: "+ Compliance block" })] }), _jsxs("div", { style: { display: 'grid', gridTemplateColumns: 'minmax(0, 2fr) minmax(0, 1fr)', gap: '1rem' }, children: [_jsx("ol", { style: { listStyle: 'none', padding: 0 }, onDragOver: onDragOver, children: blocks.length === 0 ? (_jsx("p", { className: "muted", children: "No blocks yet. Add one above." })) : (blocks.map((b, i) => (_jsxs("li", { draggable: true, onDragStart: () => onDragStart(i), onDragOver: onDragOver, onDrop: () => onDrop(i), style: {
                                border: '1px solid var(--border, #333)',
                                borderRadius: 6,
                                padding: '0.75rem',
                                marginBottom: '0.5rem',
                                background: 'var(--surface, #151515)',
                                cursor: 'move',
                            }, children: [_jsxs("div", { style: { display: 'flex', justifyContent: 'space-between', alignItems: 'center' }, children: [_jsxs("strong", { children: [b.kind.toUpperCase(), " block"] }), _jsx("button", { type: "button", className: "secondary-button", onClick: () => removeBlock(b.id), children: "\u00D7" })] }), _jsx("div", { style: { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(120px, 1fr))', gap: '0.5rem', marginTop: '0.5rem' }, children: Object.entries(b.config).map(([k, v]) => (_jsxs("label", { style: { fontSize: '0.85rem' }, children: [k, _jsx("input", { value: String(v), onChange: (e) => {
                                                    const raw = e.target.value;
                                                    const parsed = !isNaN(Number(raw)) && raw !== '' ? Number(raw) : raw;
                                                    updateConfig(b.id, k, parsed);
                                                } })] }, k))) })] }, b.id)))) }), _jsxs("aside", { style: { background: 'var(--surface, #151515)', border: '1px solid var(--border, #333)', borderRadius: 6, padding: '0.75rem' }, children: [_jsx("h3", { style: { marginTop: 0 }, children: "YAML preview" }), _jsx("pre", { style: { fontSize: '0.8rem', overflow: 'auto' }, children: yaml || '# no blocks' }), _jsxs("div", { style: { display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }, children: [_jsx("button", { type: "button", className: "primary-button", onClick: simulate, disabled: !tenantId || blocks.length === 0 || simBusy, children: simBusy ? 'Simulating…' : 'Simulate' }), _jsx("button", { type: "button", className: "primary-button", onClick: promote, disabled: !tenantId || blocks.length === 0, children: "Promote" })] }), error ? _jsx("p", { className: "error-banner", children: error }) : null, simResult ? (_jsxs("div", { style: { marginTop: '0.5rem' }, children: [_jsx("strong", { children: "Simulation" }), _jsx("p", { className: "muted", style: { fontSize: '0.85rem' }, children: simResult.summary }), _jsxs("p", { style: { fontSize: '0.85rem' }, children: ["Pass: ", simResult.nodes_would_pass, " \u00B7 Fail: ", simResult.nodes_would_fail] })] })) : null] })] })] }));
}
function blocksToYAML(blocks) {
    if (blocks.length === 0)
        return '';
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
