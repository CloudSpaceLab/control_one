import { FormEvent, useCallback, useEffect, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import { EmptyState } from '../components/EmptyState';
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

  const handleCreate = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
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
    <section className="dashboard-section">
      <header className="dashboard-header">
        <div>
          <p className="eyebrow">Custom dashboards</p>
          <h2>Build views that pull from the servers + metrics that matter to you</h2>
          <p className="subtitle">
            DB queries, system resources, log volume, network bytes — pick one or more nodes per
            widget, set a refresh interval, and the data renders live.
          </p>
        </div>
        <button type="button" className="primary-button" onClick={() => setCreating(true)}>
          New dashboard
        </button>
      </header>

      {error ? <p className="error-banner">{error}</p> : null}

      {creating ? (
        <form onSubmit={handleCreate} style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
          <input
            autoFocus
            placeholder="Dashboard name"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            style={{ flex: 1 }}
          />
          <button type="submit" className="primary-button">
            Create
          </button>
          <button type="button" className="secondary-button" onClick={() => setCreating(false)}>
            Cancel
          </button>
        </form>
      ) : null}

      {dashboards.length === 0 ? (
        <EmptyState
          title="No dashboards yet"
          description="Click New dashboard to author your first one. Add widgets that pull from the nodes + metrics you care about."
        />
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: '240px 1fr', gap: 24 }}>
          <aside>
            <ul style={{ listStyle: 'none', padding: 0, margin: 0 }}>
              {dashboards.map((d) => (
                <li key={d.id}>
                  <button
                    type="button"
                    onClick={() => handleSelect(d)}
                    className={selected?.id === d.id ? 'primary-button' : 'secondary-button'}
                    style={{ width: '100%', justifyContent: 'flex-start', marginBottom: 4 }}
                  >
                    {d.name}
                  </button>
                </li>
              ))}
            </ul>
          </aside>
          <main>
            {selected ? (
              <>
                <header style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 16 }}>
                  <div>
                    <h3>{selected.name}</h3>
                    <p style={{ color: 'var(--text-secondary)', fontSize: 13 }}>
                      {selected.description || 'No description'} · {selected.widgets?.length ?? 0} widget(s)
                      {selected.shared ? ' · shared with tenant' : ' · private'}
                    </p>
                  </div>
                  <div style={{ display: 'flex', gap: 8 }}>
                    <button type="button" className="primary-button" onClick={() => setAdding(true)}>
                      Add widget
                    </button>
                    <button type="button" className="secondary-button" onClick={handleDeleteDashboard}>
                      Delete dashboard
                    </button>
                  </div>
                </header>

                {(selected.widgets ?? []).length === 0 ? (
                  <EmptyState
                    title="No widgets on this dashboard yet"
                    description="Click Add widget to pull data from one or more nodes."
                  />
                ) : (
                  <div className="card-grid" style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))' }}>
                    {(selected.widgets ?? []).map((w) => (
                      <article key={w.id} className="card" style={{ padding: 16 }}>
                        <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
                          <h4>{w.title}</h4>
                          <small style={{ color: 'var(--text-secondary)' }}>
                            {WIDGET_TYPES.find((t) => t.value === w.widget_type)?.label}
                          </small>
                        </header>
                        <p style={{ fontSize: 13, color: 'var(--text-secondary)' }}>
                          {w.node_ids.length === 0 ? 'All nodes' : `${w.node_ids.length} node(s)`} · refresh every {w.refresh_seconds}s
                        </p>
                        <div style={{ display: 'flex', gap: 8, marginTop: 12 }}>
                          <button type="button" className="secondary-button" onClick={() => setEditingWidget(w)}>
                            Edit
                          </button>
                          <button type="button" className="secondary-button" onClick={() => handleDeleteWidget(w.id)}>
                            Remove
                          </button>
                        </div>
                      </article>
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
              />
            )}
          </main>
        </div>
      )}
    </section>
  );
}

interface EditorProps {
  initial?: DashboardWidget;
  onCancel: () => void;
  onSave: (payload: WidgetPayload) => void;
}

function WidgetEditor({ initial, onCancel, onSave }: EditorProps) {
  const client = useApiClient();
  const [title, setTitle] = useState(initial?.title ?? '');
  const [widgetType, setWidgetType] = useState<WidgetType>(initial?.widget_type ?? 'sys_resources');
  const [refresh, setRefresh] = useState(initial?.refresh_seconds ?? 30);
  const [allNodes, setAllNodes] = useState<NodeSummary[]>([]);
  const [selectedNodeIDs, setSelectedNodeIDs] = useState<string[]>(initial?.node_ids ?? []);
  const [specJson, setSpecJson] = useState(JSON.stringify(initial?.spec ?? defaultSpec(widgetType), null, 2));

  useEffect(() => {
    if (!initial) {
      setSpecJson(JSON.stringify(defaultSpec(widgetType), null, 2));
    }
  }, [widgetType, initial]);

  useEffect(() => {
    client.listNodes({ limit: 200 }).then((r) => setAllNodes(r.data)).catch(() => {});
  }, [client]);

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
    <aside
      style={{
        position: 'fixed', right: 0, top: 0, bottom: 0,
        width: 'min(560px, 90vw)', zIndex: 100,
        background: 'var(--bg-secondary)', borderLeft: '1px solid var(--border-color)',
        boxShadow: '0 0 24px var(--shadow)', padding: 24, overflow: 'auto',
      }}
    >
      <header style={{ display: 'flex', justifyContent: 'space-between' }}>
        <h3>{initial ? 'Edit widget' : 'New widget'}</h3>
        <button type="button" className="secondary-button" onClick={onCancel}>Close</button>
      </header>
      <form onSubmit={submit} style={{ display: 'grid', gap: 12, marginTop: 16 }}>
        <label>Title
          <input value={title} onChange={(e) => setTitle(e.target.value)} required />
        </label>
        <label>Widget type
          <select value={widgetType} onChange={(e) => setWidgetType(e.target.value as WidgetType)}>
            {WIDGET_TYPES.map((t) => <option key={t.value} value={t.value}>{t.label}</option>)}
          </select>
          <small style={{ display: 'block', color: 'var(--text-secondary)', marginTop: 4 }}>
            {WIDGET_TYPES.find((t) => t.value === widgetType)?.description}
          </small>
        </label>
        <label>Refresh interval (seconds)
          <input type="number" min={5} value={refresh} onChange={(e) => setRefresh(Number(e.target.value))} />
        </label>
        <label>Servers (empty = all nodes in tenant)
          <select multiple value={selectedNodeIDs} onChange={(e) =>
            setSelectedNodeIDs(Array.from(e.target.selectedOptions).map((o) => o.value))
          } style={{ minHeight: 120 }}>
            {allNodes.map((n) => (
              <option key={n.id} value={n.id}>{n.hostname || n.id}</option>
            ))}
          </select>
        </label>
        <label>Spec (JSON — see widget-type description)
          <textarea
            rows={8}
            value={specJson}
            onChange={(e) => setSpecJson(e.target.value)}
            style={{ fontFamily: 'ui-monospace, monospace', fontSize: 12 }}
          />
        </label>
        <div style={{ display: 'flex', gap: 8 }}>
          <button type="submit" className="primary-button">{initial ? 'Save' : 'Create'}</button>
          <button type="button" className="secondary-button" onClick={onCancel}>Cancel</button>
        </div>
      </form>
    </aside>
  );
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
