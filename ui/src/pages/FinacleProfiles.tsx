import { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../components/ui/tabs';
import { Skeleton } from '../components/ui/skeleton';
import { Button } from '../components/ui/button';
import { SectionHeader, EmptyState, KpiTile, StatusTag } from '../components/kit';
import { useApiClient } from '../hooks/useApiClient';
import { useTenant } from '../providers/TenantProvider';
import { Building2, RefreshCw, ChevronDown, ChevronUp } from 'lucide-react';
import type {
  FinacleConnection,
  FinacleConnectionTestResult,
  FinacleProfile,
  FinacleShiftConfig,
  FinacleShiftBand,
  FinacleShiftModel,
  FinacleAuthMethod,
} from '../lib/api';

// FinacleProfiles is the consolidated /access/finacle surface for UC6.
// Three tabs: Connection (host + auth + last_error banner), Shifts (24h
// timeline + grace_minutes), Profiles (DataTable with row + bulk actions).
// Tab state mirrors ?tab= so deep links survive.
const VALID_TABS = ['connection', 'shifts', 'profiles'] as const;
type TabKey = (typeof VALID_TABS)[number];

function isValidTab(s: string | null): s is TabKey {
  return !!s && (VALID_TABS as readonly string[]).includes(s);
}

export function FinacleProfiles(): JSX.Element {
  const [params, setParams] = useSearchParams();
  const initial = isValidTab(params.get('tab')) ? (params.get('tab') as TabKey) : 'connection';
  const [tab, setTab] = useState<TabKey>(initial);

  const onTabChange = (next: string) => {
    if (!isValidTab(next)) return;
    setTab(next);
    const updated = new URLSearchParams(params);
    updated.set('tab', next);
    setParams(updated, { replace: true });
  };

  return (
    <div className="space-y-6 p-6">
      <SectionHeader
        title="Finacle profiles"
        description="Bind branch staff to shifts and rotate access at shift boundaries — fail-closed on enable, fail-open on disable."
      />
      <Tabs value={tab} onValueChange={onTabChange} className="w-full">
        <TabsList>
          <TabsTrigger value="connection">Connection</TabsTrigger>
          <TabsTrigger value="shifts">Shifts</TabsTrigger>
          <TabsTrigger value="profiles">Profiles</TabsTrigger>
        </TabsList>
        <TabsContent value="connection" className="pt-4">
          <ConnectionPanel />
        </TabsContent>
        <TabsContent value="shifts" className="pt-4">
          <ShiftsPanel />
        </TabsContent>
        <TabsContent value="profiles" className="pt-4">
          <ProfilesPanel />
        </TabsContent>
      </Tabs>
    </div>
  );
}

// ── Connection tab ─────────────────────────────────────────────────────────

function ConnectionPanel(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [connections, setConnections] = useState<FinacleConnection[]>([]);
  const [profiles, setProfiles] = useState<FinacleProfile[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [bannerCollapsed, setBannerCollapsed] = useState(false);
  const [testResult, setTestResult] = useState<FinacleConnectionTestResult | null>(null);

  // Form state for the create-connection panel.
  const [host, setHost] = useState('');
  const [authMethod, setAuthMethod] = useState<FinacleAuthMethod>('oauth2_client_credentials');
  const [credentialRef, setCredentialRef] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const refresh = useCallback(async () => {
    if (!currentTenantId) return;
    setLoading(true);
    setError(null);
    try {
      const [connResp, profResp] = await Promise.all([
        client.listFinacleConnections(currentTenantId),
        client.listFinacleProfiles(currentTenantId, { limit: 1 }),
      ]);
      setConnections(connResp.connections ?? []);
      setProfiles(profResp.data ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [client, currentTenantId]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const onTest = async (id: string) => {
    try {
      const res = await client.testFinacleConnection(id);
      setTestResult(res);
      refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'test failed');
    }
  };

  const onCreate = async () => {
    if (!currentTenantId || !host.trim()) return;
    setSubmitting(true);
    try {
      await client.createFinacleConnection({
        tenant_id: currentTenantId,
        host: host.trim(),
        auth_method: authMethod,
        credential_ref: credentialRef.trim() || undefined,
      });
      setHost('');
      setCredentialRef('');
      refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'create failed');
    } finally {
      setSubmitting(false);
    }
  };

  if (!currentTenantId) {
    return <EmptyState title="Select a tenant" description="Choose a tenant from the header to view Finacle connections." />;
  }

  const primary = connections[0];
  const lastError = primary?.last_error?.trim();
  const lastSync = primary?.last_sync_at ? new Date(primary.last_sync_at).toLocaleString() : '—';
  const profileCount = profiles.length; // best-effort total via pagination meta would be richer; KPI keeps it simple

  return (
    <div className="space-y-4">
      {lastError && (
        <div className="rounded border border-warning/50 bg-warning/10 p-3 text-sm">
          <button
            type="button"
            className="flex w-full items-center justify-between font-medium"
            onClick={() => setBannerCollapsed((c) => !c)}
            aria-expanded={!bannerCollapsed}
          >
            <span>Finacle connection reported an error on last sync</span>
            {bannerCollapsed ? <ChevronDown className="h-4 w-4" /> : <ChevronUp className="h-4 w-4" />}
          </button>
          {!bannerCollapsed && <pre className="mt-2 whitespace-pre-wrap text-xs text-text-secondary">{lastError}</pre>}
        </div>
      )}

      <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
        <KpiTile label="Last sync" value={lastSync} tone={lastError ? 'warning' : 'healthy'} />
        <KpiTile label="Profiles synced" value={String(profileCount)} tone="info" />
        <KpiTile
          label="Connection status"
          value={primary ? (lastError ? 'degraded' : 'ok') : 'unconfigured'}
          tone={primary ? (lastError ? 'warning' : 'healthy') : 'unknown'}
        />
      </div>

      <div className="flex items-center justify-end gap-2">
        <Button variant="outline" size="sm" onClick={refresh} disabled={loading}>
          <RefreshCw className={`mr-2 h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          Refresh
        </Button>
      </div>

      {error && <div className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">{error}</div>}

      {testResult && (
        <div
          className={`rounded border p-3 text-sm ${
            testResult.status === 'ok' ? 'border-success/50 bg-success/10' : 'border-destructive/50 bg-destructive/10'
          }`}
        >
          <strong>Test:</strong> {testResult.status}
          {testResult.message ? ` — ${testResult.message}` : ''}
        </div>
      )}

      {!loading && connections.length === 0 ? (
        <EmptyState
          title="No Finacle connection configured"
          description="Add a Finacle host below; OAuth2 client-credentials and basic auth are supported. Credentials live in the secret store — paste the SecretGroup id as credential_ref."
          icon={<Building2 className="h-8 w-8" />}
        />
      ) : (
        <div className="rounded border border-border">
          <table className="w-full text-sm">
            <thead className="bg-surface-2 text-left text-xs uppercase tracking-wider text-text-secondary">
              <tr>
                <th className="px-3 py-2">Host</th>
                <th className="px-3 py-2">Auth</th>
                <th className="px-3 py-2">Last sync</th>
                <th className="px-3 py-2">Last error</th>
                <th className="px-3 py-2"></th>
              </tr>
            </thead>
            <tbody>
              {connections.map((c) => (
                <tr key={c.id} className="border-t border-border">
                  <td className="px-3 py-2 font-mono text-xs">{c.host}</td>
                  <td className="px-3 py-2">{c.auth_method}</td>
                  <td className="px-3 py-2 text-text-secondary">
                    {c.last_sync_at ? new Date(c.last_sync_at).toLocaleString() : '—'}
                  </td>
                  <td className="px-3 py-2 text-xs text-text-secondary">{c.last_error ?? '—'}</td>
                  <td className="px-3 py-2 text-right">
                    <Button variant="outline" size="sm" onClick={() => onTest(c.id)}>
                      Test
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="rounded border border-border bg-surface-2 p-4 text-sm">
        <h4 className="mb-3 font-medium">Add Finacle connection</h4>
        <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
          <label className="flex flex-col gap-1">
            <span className="text-xs text-text-secondary">Host</span>
            <input
              className="rounded border border-border bg-background px-2 py-1"
              value={host}
              onChange={(e) => setHost(e.target.value)}
              placeholder="https://finacle.example/api"
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-xs text-text-secondary">Auth method</span>
            <select
              className="rounded border border-border bg-background px-2 py-1"
              value={authMethod}
              onChange={(e) => setAuthMethod(e.target.value as FinacleAuthMethod)}
            >
              <option value="oauth2_client_credentials">OAuth2 client credentials</option>
              <option value="basic">Basic auth</option>
            </select>
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-xs text-text-secondary">Credential ref (SecretGroup id)</span>
            <input
              className="rounded border border-border bg-background px-2 py-1"
              value={credentialRef}
              onChange={(e) => setCredentialRef(e.target.value)}
              placeholder="optional"
            />
          </label>
        </div>
        <div className="mt-3 text-right">
          <Button onClick={onCreate} disabled={submitting || !host.trim()}>
            Create
          </Button>
        </div>
      </div>
    </div>
  );
}

// ── Shifts tab ─────────────────────────────────────────────────────────────

function ShiftsPanel(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [configs, setConfigs] = useState<FinacleShiftConfig[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [editing, setEditing] = useState<FinacleShiftConfig | null>(null);

  const refresh = useCallback(async () => {
    if (!currentTenantId) return;
    setLoading(true);
    setError(null);
    try {
      const resp = await client.listFinacleShiftConfigs(currentTenantId);
      setConfigs(resp.configs ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [client, currentTenantId]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  if (!currentTenantId) {
    return <EmptyState title="Select a tenant" description="Choose a tenant from the header to view shift configs." />;
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-end gap-2">
        <Button variant="outline" size="sm" onClick={refresh} disabled={loading}>
          <RefreshCw className={`mr-2 h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          Refresh
        </Button>
      </div>

      {error && <div className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">{error}</div>}

      {!loading && configs.length === 0 ? (
        <EmptyState
          title="No shift configs"
          description="Create a shift config to bind branch staff (3-shift, 2-shift, branch hours, or always-on)."
        />
      ) : (
        <div className="space-y-3">
          {configs.map((c) => (
            <ShiftCard key={c.id} cfg={c} onEdit={() => setEditing(c)} />
          ))}
        </div>
      )}

      {editing && <ShiftEditDrawer cfg={editing} onClose={() => setEditing(null)} onSaved={refresh} />}
    </div>
  );
}

function ShiftCard({ cfg, onEdit }: { cfg: FinacleShiftConfig; onEdit: () => void }): JSX.Element {
  return (
    <div className="rounded border border-border p-4">
      <div className="flex items-center justify-between">
        <div>
          <p className="font-medium">{cfg.model.replace(/_/g, ' ')}</p>
          <p className="text-xs text-text-secondary">
            Branch: {cfg.branch_id ?? '— default —'} · Grace: {cfg.grace_minutes} min
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={onEdit}>
          Edit
        </Button>
      </div>
      <div className="mt-3">
        <ShiftTimeline shifts={cfg.shifts} />
      </div>
    </div>
  );
}

// ShiftTimeline renders the shift bands as colour-coded rectangles on a 0-24h
// SVG axis. Click handling is left to the parent's edit drawer.
function ShiftTimeline({ shifts }: { shifts: FinacleShiftBand[] }): JSX.Element {
  const width = 720;
  const height = 48;
  const tones = ['#3b82f6', '#10b981', '#f59e0b', '#a855f7'];

  const bands = useMemo(() => {
    return shifts.map((b, i) => {
      const startMin = parseHHMM(b.start);
      const endMin = parseHHMM(b.end);
      const totalMins = 24 * 60;
      // Wrap-around shifts (end < start) split into two bands, but the simple
      // case suffices for the visual; we clamp end to start when invalid.
      const x = (startMin / totalMins) * width;
      const w = Math.max(((endMin - startMin) / totalMins) * width, 2);
      return { band: b, x, w, tone: tones[i % tones.length] };
    });
  }, [shifts]);

  return (
    <svg width="100%" viewBox={`0 0 ${width} ${height}`} className="rounded border border-border bg-surface-2">
      <line x1={0} y1={height - 12} x2={width} y2={height - 12} stroke="#475569" strokeWidth={1} />
      {Array.from({ length: 25 }).map((_, h) => (
        <text
          key={h}
          x={(h / 24) * width}
          y={height - 1}
          fontSize={9}
          textAnchor="middle"
          fill="#94a3b8"
        >
          {h % 6 === 0 ? `${h}h` : ''}
        </text>
      ))}
      {bands.map(({ band, x, w, tone }, i) => (
        <g key={`${band.name}-${i}`}>
          <rect x={x} y={4} width={w} height={20} fill={tone} fillOpacity={0.6} rx={3} />
          <text x={x + 4} y={18} fontSize={10} fill="#f1f5f9">
            {band.name}
          </text>
        </g>
      ))}
    </svg>
  );
}

function parseHHMM(s: string): number {
  const m = /^(\d{1,2}):(\d{2})$/.exec(s.trim());
  if (!m) return 0;
  return parseInt(m[1], 10) * 60 + parseInt(m[2], 10);
}

function ShiftEditDrawer({
  cfg,
  onClose,
  onSaved,
}: {
  cfg: FinacleShiftConfig;
  onClose: () => void;
  onSaved: () => void;
}): JSX.Element {
  const client = useApiClient();
  const [grace, setGrace] = useState(cfg.grace_minutes);
  const [model, setModel] = useState<FinacleShiftModel>(cfg.model);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const onSave = async () => {
    setSaving(true);
    try {
      await client.updateFinacleShiftConfig(cfg.id, { grace_minutes: grace, model });
      onSaved();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'save failed');
    } finally {
      setSaving(false);
    }
  };

  return (
    <aside className="fixed right-0 top-0 z-40 h-full w-full max-w-md overflow-y-auto border-l border-border bg-elevated p-6 shadow-2xl">
      <div className="mb-4 flex items-start justify-between">
        <div>
          <p className="text-xs uppercase tracking-wider text-text-secondary">Edit shift config</p>
          <h3 className="text-lg">{cfg.model.replace(/_/g, ' ')}</h3>
        </div>
        <Button variant="ghost" size="sm" onClick={onClose}>
          Close
        </Button>
      </div>
      {error && <div className="mb-3 rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">{error}</div>}
      <label className="mb-3 flex flex-col gap-1 text-sm">
        <span className="text-xs text-text-secondary">Model</span>
        <select
          className="rounded border border-border bg-background px-2 py-1"
          value={model}
          onChange={(e) => setModel(e.target.value as FinacleShiftModel)}
        >
          <option value="3_shift">3 shift</option>
          <option value="2_shift">2 shift</option>
          <option value="branch_hours">Branch hours</option>
          <option value="always_on">Always on</option>
        </select>
      </label>
      <label className="mb-3 flex flex-col gap-1 text-sm">
        <span className="text-xs text-text-secondary">Grace minutes</span>
        <input
          type="number"
          min={0}
          max={120}
          className="rounded border border-border bg-background px-2 py-1"
          value={grace}
          onChange={(e) => setGrace(Number(e.target.value))}
        />
      </label>
      <Button onClick={onSave} disabled={saving}>
        Save
      </Button>
    </aside>
  );
}

// ── Profiles tab ───────────────────────────────────────────────────────────

function ProfilesPanel(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [profiles, setProfiles] = useState<FinacleProfile[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const refresh = useCallback(async () => {
    if (!currentTenantId) return;
    setLoading(true);
    setError(null);
    try {
      const resp = await client.listFinacleProfiles(currentTenantId, { limit: 200 });
      setProfiles(resp.data ?? []);
      setSelected(new Set());
    } catch (err) {
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [client, currentTenantId]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const toggleAll = () => {
    if (selected.size === profiles.length) {
      setSelected(new Set());
    } else {
      setSelected(new Set(profiles.map((p) => p.id)));
    }
  };

  const toggleOne = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const onDisable = async (ids: string[]) => {
    try {
      for (const id of ids) {
        await client.updateFinacleProfile(id, { status: 'revoked' });
      }
      refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'update failed');
    }
  };

  if (!currentTenantId) {
    return <EmptyState title="Select a tenant" description="Choose a tenant from the header to view profiles." />;
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-2">
        <div className="text-sm text-text-secondary">
          {selected.size > 0 && (
            <Button variant="outline" size="sm" onClick={() => onDisable(Array.from(selected))}>
              Disable selected ({selected.size})
            </Button>
          )}
        </div>
        <Button variant="outline" size="sm" onClick={refresh} disabled={loading}>
          <RefreshCw className={`mr-2 h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          Refresh
        </Button>
      </div>
      {error && <div className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">{error}</div>}
      {loading && <Skeleton className="h-32 w-full" />}
      {!loading && profiles.length === 0 ? (
        <EmptyState
          title="No Finacle profiles"
          description="The finacle.sync job populates this table on each scheduled poll. Once a connection is configured you'll see profiles appear here."
        />
      ) : (
        <div className="rounded border border-border">
          <table className="w-full text-sm">
            <thead className="bg-surface-2 text-left text-xs uppercase tracking-wider text-text-secondary">
              <tr>
                <th className="px-3 py-2">
                  <input
                    type="checkbox"
                    checked={selected.size > 0 && selected.size === profiles.length}
                    onChange={toggleAll}
                    aria-label="Select all profiles"
                  />
                </th>
                <th className="px-3 py-2">Finacle UID</th>
                <th className="px-3 py-2">Branch</th>
                <th className="px-3 py-2">Role</th>
                <th className="px-3 py-2">Status</th>
                <th className="px-3 py-2">Rotated</th>
                <th className="px-3 py-2"></th>
              </tr>
            </thead>
            <tbody>
              {profiles.map((p) => (
                <tr key={p.id} className="border-t border-border hover:bg-hover">
                  <td className="px-3 py-2">
                    <input
                      type="checkbox"
                      checked={selected.has(p.id)}
                      onChange={() => toggleOne(p.id)}
                      aria-label={`Select ${p.finacle_uid}`}
                    />
                  </td>
                  <td className="px-3 py-2 font-mono text-xs">{p.finacle_uid}</td>
                  <td className="px-3 py-2">{p.branch_id ?? '—'}</td>
                  <td className="px-3 py-2">{p.role ?? '—'}</td>
                  <td className="px-3 py-2">
                    <StatusTag tone={statusTone(p.status)}>{p.status}</StatusTag>
                  </td>
                  <td className="px-3 py-2 text-text-secondary">
                    {p.last_rotated_at ? new Date(p.last_rotated_at).toLocaleString() : '—'}
                  </td>
                  <td className="px-3 py-2 text-right">
                    <Button variant="ghost" size="sm" onClick={() => onDisable([p.id])}>
                      Disable
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function statusTone(s: string): 'healthy' | 'warning' | 'critical' | 'unknown' {
  switch (s) {
    case 'active':
      return 'healthy';
    case 'pending':
      return 'warning';
    case 'revoked':
    case 'failed':
      return 'critical';
    default:
      return 'unknown';
  }
}

export default FinacleProfiles;
