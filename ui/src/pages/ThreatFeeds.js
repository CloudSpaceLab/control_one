import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useCallback, useEffect, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { Badge, severityToVariant } from '../components/Badge';
import { EmptyState } from '../components/EmptyState';
import { ConfirmModal } from '../components/ConfirmModal';
// FEED_CATALOG describes every feed the platform knows how to fetch. The UI
// adapts the form fields to each entry — built-in feeds need only a name,
// commercial feeds want an API key, custom feeds want a URL. Adding a new
// feed type is a one-line entry here plus a case in the Go SourceFromConfig.
const FEED_CATALOG = [
    {
        type: 'spamhaus_drop',
        label: 'Spamhaus DROP',
        description: 'Hijacked / malicious netblocks. Free, no key. Updated daily.',
        needsURL: 'optional',
        needsAPIKey: false,
        defaultURL: 'https://www.spamhaus.org/drop/drop.txt',
    },
    {
        type: 'spamhaus_edrop',
        label: 'Spamhaus EDROP',
        description: 'Extended DROP list. Same format as DROP but wider coverage.',
        needsURL: 'optional',
        needsAPIKey: false,
        defaultURL: 'https://www.spamhaus.org/drop/edrop.txt',
    },
    {
        type: 'firehol_l1',
        label: 'FireHOL Level 1',
        description: 'Curated aggregate of community blocklists. Low false-positive.',
        needsURL: 'optional',
        needsAPIKey: false,
        defaultURL: 'https://iplists.firehol.org/files/firehol_level1.netset',
    },
    {
        type: 'tor_exit',
        label: 'Tor exit nodes',
        description: 'Exit-node IPs. Useful as a separate signal, not always malicious.',
        needsURL: 'optional',
        needsAPIKey: false,
        defaultURL: 'https://www.dan.me.uk/torlist/?exit',
    },
    {
        type: 'abuseipdb',
        label: 'AbuseIPDB blocklist',
        description: 'Confidence-scored bad IPs. API key required.',
        needsURL: 'never',
        needsAPIKey: true,
    },
    {
        type: 'otx',
        label: 'AlienVault OTX',
        description: 'Pulse-based community intelligence. API key required.',
        needsURL: 'never',
        needsAPIKey: true,
    },
    {
        type: 'custom_lines',
        label: 'Custom — line list',
        description: 'Any URL with one IP/CIDR per line. Comments via # or ; allowed.',
        needsURL: 'required',
        needsAPIKey: false,
    },
    {
        type: 'custom_spamhaus',
        label: 'Custom — Spamhaus format',
        description: 'Any URL using the "<cidr> ; evidence" format.',
        needsURL: 'required',
        needsAPIKey: false,
    },
];
const FEED_META = FEED_CATALOG.reduce((acc, m) => ({ ...acc, [m.type]: m }), {});
function emptyForm(tenantId) {
    return {
        tenant_id: tenantId,
        name: '',
        feed_type: 'spamhaus_drop',
        url: FEED_META.spamhaus_drop.defaultURL,
        score_floor: 50,
        refresh_seconds: 3600,
        enabled: true,
    };
}
export function ThreatFeeds() {
    const client = useApiClient();
    const { data: tenants } = useTenants({ limit: 50, offset: 0 });
    const [tenantId, setTenantId] = useState('');
    const [feeds, setFeeds] = useState([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState(null);
    const [form, setForm] = useState(emptyForm(''));
    const [submitting, setSubmitting] = useState(false);
    const [confirmId, setConfirmId] = useState(null);
    useEffect(() => {
        if (!tenantId && tenants[0]?.id)
            setTenantId(tenants[0].id);
    }, [tenants, tenantId]);
    useEffect(() => {
        setForm((prev) => ({ ...prev, tenant_id: tenantId }));
    }, [tenantId]);
    const refresh = useCallback(async () => {
        if (!tenantId)
            return;
        setLoading(true);
        try {
            const resp = await client.listThreatFeeds(tenantId);
            setFeeds(resp.data);
            setError(null);
        }
        catch (err) {
            setError(err instanceof Error ? err.message : 'load failed');
        }
        finally {
            setLoading(false);
        }
    }, [client, tenantId]);
    useEffect(() => {
        refresh();
    }, [refresh]);
    const submit = async (e) => {
        e.preventDefault();
        if (!tenantId)
            return;
        setSubmitting(true);
        setError(null);
        try {
            await client.createThreatFeed({ ...form, tenant_id: tenantId });
            setForm(emptyForm(tenantId));
            refresh();
        }
        catch (err) {
            setError(err instanceof Error ? err.message : 'create failed');
        }
        finally {
            setSubmitting(false);
        }
    };
    const toggleEnabled = async (feed) => {
        await client.updateThreatFeed(feed.id, { enabled: !feed.enabled });
        refresh();
    };
    const remove = async () => {
        if (!confirmId)
            return;
        await client.deleteThreatFeed(confirmId);
        setConfirmId(null);
        refresh();
    };
    const meta = FEED_META[form.feed_type];
    const onTypeChange = (type) => {
        const m = FEED_META[type];
        setForm((prev) => ({
            ...prev,
            feed_type: type,
            url: m.defaultURL ?? '',
            api_key: undefined,
        }));
    };
    return (_jsxs("section", { className: "dashboard-section", children: [_jsxs("header", { className: "dashboard-header", children: [_jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "Threat intelligence" }), _jsx("h2", { children: "Abuse IP data sources" }), _jsx("p", { className: "subtitle", children: "Choose which feeds to consume. Built-in lists are free; commercial feeds need an API key. Custom URLs are supported for in-house honeypots and partner shares." })] }), _jsx("select", { value: tenantId, onChange: (e) => setTenantId(e.target.value), "aria-label": "Tenant", children: tenants.map((t) => (_jsx("option", { value: t.id, children: t.name }, t.id))) })] }), error ? _jsx("p", { className: "error-banner", children: error }) : null, _jsxs("form", { onSubmit: submit, style: {
                    display: 'grid',
                    gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
                    gap: '0.75rem',
                    alignItems: 'end',
                    padding: '1rem',
                    background: 'rgba(255,255,255,0.025)',
                    borderRadius: 10,
                    border: '1px solid rgba(255,255,255,0.06)',
                    marginBottom: '1rem',
                }, children: [_jsxs("label", { children: ["Name", _jsx("input", { required: true, value: form.name, onChange: (e) => setForm({ ...form, name: e.target.value }), placeholder: "e.g. Spamhaus production" })] }), _jsxs("label", { children: ["Source", _jsx("select", { value: form.feed_type, onChange: (e) => onTypeChange(e.target.value), children: FEED_CATALOG.map((m) => (_jsx("option", { value: m.type, children: m.label }, m.type))) })] }), meta.needsURL !== 'never' ? (_jsxs("label", { children: ["URL", meta.needsURL === 'optional' ? ' (optional)' : '', _jsx("input", { required: meta.needsURL === 'required', value: form.url ?? '', onChange: (e) => setForm({ ...form, url: e.target.value }), placeholder: meta.defaultURL ?? 'https://example.com/list.txt' })] })) : null, meta.needsAPIKey ? (_jsxs("label", { children: ["API key", _jsx("input", { required: true, type: "password", value: form.api_key ?? '', onChange: (e) => setForm({ ...form, api_key: e.target.value }), placeholder: "paste key", autoComplete: "off" })] })) : null, _jsxs("label", { children: ["Score floor", _jsx("input", { type: "number", min: 0, max: 100, value: form.score_floor ?? 50, onChange: (e) => setForm({ ...form, score_floor: Number(e.target.value) }) })] }), _jsxs("label", { children: ["Refresh (s)", _jsx("input", { type: "number", min: 60, value: form.refresh_seconds ?? 3600, onChange: (e) => setForm({ ...form, refresh_seconds: Number(e.target.value) }) })] }), _jsx("button", { type: "submit", className: "primary-button", disabled: submitting || !tenantId, children: submitting ? 'Adding…' : 'Add source' }), _jsx("p", { className: "muted", style: { gridColumn: '1 / -1', margin: 0, fontSize: '0.8rem' }, children: meta.description })] }), loading && feeds.length === 0 ? (_jsx("p", { className: "muted", children: "Loading sources\u2026" })) : feeds.length === 0 ? (_jsx(EmptyState, { title: "No threat sources configured yet", description: "Add Spamhaus DROP for free baseline coverage, or paste a custom URL from your honeypot or SOC team." })) : (_jsxs("table", { className: "data-table", style: { width: '100%' }, children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Name" }), _jsx("th", { children: "Type" }), _jsx("th", { children: "Last refresh" }), _jsx("th", { children: "Indicators" }), _jsx("th", { children: "Status" }), _jsx("th", { children: "Score" }), _jsx("th", { children: "Refresh" }), _jsx("th", { children: "Enabled" }), _jsx("th", {})] }) }), _jsx("tbody", { children: feeds.map((f) => (_jsxs("tr", { children: [_jsxs("td", { children: [_jsx("strong", { children: f.name }), f.url ? _jsx("small", { style: { display: 'block', color: 'var(--text-muted)' }, children: f.url }) : null] }), _jsx("td", { children: FEED_META[f.feed_type]?.label ?? f.feed_type }), _jsx("td", { children: f.last_refreshed_at ? new Date(f.last_refreshed_at).toLocaleString() : '—' }), _jsx("td", { children: f.last_indicator_count.toLocaleString() }), _jsxs("td", { children: [f.last_status === 'ok' ? _jsx(Badge, { variant: "success", size: "sm", children: "healthy" })
                                            : f.last_status === 'error' ? _jsx(Badge, { variant: "error", size: "sm", children: "error" })
                                                : _jsx(Badge, { variant: "neutral", size: "sm", children: "pending" }), f.last_error ? _jsx("small", { style: { display: 'block', color: 'var(--text-muted)', marginTop: 2 }, children: f.last_error }) : null] }), _jsx("td", { children: _jsxs(Badge, { variant: severityToVariant(f.score_floor >= 80 ? 'high' : f.score_floor >= 50 ? 'medium' : 'low'), size: "sm", children: ["\u2265 ", f.score_floor] }) }), _jsxs("td", { children: [Math.round(f.refresh_seconds / 60), " min"] }), _jsx("td", { children: _jsx("button", { type: "button", className: f.enabled ? 'primary-button' : 'secondary-button', onClick: () => toggleEnabled(f), children: f.enabled ? 'On' : 'Off' }) }), _jsx("td", { children: _jsx("button", { type: "button", className: "secondary-button", onClick: () => setConfirmId(f.id), children: "Remove" }) })] }, f.id))) })] })), _jsx(ConfirmModal, { open: confirmId !== null, title: "Remove threat source?", body: "The platform will stop pulling from this feed on the next refresh tick. Existing indicators in cache stay until they age out.", variant: "danger", confirmLabel: "Remove", onConfirm: remove, onCancel: () => setConfirmId(null) })] }));
}
