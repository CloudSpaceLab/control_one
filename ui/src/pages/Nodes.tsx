import { FormEvent, useMemo, useState } from 'react';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import type { RegisterNodePayload, UpdateNodePayload } from '../lib/api';

function formatDate(value?: string): string {
  if (!value) {
    return '—';
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toLocaleString();
}

export function Nodes(): JSX.Element {
  const api = useApiClient();
  const { data: tenants, reload: reloadTenants } = useTenants();
  const [selectedTenant, setSelectedTenant] = useState<string | undefined>(undefined);
  const [hostnameFilter, setHostnameFilter] = useState('');
  const [limit] = useState(12);
  const [offset, setOffset] = useState(0);

  const { data: nodes, loading, error, pagination, reload: reloadNodes } = useNodes({
    tenantId: selectedTenant,
    hostnamePrefix: hostnameFilter.trim() || undefined,
    limit,
    offset,
  });

  const [formTenantId, setFormTenantId] = useState('');
  const [formTenantName, setFormTenantName] = useState('');
  const [hostname, setHostname] = useState('');
  const [os, setOs] = useState('');
  const [arch, setArch] = useState('');
  const [publicIp, setPublicIp] = useState('');
  const [bootstrapToken, setBootstrapToken] = useState('');
  const {
    error: formError,
    success: formSuccess,
    showError,
    showSuccess,
    reset: resetFeedback,
  } = useFormFeedback();
  const { showToast } = useToast();
  const [registering, setRegistering] = useState(false);

  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [detailHostname, setDetailHostname] = useState('');
  const [detailOs, setDetailOs] = useState('');
  const [detailArch, setDetailArch] = useState('');
  const [detailPublicIp, setDetailPublicIp] = useState('');
  const [updating, setUpdating] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const tenantOptions = useMemo(() => tenants, [tenants]);
  const tenantNames = useMemo(() => {
    const entries = new Map<string, string>();
    for (const tenant of tenants) {
      entries.set(tenant.id, tenant.name);
    }
    return entries;
  }, [tenants]);

  const selectedNode = useMemo(
    () => nodes.find((node) => node.id === selectedNodeId) ?? null,
    [nodes, selectedNodeId],
  );

  const summary = useMemo(() => {
    return {
      total: pagination.total,
      filtered: nodes.length,
    };
  }, [pagination.total, nodes.length]);

  const handleRegisterNode = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmedHostname = hostname.trim();
    const trimmedToken = bootstrapToken.trim();
    const trimmedTenantName = formTenantName.trim();

    if (!trimmedHostname) {
      showError('Hostname is required');
      return;
    }
    if (!trimmedToken) {
      showError('Bootstrap token is required');
      return;
    }
    if (!formTenantId && !trimmedTenantName) {
      showError('Select an existing tenant or provide a new tenant name');
      return;
    }

    setRegistering(true);
    resetFeedback();

    try {
      const payload: RegisterNodePayload = {
        hostname: trimmedHostname,
        bootstrap_token: trimmedToken,
      };
      if (formTenantId) {
        payload.tenant_id = formTenantId;
      } else if (trimmedTenantName) {
        payload.tenant_name = trimmedTenantName;
      }
      if (os.trim()) {
        payload.os = os.trim();
      }
      if (arch.trim()) {
        payload.arch = arch.trim();
      }
      if (publicIp.trim()) {
        payload.public_ip = publicIp.trim();
      }

      const response = await api.registerNode(payload);
      const successMessage = `Node ${response.node_id} registered for tenant ${response.tenant_id}.`;
      showSuccess(successMessage);
      showToast(successMessage, 'success');
      setHostname('');
      setOs('');
      setArch('');
      setPublicIp('');
      setBootstrapToken('');
      setFormTenantName('');
      setSelectedTenant(response.tenant_id);
      setFormTenantId(response.tenant_id);
      reloadNodes();
      reloadTenants();
    } catch (err) {
      if (err instanceof Error) {
        showError(err.message);
        showToast(err.message, 'error');
      } else {
        const fallback = 'Failed to register node';
        showError(fallback);
        showToast(fallback, 'error');
      }
    } finally {
      setRegistering(false);
    }
  };

  const openNodeDetails = (nodeId: string) => {
    setSelectedNodeId((current) => (current === nodeId ? null : nodeId));
    const node = nodes.find((n) => n.id === nodeId);
    setDetailHostname(node?.hostname ?? '');
    setDetailOs(node?.os ?? '');
    setDetailArch(node?.arch ?? '');
    setDetailPublicIp(node?.public_ip ?? '');
  };

  const handleUpdateNode = async () => {
    if (!selectedNode) {
      return;
    }
    const payload: UpdateNodePayload = {};
    const trimmedHostname = detailHostname.trim();
    const trimmedOs = detailOs.trim();
    const trimmedArch = detailArch.trim();
    const trimmedPublicIp = detailPublicIp.trim();

    if (trimmedHostname && trimmedHostname !== selectedNode.hostname) {
      payload.hostname = trimmedHostname;
    }
    if (trimmedOs !== (selectedNode.os ?? '')) {
      payload.os = trimmedOs;
    }
    if (trimmedArch !== (selectedNode.arch ?? '')) {
      payload.arch = trimmedArch;
    }
    if (trimmedPublicIp !== (selectedNode.public_ip ?? '')) {
      payload.public_ip = trimmedPublicIp;
    }

    if (!payload.hostname && payload.os === undefined && payload.arch === undefined && payload.public_ip === undefined) {
      showToast('No changes to save.', 'info');
      return;
    }

    setUpdating(true);
    try {
      await api.updateNode(selectedNode.id, payload);
      showToast('Node updated.', 'success');
      await reloadNodes();
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to update node.';
      showToast(message, 'error');
    } finally {
      setUpdating(false);
    }
  };

  const handleDeleteNode = async () => {
    if (!selectedNode) {
      return;
    }
    const confirmed = window.confirm(`Delete node “${selectedNode.hostname}”?`);
    if (!confirmed) {
      return;
    }
    setDeleting(true);
    try {
      await api.deleteNode(selectedNode.id);
      showToast('Node deleted.', 'success');
      setSelectedNodeId(null);
      await reloadNodes();
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to delete node.';
      showToast(message, 'error');
    } finally {
      setDeleting(false);
    }
  };

  return (
    <section className="nodes-page">
      <div className="page-header">
        <div>
          <h2>Nodes</h2>
          <p>Connected agents reporting into the control plane.</p>
        </div>
      </div>

      <div className="stat-card-grid">
        <article className="stat-card">
          <span className="muted">Total nodes</span>
          <strong>{summary.total}</strong>
          <small className="muted">{selectedTenant ? `Filtered by tenant` : 'All tenants'}</small>
        </article>
        <article className="stat-card">
          <span className="muted">Visible</span>
          <strong>{summary.filtered}</strong>
          <small className="muted">matching current filters</small>
        </article>
      </div>

      <div className="nodes-layout">
        <form className="panel nodes-form" onSubmit={handleRegisterNode}>
          <h3>Register node</h3>
          <label htmlFor="register-tenant">Existing tenant</label>
          <select
            id="register-tenant"
            value={formTenantId}
            onChange={(event) => {
              setFormTenantId(event.target.value);
            }}
            disabled={registering}
          >
            <option value="">— Select tenant —</option>
            {tenantOptions.map((tenant) => (
              <option key={tenant.id} value={tenant.id}>
                {tenant.name}
              </option>
            ))}
          </select>
          <small className="muted">
            Optionally provide a new tenant name below to auto-create a tenant during registration.
          </small>
          <label htmlFor="new-tenant-name">New tenant name</label>
          <input
            id="new-tenant-name"
            type="text"
            placeholder="e.g. Edge Cluster"
            value={formTenantName}
            onChange={(event) => setFormTenantName(event.target.value)}
            disabled={registering}
          />
          <label htmlFor="hostname">Hostname</label>
          <input
            id="hostname"
            type="text"
            value={hostname}
            onChange={(event) => setHostname(event.target.value)}
            placeholder="node-01.example.com"
            disabled={registering}
            required
          />
          <label htmlFor="node-os">Operating system</label>
          <input
            id="node-os"
            type="text"
            value={os}
            onChange={(event) => setOs(event.target.value)}
            placeholder="Ubuntu 24.04"
            disabled={registering}
          />
          <label htmlFor="node-arch">Architecture</label>
          <input
            id="node-arch"
            type="text"
            value={arch}
            onChange={(event) => setArch(event.target.value)}
            placeholder="x86_64"
            disabled={registering}
          />
          <label htmlFor="node-ip">Public IP</label>
          <input
            id="node-ip"
            type="text"
            value={publicIp}
            onChange={(event) => setPublicIp(event.target.value)}
            placeholder="203.0.113.10"
            disabled={registering}
          />
          <label htmlFor="bootstrap-token">Bootstrap token</label>
          <input
            id="bootstrap-token"
            type="text"
            value={bootstrapToken}
            onChange={(event) => setBootstrapToken(event.target.value)}
            placeholder="control-one-bootstrap-token"
            disabled={registering}
            required
          />
          {formError ? <p className="form-error">{formError}</p> : null}
          {formSuccess ? <p className="form-success">{formSuccess}</p> : null}
          <button type="submit" disabled={registering}>
            {registering ? 'Registering…' : 'Register node'}
          </button>
        </form>

        <div className="panel nodes-list">
          <div className="toolbar nodes-toolbar">
            <label htmlFor="tenant-filter">
              Tenant
              <select
                id="tenant-filter"
                value={selectedTenant ?? ''}
                onChange={(event) => {
                  const value = event.target.value;
                  setSelectedTenant(value === '' ? undefined : value);
                  setOffset(0);
                }}
              >
                <option value="">All tenants</option>
                {tenantOptions.map((tenant) => (
                  <option key={tenant.id} value={tenant.id}>
                    {tenant.name}
                  </option>
                ))}
              </select>
            </label>
            <label htmlFor="hostname-filter">
              Hostname
              <input
                id="hostname-filter"
                type="search"
                placeholder="Search hostname"
                value={hostnameFilter}
                onChange={(event) => {
                  setHostnameFilter(event.target.value);
                  setOffset(0);
                }}
              />
            </label>
            <button type="button" className="ghost-button" onClick={reloadNodes} disabled={loading}>
              {loading ? 'Refreshing…' : 'Refresh'}
            </button>
          </div>

          {loading ? <p className="muted">Loading nodes&hellip;</p> : null}
          {error ? <p className="form-error">Failed to load nodes: {error}</p> : null}
          {!loading && !error && nodes.length === 0 ? <p className="muted">No nodes match the current filters.</p> : null}

          {!loading && !error && nodes.length > 0 ? (
            <>
              <table className="nodes-table">
                <thead>
                  <tr>
                    <th>Hostname</th>
                    <th>Tenant</th>
                    <th>OS</th>
                    <th>Public IP</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {nodes.map((node) => (
                    <tr key={node.id} className={selectedNodeId === node.id ? 'active-row' : undefined}>
                      <td>{node.hostname}</td>
                      <td>{tenantNames.get(node.tenant_id) ?? node.tenant_id}</td>
                      <td>{node.os ?? '—'}</td>
                      <td>{node.public_ip ?? '—'}</td>
                      <td>
                        <button type="button" className="ghost-button" onClick={() => openNodeDetails(node.id)}>
                          {selectedNodeId === node.id ? 'Hide' : 'View'}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <div className="pagination">
                <button
                  type="button"
                  disabled={pagination.prevOffset === null || pagination.prevOffset === undefined}
                  onClick={() => setOffset(pagination.prevOffset ?? 0)}
                >
                  Previous
                </button>
                <span>
                  Showing {nodes.length} of {pagination.total} nodes
                </span>
                <button
                  type="button"
                  disabled={pagination.nextOffset === null || pagination.nextOffset === undefined}
                  onClick={() => setOffset(pagination.nextOffset ?? offset + limit)}
                >
                  Next
                </button>
              </div>
            </>
          ) : null}
        </div>

        {selectedNode ? (
          <aside className="panel node-detail">
            <h3>Node details</h3>
            <dl className="meta-grid">
              <div>
                <dt>Hostname</dt>
                <dd>{selectedNode.hostname}</dd>
              </div>
              <div>
                <dt>Node ID</dt>
                <dd className="mono">{selectedNode.id}</dd>
              </div>
              <div>
                <dt>Tenant</dt>
                <dd>{tenantNames.get(selectedNode.tenant_id) ?? selectedNode.tenant_id}</dd>
              </div>
              <div>
                <dt>Created</dt>
                <dd>{formatDate(selectedNode.created_at)}</dd>
              </div>
              <div>
                <dt>Updated</dt>
                <dd>{formatDate(selectedNode.updated_at)}</dd>
              </div>
            </dl>
            <div className="node-detail-form">
              <label htmlFor="detail-hostname">Hostname</label>
              <input
                id="detail-hostname"
                type="text"
                value={detailHostname}
                onChange={(event) => setDetailHostname(event.target.value)}
              />
              <label htmlFor="detail-os">Operating system</label>
              <input
                id="detail-os"
                type="text"
                value={detailOs}
                onChange={(event) => setDetailOs(event.target.value)}
                placeholder="Ubuntu 24.04"
              />
              <label htmlFor="detail-arch">Architecture</label>
              <input
                id="detail-arch"
                type="text"
                value={detailArch}
                onChange={(event) => setDetailArch(event.target.value)}
                placeholder="x86_64"
              />
              <label htmlFor="detail-ip">Public IP</label>
              <input
                id="detail-ip"
                type="text"
                value={detailPublicIp}
                onChange={(event) => setDetailPublicIp(event.target.value)}
                placeholder="203.0.113.10"
              />
              <div className="detail-actions">
                <button type="button" className="ghost-button" onClick={() => setSelectedNodeId(null)}>
                  Close
                </button>
                <button type="button" className="primary-button" onClick={handleUpdateNode} disabled={updating}>
                  {updating ? 'Saving…' : 'Save changes'}
                </button>
                <button type="button" className="danger-button" onClick={handleDeleteNode} disabled={deleting}>
                  {deleting ? 'Deleting…' : 'Delete node'}
                </button>
              </div>
            </div>
          </aside>
        ) : null}
      </div>
    </section>
  );
}
