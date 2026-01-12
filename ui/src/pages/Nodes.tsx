import { FormEvent, useMemo, useState, useCallback } from 'react';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { NodeDiscovery } from '../components/NodeDiscovery';
import { DemandForm } from '../components/DemandForm';
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

  const {
    error: formError,
    success: formSuccess,
    showError,
    showSuccess,
    reset: resetFeedback,
  } = useFormFeedback();
  const { showToast } = useToast();
  const [registering, setRegistering] = useState(false);

  const [formTenantId, setFormTenantId] = useState('');
  const [formTenantName, setFormTenantName] = useState('');
  const [hostname, setHostname] = useState('');
  const [os, setOs] = useState('');
  const [arch, setArch] = useState('');
  const [publicIp, setPublicIp] = useState('');
  const [bootstrapToken, setBootstrapToken] = useState('');
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [editHostname, setEditHostname] = useState('');
  const [editOs, setEditOs] = useState('');
  const [editPublicIp, setEditPublicIp] = useState('');
  const [updating, setUpdating] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const handleDiscoveredNodes = useCallback((discoveredNodes: any[]) => {
    // Auto-fill form with first discovered node
    if (discoveredNodes.length > 0) {
      const node = discoveredNodes[0];
      setHostname(node.ip);
      setPublicIp(node.ip);
      setOs(node.os || 'Unknown');
      showToast(`Discovered ${discoveredNodes.length} node(s)`, 'success');
    }
  }, [showToast]);

  const tenantOptions = useMemo(() => tenants, [tenants]);
  const tenantNames = useMemo(() => new Map(tenants.map((t) => [t.id, t.name])), [tenants]);

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
    setEditHostname(node?.hostname ?? '');
    setEditOs(node?.os ?? '');
    setEditPublicIp(node?.public_ip ?? '');
  };

  const handleUpdateNode = async () => {
    if (!selectedNode) {
      return;
    }
    const payload: UpdateNodePayload = {};
    const trimmedHostname = editHostname.trim();
    const trimmedOs = editOs.trim();
    const trimmedPublicIp = editPublicIp.trim();

    if (trimmedHostname && trimmedHostname !== selectedNode.hostname) {
      payload.hostname = trimmedHostname;
    }
    if (trimmedOs !== (selectedNode.os ?? '')) {
      payload.os = trimmedOs;
    }
    if (trimmedPublicIp !== (selectedNode.public_ip ?? '')) {
      payload.public_ip = trimmedPublicIp;
    }

    if (!payload.hostname && payload.os === undefined && payload.public_ip === undefined) {
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
    <div className="focused-content">
      {/* Overview Section */}
      <div className="focused-section">
        <div className="focused-section-header">
          <h2 className="focused-section-title">🖥️ Node Management</h2>
          <p className="focused-section-subtitle">Connected agents reporting into the control plane.</p>
        </div>
        <div className="focused-section-content">
          <div className="stat-grid">
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
        </div>
      </div>

      {/* Filters Section */}
      <DemandForm 
        title="Filters" 
        icon="🔍"
        summary={`${summary.filtered} nodes shown`}
      >
        <div className="compact-form">
          <div className="form-field">
            <label>Filter by hostname</label>
            <input
              type="text"
              placeholder="e.g. node-01"
              value={hostnameFilter}
              onChange={(event) => setHostnameFilter(event.target.value)}
            />
          </div>
          <div className="form-field">
            <label>Tenant</label>
            <select
              value={selectedTenant || ''}
              onChange={(event) => setSelectedTenant(event.target.value || undefined)}
            >
              <option value="">All tenants</option>
              {tenantOptions.map((tenant) => (
                <option key={tenant.id} value={tenant.id}>
                  {tenant.name}
                </option>
              ))}
            </select>
          </div>
        </div>
      </DemandForm>

      {/* Node Discovery */}
      <DemandForm 
        title="Smart Discovery" 
        icon="🔍"
        summary="Find and auto-register nodes"
      >
        <NodeDiscovery onNodesDiscovered={handleDiscoveredNodes} />
      </DemandForm>

      {/* Quick Register */}
      <DemandForm 
        title="Quick Register" 
        icon="➕"
        summary="Register a new node manually"
      >
        <form className="compact-form" onSubmit={handleRegisterNode}>
          <div className="form-field">
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
          </div>
          <div className="form-field">
            <label htmlFor="node-os">OS</label>
            <input
              id="node-os"
              type="text"
              value={os}
              onChange={(event) => setOs(event.target.value)}
              placeholder="Ubuntu 24.04"
              disabled={registering}
            />
          </div>
          <div className="form-field">
            <label htmlFor="node-arch">Architecture</label>
            <input
              id="node-arch"
              type="text"
              value={arch}
              onChange={(event) => setArch(event.target.value)}
              placeholder="x86_64"
              disabled={registering}
            />
          </div>
          <div className="form-field">
            <label htmlFor="new-tenant-name">New tenant (optional)</label>
            <input
              id="new-tenant-name"
              type="text"
              placeholder="e.g. Edge Cluster"
              value={formTenantName}
              onChange={(event) => setFormTenantName(event.target.value)}
              disabled={registering}
            />
          </div>
          <div className="form-field">
            <label htmlFor="bootstrap-token">Bootstrap token</label>
            <input
              id="bootstrap-token"
              type="text"
              value={bootstrapToken}
              onChange={(event) => setBootstrapToken(event.target.value)}
              placeholder="Auto-generated if empty"
              disabled={registering}
            />
          </div>
          <button type="submit" className="primary-button" disabled={registering}>
            {registering ? 'Registering…' : 'Register Node'}
          </button>
        </form>
      </DemandForm>

      {/* Node List */}
      <div className="focused-section">
        <div className="focused-section-header">
          <h2 className="focused-section-title">📋 Node Registry</h2>
          <p className="focused-section-subtitle">Manage and monitor connected nodes</p>
        </div>
        <div className="focused-section-content">
          {nodes.length === 0 ? (
            <div className="empty-state">
              <p>No nodes match the current filters.</p>
            </div>
          ) : (
            <div className="nodes-list">
              {nodes.map((node) => (
                <div key={node.id} className="node-card">
                  <header>
                    <h3>{node.hostname}</h3>
                    <div className={`status-dot status-online`} />
                  </header>
                  <dl>
                    <dt>Tenant</dt>
                    <dd>{node.tenant_id}</dd>
                    <dt>OS</dt>
                    <dd>{node.os || '—'}</dd>
                    <dt>Architecture</dt>
                    <dd>{node.arch || '—'}</dd>
                    <dt>Public IP</dt>
                    <dd>{node.public_ip || '—'}</dd>
                    <dt>Last seen</dt>
                    <dd>{formatDate(node.updated_at)}</dd>
                  </dl>
                  <div className="node-card-actions">
                    <button
                      type="button"
                      className="ghost-button"
                      onClick={() => setSelectedNodeId(node.id)}
                      disabled={deleting}
                    >
                      {selectedNode?.id === node.id ? 'Close' : 'Manage'}
                    </button>
                    <button
                      type="button"
                      className="danger-button"
                      onClick={handleDeleteNode}
                      disabled={deleting || selectedNode?.id !== node.id}
                    >
                      Delete
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Node Management */}
      {selectedNode && (
        <DemandForm 
          title={`Manage: ${selectedNode.hostname}`} 
          icon="⚙️"
          summary="Update node configuration"
          defaultExpanded={true}
        >
          <div className="node-detail">
            <div className="node-detail-form">
              <div className="form-field">
                <label htmlFor="edit-hostname">Hostname</label>
                <input
                  id="edit-hostname"
                  type="text"
                  value={editHostname}
                  onChange={(event) => setEditHostname(event.target.value)}
                  disabled={updating}
                />
              </div>
              <div className="form-field">
                <label htmlFor="edit-os">Operating system</label>
                <input
                  id="edit-os"
                  type="text"
                  value={editOs}
                  onChange={(event) => setEditOs(event.target.value)}
                  disabled={updating}
                />
              </div>
              <div className="form-field">
                <label htmlFor="edit-public-ip">Public IP</label>
                <input
                  id="edit-public-ip"
                  type="text"
                  value={editPublicIp}
                  onChange={(event) => setEditPublicIp(event.target.value)}
                  disabled={updating}
                />
              </div>
              <div className="detail-actions">
                <button
                  type="button"
                  className="primary-button"
                  onClick={handleUpdateNode}
                  disabled={updating}
                >
                  {updating ? 'Updating…' : 'Update Node'}
                </button>
                <button
                  type="button"
                  className="danger-button"
                  onClick={handleDeleteNode}
                  disabled={deleting}
                >
                  {deleting ? 'Deleting…' : 'Delete Node'}
                </button>
              </div>
            </div>
          </div>
        </DemandForm>
      )}
    </div>
  );
}
