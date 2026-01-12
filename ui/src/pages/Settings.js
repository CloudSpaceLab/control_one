import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { useWebhooks } from '../hooks/useWebhooks';
import { useTenants } from '../hooks/useTenants';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import './Settings.css';
function formatDate(value) {
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
export function Settings() {
    const api = useApiClient();
    const [activeTab, setActiveTab] = useState('webhooks');
    const [selectedTenant, setSelectedTenant] = useState(undefined);
    const [isCreatingWebhook, setIsCreatingWebhook] = useState(false);
    const [editingWebhook, setEditingWebhook] = useState(null);
    const { data: tenants } = useTenants();
    const { data: webhooks, loading: webhooksLoading, error: webhooksError, reload: reloadWebhooks, } = useWebhooks({
        tenant_id: selectedTenant,
        limit: 100,
    });
    const { error: formError, success: formSuccess, showError, showSuccess, reset: resetFeedback, } = useFormFeedback();
    const { showToast } = useToast();
    const [saving, setSaving] = useState(false);
    const [webhookForm, setWebhookForm] = useState({
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
    const handleEditWebhook = (webhook) => {
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
    const handleEventToggle = (event) => {
        setWebhookForm((prev) => {
            const events = prev.events || [];
            if (events.includes(event)) {
                return { ...prev, events: events.filter((e) => e !== event) };
            }
            return { ...prev, events: [...events, event] };
        });
    };
    const handleSaveWebhook = async (event) => {
        event.preventDefault();
        if (!webhookForm.name || !webhookForm.url || !webhookForm.events || webhookForm.events.length === 0) {
            showError('Name, URL, and at least one event are required');
            return;
        }
        setSaving(true);
        resetFeedback();
        try {
            if (editingWebhook) {
                const payload = {
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
            }
            else {
                const payload = {
                    ...webhookForm,
                    tenant_id: selectedTenant,
                };
                await api.createWebhook(payload);
                showSuccess('Webhook created successfully');
            }
            setIsCreatingWebhook(false);
            setEditingWebhook(null);
            reloadWebhooks();
        }
        catch (error) {
            const message = error?.message || 'Failed to save webhook';
            showError(message);
            showToast(message, 'error');
        }
        finally {
            setSaving(false);
        }
    };
    const handleDeleteWebhook = async (webhookId) => {
        if (!confirm('Are you sure you want to delete this webhook?')) {
            return;
        }
        try {
            await api.deleteWebhook(webhookId);
            showToast('Webhook deleted successfully', 'success');
            reloadWebhooks();
        }
        catch (error) {
            const message = error?.message || 'Failed to delete webhook';
            showToast(message, 'error');
        }
    };
    const handleTestWebhook = async (webhookId) => {
        try {
            const result = await api.testWebhook(webhookId, {
                event_type: 'test',
                payload: { message: 'Test webhook delivery' },
            });
            if (result.success) {
                showToast('Webhook test successful', 'success');
            }
            else {
                showToast(`Webhook test failed: ${result.error || 'Unknown error'}`, 'error');
            }
        }
        catch (error) {
            showToast(error?.message || 'Failed to test webhook', 'error');
        }
    };
    return (_jsxs("div", { className: "settings-page", children: [_jsx("div", { className: "page-header", children: _jsxs("div", { children: [_jsx("h1", { children: "Settings" }), _jsx("p", { className: "subtitle", children: "Configure system settings, webhooks, and integrations" })] }) }), _jsxs("div", { className: "settings-tabs", children: [_jsx("button", { type: "button", className: activeTab === 'webhooks' ? 'tab-active' : 'tab-inactive', onClick: () => setActiveTab('webhooks'), children: "Webhooks" }), _jsx("button", { type: "button", className: activeTab === 'integrations' ? 'tab-active' : 'tab-inactive', onClick: () => setActiveTab('integrations'), children: "Integrations" })] }), activeTab === 'webhooks' && (_jsxs("div", { className: "settings-content", children: [_jsxs("div", { className: "section-header", children: [_jsx("h2", { children: "Webhooks" }), _jsx("button", { type: "button", onClick: handleCreateWebhook, className: "btn-primary", children: "Create Webhook" })] }), _jsxs("div", { className: "filter-section", children: [_jsx("label", { htmlFor: "tenant-filter", children: "Filter by Tenant" }), _jsxs("select", { id: "tenant-filter", value: selectedTenant || '', onChange: (e) => {
                                    setSelectedTenant(e.target.value || undefined);
                                }, children: [_jsx("option", { value: "", children: "All Tenants" }), tenants.map((t) => (_jsx("option", { value: t.id, children: t.name }, t.id)))] })] }), webhooksError && (_jsx("div", { className: "error-banner", children: _jsxs("p", { children: ["Error loading webhooks: ", webhooksError] }) })), webhooksLoading ? (_jsx("div", { className: "loading-placeholder", children: "Loading webhooks..." })) : webhooks.length === 0 ? (_jsx("div", { className: "empty-state", children: _jsx("p", { children: "No webhooks configured. Create one to get started." }) })) : (_jsx("div", { className: "webhooks-list", children: webhooks.map((webhook) => (_jsxs("div", { className: "webhook-card", children: [_jsxs("div", { className: "webhook-header", children: [_jsxs("div", { children: [_jsx("h3", { className: "webhook-name", children: webhook.name }), _jsx("div", { className: "webhook-url", children: webhook.url })] }), _jsx("div", { className: "webhook-status", children: _jsx("span", { className: `status-badge ${webhook.enabled ? 'enabled' : 'disabled'}`, children: webhook.enabled ? 'Enabled' : 'Disabled' }) })] }), _jsxs("div", { className: "webhook-details", children: [_jsxs("div", { className: "detail-item", children: [_jsx("span", { className: "detail-label", children: "Events:" }), _jsx("div", { className: "events-list", children: webhook.events.map((event) => (_jsx("span", { className: "event-badge", children: event }, event))) })] }), _jsxs("div", { className: "detail-item", children: [_jsx("span", { className: "detail-label", children: "Last Triggered:" }), _jsx("span", { children: formatDate(webhook.last_triggered_at) })] }), webhook.failure_count > 0 && (_jsxs("div", { className: "detail-item error", children: [_jsx("span", { className: "detail-label", children: "Failures:" }), _jsx("span", { children: webhook.failure_count })] }))] }), _jsxs("div", { className: "webhook-actions", children: [_jsx("button", { type: "button", onClick: () => handleTestWebhook(webhook.id), className: "btn-secondary", children: "Test" }), _jsx("button", { type: "button", onClick: () => handleEditWebhook(webhook), className: "btn-secondary", children: "Edit" }), _jsx("button", { type: "button", onClick: () => handleDeleteWebhook(webhook.id), className: "btn-danger", children: "Delete" })] })] }, webhook.id))) }))] })), activeTab === 'integrations' && (_jsxs("div", { className: "settings-content", children: [_jsx("h2", { children: "Integrations" }), _jsxs("div", { className: "integrations-placeholder", children: [_jsx("p", { children: "Integration settings coming soon." }), _jsx("p", { className: "hint", children: "This section will include configuration for:" }), _jsxs("ul", { children: [_jsx("li", { children: "Vault integration settings" }), _jsx("li", { children: "LDAP/AD configuration" }), _jsx("li", { children: "External directory services" }), _jsx("li", { children: "Notification channels" })] })] })] })), (isCreatingWebhook || editingWebhook) && (_jsx("div", { className: "modal-overlay", onClick: handleCancelEdit, children: _jsxs("div", { className: "modal-content", onClick: (e) => e.stopPropagation(), children: [_jsxs("div", { className: "modal-header", children: [_jsx("h2", { children: editingWebhook ? 'Edit Webhook' : 'Create Webhook' }), _jsx("button", { type: "button", onClick: handleCancelEdit, className: "modal-close", children: "\u00D7" })] }), _jsxs("form", { onSubmit: handleSaveWebhook, children: [_jsxs("div", { className: "modal-body", children: [formError && (_jsx("div", { className: "error-banner", children: _jsx("p", { children: formError }) })), formSuccess && (_jsx("div", { className: "success-banner", children: _jsx("p", { children: formSuccess }) })), _jsxs("div", { className: "form-group", children: [_jsx("label", { htmlFor: "webhook-name", children: "Name *" }), _jsx("input", { id: "webhook-name", type: "text", value: webhookForm.name, onChange: (e) => setWebhookForm({ ...webhookForm, name: e.target.value }), required: true, placeholder: "My Webhook" })] }), _jsxs("div", { className: "form-group", children: [_jsx("label", { htmlFor: "webhook-url", children: "URL *" }), _jsx("input", { id: "webhook-url", type: "url", value: webhookForm.url, onChange: (e) => setWebhookForm({ ...webhookForm, url: e.target.value }), required: true, placeholder: "https://example.com/webhook" })] }), _jsxs("div", { className: "form-group", children: [_jsx("label", { children: "Events *" }), _jsx("div", { className: "events-selection", children: AVAILABLE_EVENTS.map((event) => (_jsxs("label", { className: "event-checkbox", children: [_jsx("input", { type: "checkbox", checked: webhookForm.events?.includes(event) || false, onChange: () => handleEventToggle(event) }), _jsx("span", { children: event })] }, event))) })] }), _jsxs("div", { className: "form-row", children: [_jsxs("div", { className: "form-group", children: [_jsx("label", { htmlFor: "webhook-timeout", children: "Timeout (seconds)" }), _jsx("input", { id: "webhook-timeout", type: "number", min: "1", max: "300", value: webhookForm.timeout_seconds, onChange: (e) => setWebhookForm({ ...webhookForm, timeout_seconds: parseInt(e.target.value) || 30 }) })] }), _jsxs("div", { className: "form-group", children: [_jsx("label", { htmlFor: "webhook-retries", children: "Retry Count" }), _jsx("input", { id: "webhook-retries", type: "number", min: "0", max: "10", value: webhookForm.retry_count, onChange: (e) => setWebhookForm({ ...webhookForm, retry_count: parseInt(e.target.value) || 3 }) })] })] }), _jsx("div", { className: "form-group", children: _jsxs("label", { className: "checkbox-label", children: [_jsx("input", { type: "checkbox", checked: webhookForm.enabled, onChange: (e) => setWebhookForm({ ...webhookForm, enabled: e.target.checked }) }), _jsx("span", { children: "Enabled" })] }) }), _jsx("div", { className: "form-group", children: _jsxs("label", { className: "checkbox-label", children: [_jsx("input", { type: "checkbox", checked: webhookForm.verify_ssl, onChange: (e) => setWebhookForm({ ...webhookForm, verify_ssl: e.target.checked }) }), _jsx("span", { children: "Verify SSL Certificate" })] }) })] }), _jsxs("div", { className: "modal-footer", children: [_jsx("button", { type: "button", onClick: handleCancelEdit, className: "btn-secondary", disabled: saving, children: "Cancel" }), _jsx("button", { type: "submit", className: "btn-primary", disabled: saving, children: saving ? 'Saving...' : editingWebhook ? 'Update' : 'Create' })] })] })] }) }))] }));
}
