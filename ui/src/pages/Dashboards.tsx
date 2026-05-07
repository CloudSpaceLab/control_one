import { FormEvent, useCallback, useEffect, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { useTenant } from '../providers/TenantProvider';
import { SectionHeader, Panel, EmptyState, SelectField } from '../components/kit';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import type {
  CustomDashboard,
  DashboardWidget,
  WidgetType,
  WidgetPayload,
  NodeSummary,
} from '../lib/api';

// Custom dashboards builder.
// Layout: left rail = list of user's dashboards + "New". Right pane =
// selected dashboard's widgets in a grid. Each widget renders a small
// preview based on widget_type; clicking opens an edit dialog.

const WIDGET_TYPES: { value: WidgetType; label: string; description: string }[] = [
  { value: 'db_query', label: 'DB query', description: 'Pull from pg_stat_statements / dm_exec_query_stats — top queries by rows or duration.' },
  { value: 'sys_resources', label: 'System resources', description: 'CPU / memory / disk over time, sourced from agent telemetry.' },
  { value: 'log_size', label: 'Log size', description: 'Total bytes of log lines forwarded from selected nodes.' },
  { value: 'network_bytes', label: 'Network bytes', description: 'Sum of bytes_in / bytes_out from connection events.' },
];

export function Dashboards(): JSX.Element {
  const client = useApiClient();
  const { data: tenants } = useTenants({ limit: 1, offset: 0 });
  const tenantId = tenants[0]?.id;

  const [dashboards, setDashboards] = useState<CustomDashboard[]>([]);
  const [selected, setSelected] = useState<CustomDashboard | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState('');
  const [editingWidget, setEditingWidget] = useState<DashboardWidget | null>(null);
  const [adding, setAdding] = useState(false);

  const refresh = useCallback(async () => {
    if (!tenantId) return;
    try {
      const list = await client.listDashboards(tenantId);
      setDashboards(list);
      if (list.length && !selected) {
        const detail = await client.getDashboard(list[0].id);
        setSelected(detail);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'load failed');
    }
  }, [client, tenantId, selected]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const handleCreate = async () => {
    if (!tenantId || !newName.trim()) return;
    try {
      const d = await client.createDashboard({ tenant_id: tenantId, name: newName.trim() });
      setDashboards((cur) => [d, ...cur]);
      setSelected(d);
      setCreating(false);
      setNewName('');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'create failed');
    }
  };

  const handleSelect = async (d: CustomDashboard) => {
    try {
      const detail = await client.getDashboard(d.id);
      setSelected(detail);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'load failed');
    }
  };

  const handleAddWidget = async (payload: WidgetPayload) => {
    if (!selected) return;
    try {
      await client.createWidget(selected.id, payload);
      const refreshed = await client.getDashboard(selected.id);
      setSelected(refreshed);
      setAdding(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'create widget failed');
    }
  };

  const handleSaveWidget = async (w: DashboardWidget) => {
    if (!selected) return;
    try {
      await client.updateWidget(selected.id, w.id, {
        title: w.title,
        widget_type: w.widget_type,
        spec: w.spec,
        node_ids: w.node_ids,
        refresh_seconds: w.refresh_seconds,
        sort_order: w.sort_order,
      });
      const refreshed = await client.getDashboard(selected.id);
      setSelected(refreshed);
      setEditingWidget(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'update widget failed');
    }
  };

  const handleDeleteWidget = async (id: string) => {
    if (!selected) return;
    try {
      await client.deleteWidget(selected.id, id);
      const refreshed = await client.getDashboard(selected.id);
      setSelected(refreshed);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'delete failed');
    }
  };

  const handleDeleteDashboard = async () => {
    if (!selected) return;
    if (!confirm(`Delete dashboard "${selected.name}"? This cannot be undone.`)) return;
    try {
      await client.deleteDashboard(selected.id);
      setSelected(null);
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'delete failed');
    }
  };

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="DETECT & RESPOND · CUSTOM DASHBOARDS"
        title="Build views that pull from the servers + metrics that matter to you"
        description="DB queries, system resources, log volume, network bytes — pick one or more nodes per widget, set a refresh interval, and data renders live."
        actions={
          <Button type="button" variant="primary" onClick={() => setCreating(true)}>
            New dashboard
          </Button>
        }
      />

      {error ? (
        <div className="rounded-lg border border-state-critical/30 bg-state-critical/10 px-4 py-3 text-sm text-state-critical">
          {error}
        </div>
      ) : null}

      {creating ? (
        <Panel padding="md" eyebrow="NEW DASHBOARD" title="Create dashboard" toneAccent="brand">
          <div className="flex gap-3">
            <Input
              autoFocus
              placeholder="Dashboard name"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
            />
            <Button type="button" variant="primary" onClick={handleCreate}>
              Create
            </Button>
            <Button type="button" variant="ghost" onClick={() => setCreating(false)}>
              Cancel
            </Button>
          </div>
        </Panel>
      ) : null}

      {dashboards.length === 0 ? (
        <EmptyState
          title="No dashboards yet"
          description="Create your first custom dashboard to track the metrics that matter to your team."
          action={
            <Button variant="primary" onClick={() => setCreating(true)}>
              Create dashboard
            </Button>
          }
        />
      ) : (
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-[240px_1fr]">
          <aside>
            <ul className="flex flex-col gap-1">
              {dashboards.map((d) => (
                <li key={d.id}>
                  <button
                    type="button"
                    onClick={() => handleSelect(d)}
                    className={
                      selected?.id === d.id
                        ? 'w-full rounded-md bg-brand-500/15 px-3 py-2 text-left text-sm font-medium text-brand-400 border border-brand-500/30'
                        : 'w-full rounded-md px-3 py-2 text-left text-sm text-foreground hover:bg-hover border border-transparent'
                    }
                  >
                    {d.name}
                  </button>
                </li>
              ))}
            </ul>
          </aside>
          <main className="flex flex-col gap-4">
            {selected ? (
              <>
                <div className="flex items-start justify-between gap-4">
                  <div>
                    <h3 className="font-display text-base font-semibold text-foreground">{selected.name}</h3>
                    <p className="mt-0.5 text-xs text-text-muted">
                      {selected.description || 'No description'} · {selected.widgets?.length ?? 0} widget(s)
                      {selected.shared ? ' · shared with tenant' : ' · private'}
                    </p>
                  </div>
                  <div className="flex shrink-0 gap-2">
                    <Button type="button" variant="primary" size="sm" onClick={() => setAdding(true)}>
                      Add widget
                    </Button>
                    <Button type="button" variant="danger" size="sm" onClick={handleDeleteDashboard}>
                      Delete dashboard
                    </Button>
                  </div>
                </div>

                {(selected.widgets ?? []).length === 0 ? (
                  <EmptyState
                    title="No widgets on this dashboard yet"
                    description="Click Add widget to pull data from one or more nodes."
                    action={
                      <Button variant="primary" size="sm" onClick={() => setAdding(true)}>
                        Add widget
                      </Button>
                    }
                  />
                ) : (
                  <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
                    {(selected.widgets ?? []).map((w) => (
                      <Panel key={w.id} padding="md">
                        <div className="flex items-baseline justify-between gap-2">
                          <h4 className="font-display text-sm font-semibold text-foreground">{w.title}</h4>
                          <span className="shrink-0 text-xs text-text-muted">
                            {WIDGET_TYPES.find((t) => t.value === w.widget_type)?.label}
                          </span>
                        </div>
                        <p className="text-xs text-text-muted">
                          {w.node_ids.length === 0 ? 'All nodes' : `${w.node_ids.length} node(s)`} · refresh every {w.refresh_seconds}s
                        </p>
                        <div className="flex gap-2 pt-1">
                          <Button type="button" variant="secondary" size="sm" onClick={() => setEditingWidget(w)}>
                            Edit
                          </Button>
                          <Button type="button" variant="ghost" size="sm" onClick={() => handleDeleteWidget(w.id)}>
                            Remove
                          </Button>
                        </div>
                      </Panel>
                    ))}
                  </div>
                )}

                {adding ? (
                  <WidgetEditor
                    onCancel={() => setAdding(false)}
                    onSave={handleAddWidget}
                  />
                ) : null}
                {editingWidget ? (
                  <WidgetEditor
                    initial={editingWidget}
                    onCancel={() => setEditingWidget(null)}
                    onSave={(payload) => handleSaveWidget({ ...editingWidget, ...payload, spec: payload.spec, node_ids: payload.node_ids })}
                  />
                ) : null}
              </>
            ) : (
              <EmptyState
                title="Pick a dashboard"
                description="Select one from the left, or create a new one."
                action={
                  <Button variant="primary" size="sm" onClick={() => setCreating(true)}>
                    Create dashboard
                  </Button>
                }
              />
            )}
          </main>
        </div>
      )}
    </div>
  );
}

