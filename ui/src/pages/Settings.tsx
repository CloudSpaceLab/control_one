import { FormEvent, useState } from 'react';
import { useWebhooks } from '../hooks/useWebhooks';
import { useTenants } from '../hooks/useTenants';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { Webhook, CreateWebhookPayload, UpdateWebhookPayload } from '../lib/api';
import { ConfirmModal } from '../components/ConfirmModal';
import { SectionHeader } from '../components/kit';
import './Settings.css';

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

const AVAILABLE_EVENTS = [
  'job.created',
  'job.succeeded',
  'job.failed',
  'compliance.scan.completed',
  'compliance.violation.detected',
  'node.registered',
  'node.updated',
  'policy.created',
  'policy.updated',
  'tenant.created',
  'tenant.updated',
];

export function Settings(): JSX.Element {
  const api = useApiClient();
  const [activeTab, setActiveTab] = useState<'webhooks' | 'integrations'>('webhooks');
  const [selectedTenant, setSelectedTenant] = useState<string | undefined>(undefined);
  const [isCreatingWebhook, setIsCreatingWebhook] = useState(false);
  const [editingWebhook, setEditingWebhook] = useState<Webhook | null>(null);

  const { data: tenants } = useTenants();
  const {
    data: webhooks,
    loading: webhooksLoading,
    error: webhooksError,
    reload: reloadWebhooks,
  } = useWebhooks({
    tenant_id: selectedTenant,
    limit: 100,
  });

  const {
    error: formError,
    success: formSuccess,
    showError,
    showSuccess,
    reset: resetFeedback,
  } = useFormFeedback();
  const { showToast } = useToast();
  const [saving, setSaving] = useState(false);
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);

  const [webhookForm, setWebhookForm] = useState<CreateWebhookPayload>({
    name: '',
    url: '',
    events: [],
    enabled: true,
    verify_ssl: true,
    timeout_seconds: 30,
    retry_count: 3,
  });

  const handleCreateWebhook = () => {
    setIsCreatingWebhook(true);
    setEditingWebhook(null);
    setWebhookForm({
      name: '',
      url: '',
      events: [],
      enabled: true,
      verify_ssl: true,
      timeout_seconds: 30,
      retry_count: 3,
    });
    resetFeedback();
  };

  const handleEditWebhook = (webhook: Webhook) => {
    setEditingWebhook(webhook);
    setIsCreatingWebhook(false);
    setWebhookForm({
      name: webhook.name,
      url: webhook.url,
      events: [...webhook.events],
      enabled: webhook.enabled,
      verify_ssl: webhook.verify_ssl,
      timeout_seconds: webhook.timeout_seconds,
      retry_count: webhook.retry_count,
    });
    resetFeedback();
  };

  const handleCancelEdit = () => {
    setIsCreatingWebhook(false);
    setEditingWebhook(null);
    resetFeedback();
  };

  const handleEventToggle = (event: string) => {
    setWebhookForm((prev) => {
      const events = prev.events || [];
      if (events.includes(event)) {
        return { ...prev, events: events.filter((e) => e !== event) };
      }
      return { ...prev, events: [...events, event] };
    });
  };

  const handleSaveWebhook = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!webhookForm.name || !webhookForm.url || !webhookForm.events || webhookForm.events.length === 0) {
      showError('Name, URL, and at least one event are required');
      return;
    }

    setSaving(true);
    resetFeedback();

    try {
      if (editingWebhook) {
        const payload: UpdateWebhookPayload = {
          name: webhookForm.name,
          url: webhookForm.url,
          events: webhookForm.events,
          enabled: webhookForm.enabled,
          verify_ssl: webhookForm.verify_ssl,
          timeout_seconds: webhookForm.timeout_seconds,
          retry_count: webhookForm.retry_count,
        };
        await api.updateWebhook(editingWebhook.id, payload);
        showSuccess('Webhook updated successfully');
      } else {
        const payload: CreateWebhookPayload = {
          ...webhookForm,
          tenant_id: selectedTenant,
        };
        await api.createWebhook(payload);
        showSuccess('Webhook created successfully');
      }
      setIsCreatingWebhook(false);
      setEditingWebhook(null);
      reloadWebhooks();
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : 'Failed to save webhook';
      showError(message);
      showToast(message, 'error');
    } finally {
      setSaving(false);
    }
  };

  const handleDeleteWebhook = async (webhookId: string) => {
    try {
      await api.deleteWebhook(webhookId);
      showToast('Webhook deleted successfully', 'success');
      reloadWebhooks();
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : 'Failed to delete webhook';
      showToast(message, 'error');
    }
  };

  const handleTestWebhook = async (webhookId: string) => {
    try {
      const result = await api.testWebhook(webhookId, {
        event_type: 'test',
        payload: { message: 'Test webhook delivery' },
      });
      if (result.success) {
        showToast('Webhook test successful', 'success');
      } else {
        showToast(`Webhook test failed: ${result.error || 'Unknown error'}`, 'error');
      }
    } catch (error: unknown) {
      showToast(error instanceof Error ? error.message : 'Failed to test webhook', 'error');
    }
  };

  return (
    <div className="flex flex-col gap-5 settings-page">
      <SectionHeader
        eyebrow="CONFIGURATION · SETTINGS"
        title="Settings"
        description="Configure system settings, webhooks, and integrations."
      />

      <div className="settings-tabs">
        <button
          type="button"
          className={activeTab === 'webhooks' ? 'tab-active' : 'tab-inactive'}
          onClick={() => setActiveTab('webhooks')}
        >
          Webhooks
        </button>
        <button
          type="button"
          className={activeTab === 'integrations' ? 'tab-active' : 'tab-inactive'}
          onClick={() => setActiveTab('integrations')}
        >
          Integrations
        </button>
      </div>

      {activeTab === 'webhooks' && (
        <div className="settings-content">
          <div className="section-header">
            <h2>Webhooks</h2>
            <button type="button" onClick={handleCreateWebhook} className="primary-button">
              Create Webhook
            </button>
          </div>

          <div className="filter-section">
            <label htmlFor="tenant-filter">Filter by Tenant</label>
            <select
              id="tenant-filter"
              value={selectedTenant || ''}
              onChange={(e) => {
                setSelectedTenant(e.target.value || undefined);
              }}
            >
              <option value="">All Tenants</option>
              {tenants.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          </div>

          {webhooksError && (
            <div className="error-banner">
              <p>Error loading webhooks: {webhooksError}</p>
            </div>
          )}

          {webhooksLoading ? (
            <div className="loading-placeholder">Loading webhooks...</div>
          ) : webhooks.length === 0 ? (
            <div className="empty-state">
              <p>No webhooks configured. Create one to get started.</p>
            </div>
          ) : (
            <div className="webhooks-list">
              {webhooks.map((webhook) => (
                <div key={webhook.id} className="webhook-card">
                  <div className="webhook-header">
                    <div>
                      <h3 className="webhook-name">{webhook.name}</h3>
                      <div className="webhook-url">{webhook.url}</div>
                    </div>
                    <div className="webhook-status">
                      <span className={`status-badge ${webhook.enabled ? 'enabled' : 'disabled'}`}>
                        {webhook.enabled ? 'Enabled' : 'Disabled'}
                      </span>
                    </div>
                  </div>
                  <div className="webhook-details">
                    <div className="detail-item">
                      <span className="detail-label">Events:</span>
                      <div className="events-list">
                        {webhook.events.map((event) => (
                          <span key={event} className="event-badge">
                            {event}
                          </span>
                        ))}
                      </div>
                    </div>
                    <div className="detail-item">
                      <span className="detail-label">Last Triggered:</span>
                      <span>{formatDate(webhook.last_triggered_at)}</span>
                    </div>
                    {webhook.failure_count > 0 && (
                      <div className="detail-item error">
                        <span className="detail-label">Failures:</span>
                        <span>{webhook.failure_count}</span>
                      </div>
                    )}
                  </div>
                  <div className="webhook-actions">
                    <button
                      type="button"
                      onClick={() => handleTestWebhook(webhook.id)}
                      className="ghost-button"
                    >
                      Test
                    </button>
                    <button
                      type="button"
                      onClick={() => handleEditWebhook(webhook)}
                      className="ghost-button"
                    >
                      Edit
                    </button>
                    <button
                      type="button"
                      onClick={() => setConfirmDeleteId(webhook.id)}
                      className="danger-button"
                    >
                      Delete
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {activeTab === 'integrations' && (
        <div className="settings-content">
          <h2>Integrations</h2>
          <div className="integrations-placeholder">
            <p>Integration settings coming soon.</p>
            <p className="hint">This section will include configuration for:</p>
            <ul>
              <li>Vault integration settings</li>
              <li>LDAP/AD configuration</li>
              <li>External directory services</li>
              <li>Notification channels</li>
            </ul>
          </div>
        </div>
      )}

      <ConfirmModal
        open={confirmDeleteId !== null}
        title="Delete webhook?"
        body="This cannot be undone."
        variant="danger"
        confirmLabel="Delete"
        onConfirm={() => {
          if (confirmDeleteId) handleDeleteWebhook(confirmDeleteId);
          setConfirmDeleteId(null);
        }}
        onCancel={() => setConfirmDeleteId(null)}
      />

      {(isCreatingWebhook || editingWebhook) && (
        <div className="modal-overlay" onClick={handleCancelEdit}>
          <div className="modal-content" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h2>{editingWebhook ? 'Edit Webhook' : 'Create Webhook'}</h2>
              <button type="button" onClick={handleCancelEdit} className="modal-close">
                ×
              </button>
            </div>

            <form onSubmit={handleSaveWebhook}>
              <div className="modal-body">
                {formError && (
                  <div className="error-banner">
                    <p>{formError}</p>
                  </div>
                )}

                {formSuccess && (
                  <div className="success-banner">
                    <p>{formSuccess}</p>
                  </div>
                )}

                <div className="form-group">
                  <label htmlFor="webhook-name">Name *</label>
                  <input
                    id="webhook-name"
                    type="text"
                    value={webhookForm.name}
                    onChange={(e) => setWebhookForm({ ...webhookForm, name: e.target.value })}
                    required
                    placeholder="My Webhook"
                  />
                </div>

                <div className="form-group">
                  <label htmlFor="webhook-url">URL *</label>
                  <input
                    id="webhook-url"
                    type="url"
                    value={webhookForm.url}
                    onChange={(e) => setWebhookForm({ ...webhookForm, url: e.target.value })}
                    required
                    placeholder="https://example.com/webhook"
                  />
                </div>

                <div className="form-group">
                  <label>Events *</label>
                  <div className="events-selection">
                    {AVAILABLE_EVENTS.map((event) => (
                      <label key={event} className="event-checkbox">
                        <input
                          type="checkbox"
                          checked={webhookForm.events?.includes(event) || false}
                          onChange={() => handleEventToggle(event)}
                        />
                        <span>{event}</span>
                      </label>
                    ))}
                  </div>
                </div>

                <div className="form-row">
                  <div className="form-group">
                    <label htmlFor="webhook-timeout">Timeout (seconds)</label>
                    <input
                      id="webhook-timeout"
                      type="number"
                      min="1"
                      max="300"
                      value={webhookForm.timeout_seconds}
                      onChange={(e) =>
                        setWebhookForm({ ...webhookForm, timeout_seconds: parseInt(e.target.value) || 30 })
                      }
                    />
                  </div>

                  <div className="form-group">
                    <label htmlFor="webhook-retries">Retry Count</label>
                    <input
                      id="webhook-retries"
                      type="number"
                      min="0"
                      max="10"
                      value={webhookForm.retry_count}
                      onChange={(e) =>
                        setWebhookForm({ ...webhookForm, retry_count: parseInt(e.target.value) || 3 })
                      }
                    />
                  </div>
                </div>

                <div className="form-group">
                  <label className="checkbox-label">
                    <input
                      type="checkbox"
                      checked={webhookForm.enabled}
                      onChange={(e) => setWebhookForm({ ...webhookForm, enabled: e.target.checked })}
                    />
                    <span>Enabled</span>
                  </label>
                </div>

                <div className="form-group">
                  <label className="checkbox-label">
                    <input
                      type="checkbox"
                      checked={webhookForm.verify_ssl}
                      onChange={(e) => setWebhookForm({ ...webhookForm, verify_ssl: e.target.checked })}
                    />
                    <span>Verify SSL Certificate</span>
                  </label>
                </div>
              </div>

              <div className="modal-footer">
                <button type="button" onClick={handleCancelEdit} className="ghost-button" disabled={saving}>
                  Cancel
                </button>
                <button type="submit" className="primary-button" disabled={saving}>
                  {saving ? 'Saving...' : editingWebhook ? 'Update' : 'Create'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}