interface EditorProps {
  initial?: DashboardWidget;
  onCancel: () => void;
  onSave: (payload: WidgetPayload) => void;
}

function WidgetEditor({ initial, onCancel, onSave }: EditorProps) {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [title, setTitle] = useState(initial?.title ?? '');
  const [widgetType, setWidgetType] = useState<WidgetType>(initial?.widget_type ?? 'sys_resources');
  const [refresh, setRefresh] = useState(initial?.refresh_seconds ?? 30);
  const [allNodes, setAllNodes] = useState<NodeSummary[]>([]);
  const [selectedNodeIDs, setSelectedNodeIDs] = useState<string[]>(initial?.node_ids ?? []);
  const [specJson, setSpecJson] = useState(JSON.stringify(initial?.spec ?? defaultSpec(widgetType), null, 2));
  const [nodeFilter, setNodeFilter] = useState('');
  const [rawJsonMode, setRawJsonMode] = useState(false);

  useEffect(() => {
    if (!initial) {
      setSpecJson(JSON.stringify(defaultSpec(widgetType), null, 2));
    }
  }, [widgetType, initial]);

  useEffect(() => {
    if (!currentTenantId) {
      setAllNodes([]);
      return;
    }
    client
      .listNodes({ tenantId: currentTenantId, limit: 200 })
      .then((r) => setAllNodes(r.data))
      .catch(() => {});
  }, [client, currentTenantId]);

  const parsedSpec = (): Record<string, unknown> => {
    try { return JSON.parse(specJson); } catch { return {}; }
  };

  const updateSpec = (key: string, value: unknown) => {
    const current = parsedSpec();
    setSpecJson(JSON.stringify({ ...current, [key]: value }, null, 2));
  };

  const filteredNodes = allNodes.filter((n) =>
    nodeFilter === '' || (n.hostname || n.id).toLowerCase().includes(nodeFilter.toLowerCase()),
  );

  const toggleNode = (id: string) => {
    setSelectedNodeIDs((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id],
    );
  };

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    let spec: Record<string, unknown> = {};
    try {
      spec = specJson.trim() ? JSON.parse(specJson) : {};
    } catch {
      alert('Spec is not valid JSON');
      return;
    }
    onSave({
      title: title.trim() || 'Untitled',
      widget_type: widgetType,
      spec,
      node_ids: selectedNodeIDs,
      refresh_seconds: Math.max(5, refresh),
      sort_order: initial?.sort_order ?? 0,
    });
  };

  return (
    <>
      <div className="fixed inset-0 z-40 bg-black/50 backdrop-blur-sm" onClick={onCancel} />
      <aside className="fixed inset-y-0 right-0 z-50 flex w-[min(560px,90vw)] flex-col overflow-auto border-l border-border-subtle bg-elevated shadow-[0_0_24px_var(--shadow)]">
        <div className="flex items-center justify-between border-b border-border-subtle px-6 py-4">
          <h3 className="font-display text-base font-semibold text-foreground">
            {initial ? 'Edit widget' : 'New widget'}
          </h3>
          <Button type="button" variant="ghost" size="sm" onClick={onCancel}>
            Close
          </Button>
        </div>
        <form onSubmit={submit} className="flex flex-col gap-4 p-6">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="widget-title">Title</Label>
            <Input
              id="widget-title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              required
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <SelectField
              id="widget-type"
              label="Widget type"
              value={widgetType}
              onChange={(e) => setWidgetType(e.target.value as WidgetType)}
            >
              {WIDGET_TYPES.map((t) => (
                <option key={t.value} value={t.value}>{t.label}</option>
              ))}
            </SelectField>
            <p className="text-xs text-text-muted">
              {WIDGET_TYPES.find((t) => t.value === widgetType)?.description}
            </p>
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="widget-refresh">Refresh interval (seconds)</Label>
            <Input
              id="widget-refresh"
              type="number"
              min={5}
              value={refresh}
              onChange={(e) => setRefresh(Number(e.target.value))}
            />
          </div>

          {/* Node checklist */}
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center justify-between">
              <Label>Servers</Label>
              {selectedNodeIDs.length > 0 && (
                <span className="rounded-full bg-brand-500/15 px-2 py-0.5 text-xs font-medium text-brand-400">
                  {selectedNodeIDs.length} selected
                </span>
              )}
            </div>
            <Input
              placeholder="Filter nodes…"
              value={nodeFilter}
              onChange={(e) => setNodeFilter(e.target.value)}
            />
            <div className="max-h-[180px] overflow-y-auto rounded-md border border-border-subtle bg-surface">
              <label className="flex cursor-pointer items-center gap-2 border-b border-border-subtle px-3 py-2 text-sm text-text-muted hover:bg-hover">
                <input
                  type="checkbox"
                  className="h-4 w-4 rounded border-border-subtle accent-brand-500"
                  checked={selectedNodeIDs.length === 0}
                  onChange={() => setSelectedNodeIDs([])}
                />
                <span className="italic">All nodes (tenant default)</span>
              </label>
              {filteredNodes.length === 0 ? (
                <p className="px-3 py-2 text-xs text-text-muted">
                  {allNodes.length === 0 ? 'No nodes registered.' : 'No nodes match filter.'}
                </p>
              ) : (
                filteredNodes.map((n) => (
                  <label
                    key={n.id}
                    className="flex cursor-pointer items-center gap-2 border-b border-border-subtle px-3 py-2 text-sm last:border-0 hover:bg-hover"
                  >
                    <input
                      type="checkbox"
                      className="h-4 w-4 rounded border-border-subtle accent-brand-500"
                      checked={selectedNodeIDs.includes(n.id)}
                      onChange={() => toggleNode(n.id)}
                    />
                    <span className="text-foreground">{n.hostname || n.id}</span>
                  </label>
                ))
              )}
            </div>
          </div>

          {/* Spec / configuration */}
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center justify-between">
              <Label>Configuration</Label>
              <button
                type="button"
                className="text-xs text-brand-400 hover:underline"
                onClick={() => setRawJsonMode((m) => !m)}
              >
                {rawJsonMode ? 'Use form' : 'Edit raw JSON'}
              </button>
            </div>
            {rawJsonMode ? (
              <textarea
                className="flex min-h-[120px] w-full rounded-md border border-border-subtle bg-surface px-3 py-2 font-mono text-xs text-foreground placeholder:text-text-muted focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:opacity-50"
                rows={8}
                value={specJson}
                onChange={(e) => setSpecJson(e.target.value)}
              />
            ) : (
              <SpecForm widgetType={widgetType} spec={parsedSpec()} onChange={updateSpec} />
            )}
          </div>

          <div className="flex gap-2 pt-1">
            <Button type="submit" variant="primary">
              {initial ? 'Save' : 'Create'}
            </Button>
            <Button type="button" variant="ghost" onClick={onCancel}>
              Cancel
            </Button>
          </div>
        </form>
      </aside>
    </>
  );
}

interface SpecFormProps {
  widgetType: WidgetType;
  spec: Record<string, unknown>;
  onChange: (key: string, value: unknown) => void;
}

function SpecForm({ widgetType, spec, onChange }: SpecFormProps) {
  switch (widgetType) {
    case 'sys_resources':
      return (
        <div className="flex flex-col gap-3">
          <SelectField
            label="Metric"
            value={String(spec.metric ?? 'cpu')}
            onChange={(e) => onChange('metric', e.target.value)}
          >
            <option value="cpu">CPU</option>
            <option value="memory">Memory</option>
            <option value="disk">Disk</option>
            <option value="network">Network</option>
          </SelectField>
          <SelectField
            label="Time range"
            value={String(spec.range ?? '1h')}
            onChange={(e) => onChange('range', e.target.value)}
          >
            <option value="1h">Last 1 hour</option>
            <option value="6h">Last 6 hours</option>
            <option value="24h">Last 24 hours</option>
            <option value="7d">Last 7 days</option>
          </SelectField>
        </div>
      );
    case 'log_size':
      return (
        <div className="flex flex-col gap-3">
          <SelectField
            label="Time range"
            value={String(spec.range ?? '24h')}
            onChange={(e) => onChange('range', e.target.value)}
          >
            <option value="1h">Last 1 hour</option>
            <option value="6h">Last 6 hours</option>
            <option value="24h">Last 24 hours</option>
            <option value="7d">Last 7 days</option>
          </SelectField>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="spec-source">Source program (optional)</Label>
            <Input
              id="spec-source"
              placeholder="e.g. nginx, sshd"
              value={String(spec.source_program ?? '')}
              onChange={(e) => onChange('source_program', e.target.value)}
            />
          </div>
        </div>
      );
    case 'network_bytes':
      return (
        <div className="flex flex-col gap-3">
          <SelectField
            label="Direction"
            value={String(spec.direction ?? 'both')}
            onChange={(e) => onChange('direction', e.target.value)}
          >
            <option value="in">Inbound</option>
            <option value="out">Outbound</option>
            <option value="both">Both</option>
          </SelectField>
          <SelectField
            label="Time range"
            value={String(spec.range ?? '1h')}
            onChange={(e) => onChange('range', e.target.value)}
          >
            <option value="1h">Last 1 hour</option>
            <option value="6h">Last 6 hours</option>
            <option value="24h">Last 24 hours</option>
            <option value="7d">Last 7 days</option>
          </SelectField>
        </div>
      );
    case 'db_query':
      return (
        <div className="flex flex-col gap-3">
          <SelectField
            label="Database engine"
            value={String(spec.engine ?? 'postgres')}
            onChange={(e) => onChange('engine', e.target.value)}
          >
            <option value="postgres">PostgreSQL</option>
            <option value="mysql">MySQL / MariaDB</option>
          </SelectField>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="spec-target">Target name</Label>
            <Input
              id="spec-target"
              placeholder="e.g. mydb"
              value={String(spec.target_name ?? '')}
              onChange={(e) => onChange('target_name', e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="spec-limit">Limit (rows)</Label>
            <Input
              id="spec-limit"
              type="number"
              min={1}
              max={1000}
              value={Number(spec.limit ?? 10)}
              onChange={(e) => onChange('limit', Number(e.target.value))}
            />
          </div>
          <SelectField
            label="Order by"
            value={String(spec.order_by ?? 'rows')}
            onChange={(e) => onChange('order_by', e.target.value)}
          >
            <option value="rows">Rows returned</option>
            <option value="duration">Duration</option>
            <option value="calls">Calls</option>
          </SelectField>
        </div>
      );
  }
}

function defaultSpec(t: WidgetType): Record<string, unknown> {
  switch (t) {
    case 'db_query':
      return { engine: 'postgres', target_name: '', limit: 10, order_by: 'rows' };
    case 'sys_resources':
      return { metric: 'cpu', range: '1h' };
    case 'log_size':
      return { range: '24h', source_program: '' };
    case 'network_bytes':
      return { direction: 'both', range: '1h' };
  }
}
