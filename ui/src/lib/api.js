const DEFAULT_API_BASE_URL = 'http://localhost:8080';
const HTTP_STATUS_UNAUTHORIZED = 401;
async function safeErrorMessage(response) {
    try {
        const data = await response.json();
        if (data && typeof data.message === 'string') {
            return data.message;
        }
    }
    catch (error) {
        // ignore json parse errors
    }
    return response.statusText;
}
export class APIError extends Error {
    status;
    constructor(message, status) {
        super(message);
        this.name = 'APIError';
        this.status = status;
    }
}
export class APIClient {
    baseUrl;
    token;
    unauthorizedHandler;
    constructor({ baseUrl, token } = {}) {
        const configured = baseUrl ?? import.meta.env.VITE_API_URL;
        const resolved = configured ? configured.replace(/\/$/, '') : DEFAULT_API_BASE_URL;
        this.baseUrl = resolved;
        this.token = token ?? null;
    }
    setToken(token) {
        this.token = token;
    }
    onUnauthorized(handler) {
        this.unauthorizedHandler = handler;
    }
    async getProfile() {
        return this.request('/api/v1/me');
    }
    async listTenants(params = {}) {
        const search = new URLSearchParams();
        if (params.namePrefix) {
            search.set('name_prefix', params.namePrefix);
        }
        if (typeof params.limit === 'number') {
            search.set('limit', params.limit.toString());
        }
        if (typeof params.offset === 'number') {
            search.set('offset', params.offset.toString());
        }
        const query = search.toString();
        const suffix = query ? `?${query}` : '';
        const response = await this.request(`/api/v1/tenants${suffix}`);
        return {
            data: response.data,
            pagination: normalizePagination(response.pagination),
        };
    }
    async listNodes(options = {}) {
        const search = new URLSearchParams();
        if (options.tenantId) {
            search.set('tenant_id', options.tenantId);
        }
        if (options.hostnamePrefix) {
            search.set('hostname_prefix', options.hostnamePrefix);
        }
        if (typeof options.limit === 'number') {
            search.set('limit', options.limit.toString());
        }
        if (typeof options.offset === 'number') {
            search.set('offset', options.offset.toString());
        }
        const query = search.toString();
        const suffix = query ? `?${query}` : '';
        const response = await this.request(`/api/v1/nodes${suffix}`);
        return {
            data: response.data,
            pagination: normalizePagination(response.pagination),
        };
    }
    async listJobs(params = {}) {
        const search = new URLSearchParams();
        if (params.tenantId) {
            search.set('tenant_id', params.tenantId);
        }
        if (params.status) {
            search.set('status', params.status);
        }
        if (params.type) {
            search.set('type', params.type);
        }
        if (typeof params.limit === 'number') {
            search.set('limit', params.limit.toString());
        }
        if (typeof params.offset === 'number') {
            search.set('offset', params.offset.toString());
        }
        const query = search.toString();
        const suffix = query ? `?${query}` : '';
        const response = await this.request(`/api/v1/jobs${suffix}`);
        return {
            data: response.data,
            pagination: normalizePagination(response.pagination),
        };
    }
    async getJob(jobId) {
        const encoded = encodeURIComponent(jobId);
        return this.request(`/api/v1/jobs/${encoded}`);
    }
    async cancelJob(jobId) {
        const encoded = encodeURIComponent(jobId);
        return this.request(`/api/v1/jobs/${encoded}/cancel`, {
            method: 'POST',
        });
    }
    async getWorkerStatus() {
        return this.request('/api/v1/worker/status');
    }
    async createJob(payload) {
        return this.request('/api/v1/jobs', {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    async createTenant(payload) {
        return this.request('/api/v1/tenants', {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    async getTenant(tenantId) {
        const encoded = encodeURIComponent(tenantId);
        return this.request(`/api/v1/tenants/${encoded}`);
    }
    async updateTenant(tenantId, payload) {
        const encoded = encodeURIComponent(tenantId);
        return this.request(`/api/v1/tenants/${encoded}`, {
            method: 'PATCH',
            body: JSON.stringify(payload),
        });
    }
    async deleteTenant(tenantId) {
        const encoded = encodeURIComponent(tenantId);
        await this.request(`/api/v1/tenants/${encoded}`, { method: 'DELETE' });
    }
    async registerNode(payload) {
        return this.request('/api/v1/register', {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    async getNode(nodeId) {
        const encoded = encodeURIComponent(nodeId);
        return this.request(`/api/v1/nodes/${encoded}`);
    }
    async updateNode(nodeId, payload) {
        const encoded = encodeURIComponent(nodeId);
        return this.request(`/api/v1/nodes/${encoded}`, {
            method: 'PATCH',
            body: JSON.stringify(payload),
        });
    }
    async deleteNode(nodeId) {
        const encoded = encodeURIComponent(nodeId);
        await this.request(`/api/v1/nodes/${encoded}`, { method: 'DELETE' });
    }
    async listUsers(params = {}) {
        const search = new URLSearchParams();
        if (typeof params.limit === 'number') {
            search.set('limit', params.limit.toString());
        }
        if (typeof params.offset === 'number') {
            search.set('offset', params.offset.toString());
        }
        const suffix = search.toString() ? `?${search.toString()}` : '';
        const response = await this.request(`/api/v1/users${suffix}`);
        return {
            data: response.data,
            pagination: normalizePagination(response.pagination),
        };
    }
    async getUser(userID) {
        const encoded = encodeURIComponent(userID);
        return this.request(`/api/v1/users/${encoded}`);
    }
    async updateUserRoles(userID, payload) {
        const encoded = encodeURIComponent(userID);
        return this.request(`/api/v1/users/${encoded}`, {
            method: 'PATCH',
            body: JSON.stringify(payload),
        });
    }
    async listRoles() {
        return this.request('/api/v1/roles');
    }
    async listTemplates(params = {}) {
        const search = new URLSearchParams();
        if (params.provider) {
            search.set('provider', params.provider);
        }
        if (params.namePrefix) {
            search.set('name_prefix', params.namePrefix);
        }
        if (typeof params.includeArchived === 'boolean') {
            search.set('include_archived', params.includeArchived ? 'true' : 'false');
        }
        if (typeof params.limit === 'number') {
            search.set('limit', params.limit.toString());
        }
        if (typeof params.offset === 'number') {
            search.set('offset', params.offset.toString());
        }
        const suffix = search.toString() ? `?${search.toString()}` : '';
        const response = await this.request(`/api/v1/templates${suffix}`);
        return {
            data: response.data,
            pagination: normalizePagination(response.pagination),
        };
    }
    async createTemplate(payload) {
        return this.request('/api/v1/templates', {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    async getTemplate(templateId) {
        const encoded = encodeURIComponent(templateId);
        return this.request(`/api/v1/templates/${encoded}`);
    }
    async updateTemplate(templateId, payload) {
        const encoded = encodeURIComponent(templateId);
        return this.request(`/api/v1/templates/${encoded}`, {
            method: 'PATCH',
            body: JSON.stringify(payload),
        });
    }
    async listTemplateVersions(templateId, params = {}) {
        const search = new URLSearchParams();
        if (typeof params.limit === 'number') {
            search.set('limit', params.limit.toString());
        }
        if (typeof params.offset === 'number') {
            search.set('offset', params.offset.toString());
        }
        const suffix = search.toString() ? `?${search.toString()}` : '';
        const encoded = encodeURIComponent(templateId);
        const response = await this.request(`/api/v1/templates/${encoded}/versions${suffix}`);
        return {
            data: response.data,
            pagination: normalizePagination(response.pagination),
        };
    }
    async createTemplateVersion(templateId, payload) {
        const encoded = encodeURIComponent(templateId);
        return this.request(`/api/v1/templates/${encoded}/versions`, {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    async promoteTemplateVersion(templateId, versionNumber) {
        const encoded = encodeURIComponent(templateId);
        return this.request(`/api/v1/templates/${encoded}/versions/${versionNumber}/promote`, { method: 'POST' });
    }
    async archiveTemplate(templateId) {
        const encoded = encodeURIComponent(templateId);
        return this.request(`/api/v1/templates/${encoded}/archive`, {
            method: 'POST',
        });
    }
    async unarchiveTemplate(templateId) {
        const encoded = encodeURIComponent(templateId);
        return this.request(`/api/v1/templates/${encoded}/unarchive`, {
            method: 'POST',
        });
    }
    async executeTemplate(templateId, execution) {
        const encoded = encodeURIComponent(templateId);
        return this.request(`/api/v1/templates/${encoded}/execute`, {
            method: 'POST',
            body: JSON.stringify(execution),
        });
    }
    async getTemplateExecution(executionId) {
        const encoded = encodeURIComponent(executionId);
        return this.request(`/api/v1/template-executions/${encoded}`);
    }
    async listTemplateExecutions(templateId) {
        const search = templateId ? `?template_id=${encodeURIComponent(templateId)}` : '';
        return this.request(`/api/v1/template-executions${search}`);
    }
    async listComplianceResults(params = {}) {
        const search = new URLSearchParams();
        if (params.tenant_id)
            search.set('tenant_id', params.tenant_id);
        if (params.node_id)
            search.set('node_id', params.node_id);
        if (params.job_id)
            search.set('job_id', params.job_id);
        if (params.scan_id)
            search.set('scan_id', params.scan_id);
        if (params.rule_id)
            search.set('rule_id', params.rule_id);
        if (typeof params.passed === 'boolean')
            search.set('passed', params.passed.toString());
        if (params.severity)
            search.set('severity', params.severity);
        if (params.since)
            search.set('since', params.since);
        if (params.until)
            search.set('until', params.until);
        if (typeof params.limit === 'number')
            search.set('limit', params.limit.toString());
        if (typeof params.offset === 'number')
            search.set('offset', params.offset.toString());
        const suffix = search.toString() ? `?${search.toString()}` : '';
        const response = await this.request(`/api/v1/compliance/results${suffix}`);
        return {
            data: response.data,
            pagination: normalizePagination(response.pagination),
        };
    }
    async getComplianceSummary(params = {}) {
        const search = new URLSearchParams();
        if (params.tenant_id)
            search.set('tenant_id', params.tenant_id);
        if (params.node_id)
            search.set('node_id', params.node_id);
        const suffix = search.toString() ? `?${search.toString()}` : '';
        return this.request(`/api/v1/compliance/summary${suffix}`);
    }
    async getComplianceTrends(params = {}) {
        const search = new URLSearchParams();
        if (params.tenant_id)
            search.set('tenant_id', params.tenant_id);
        if (params.node_id)
            search.set('node_id', params.node_id);
        if (typeof params.days === 'number')
            search.set('days', params.days.toString());
        const suffix = search.toString() ? `?${search.toString()}` : '';
        const response = await this.request(`/api/v1/compliance/trends${suffix}`);
        return response.trends || [];
    }
    async listAuditLogs(params = {}) {
        const search = new URLSearchParams();
        if (params.tenant_id)
            search.set('tenant_id', params.tenant_id);
        if (params.actor_type)
            search.set('actor_type', params.actor_type);
        if (params.action)
            search.set('action', params.action);
        if (params.resource_type)
            search.set('resource_type', params.resource_type);
        if (params.resource_id)
            search.set('resource_id', params.resource_id);
        if (params.since)
            search.set('since', params.since);
        if (params.until)
            search.set('until', params.until);
        if (typeof params.limit === 'number')
            search.set('limit', params.limit.toString());
        if (typeof params.offset === 'number')
            search.set('offset', params.offset.toString());
        const suffix = search.toString() ? `?${search.toString()}` : '';
        const response = await this.request(`/api/v1/audit${suffix}`);
        return {
            data: response.data,
            pagination: normalizePagination(response.pagination),
        };
    }
    async listTelemetryMetrics(params = {}) {
        const search = new URLSearchParams();
        if (params.tenant_id)
            search.set('tenant_id', params.tenant_id);
        if (params.node_id)
            search.set('node_id', params.node_id);
        if (params.metric_name)
            search.set('metric_name', params.metric_name);
        if (params.since)
            search.set('since', params.since);
        if (params.until)
            search.set('until', params.until);
        if (typeof params.limit === 'number')
            search.set('limit', params.limit.toString());
        if (typeof params.offset === 'number')
            search.set('offset', params.offset.toString());
        const suffix = search.toString() ? `?${search.toString()}` : '';
        const response = await this.request(`/api/v1/telemetry/metrics${suffix}`);
        return {
            data: response.data,
            pagination: normalizePagination(response.pagination),
        };
    }
    async listTelemetryLogs(params = {}) {
        const search = new URLSearchParams();
        if (params.tenant_id)
            search.set('tenant_id', params.tenant_id);
        if (params.node_id)
            search.set('node_id', params.node_id);
        if (params.log_level)
            search.set('log_level', params.log_level);
        if (params.log_source)
            search.set('log_source', params.log_source);
        if (params.since)
            search.set('since', params.since);
        if (params.until)
            search.set('until', params.until);
        if (typeof params.limit === 'number')
            search.set('limit', params.limit.toString());
        if (typeof params.offset === 'number')
            search.set('offset', params.offset.toString());
        const suffix = search.toString() ? `?${search.toString()}` : '';
        const response = await this.request(`/api/v1/telemetry/logs${suffix}`);
        return {
            data: response.data,
            pagination: normalizePagination(response.pagination),
        };
    }
    async getNodeTelemetryMetrics(nodeId, params = {}) {
        const search = new URLSearchParams();
        if (params.tenant_id)
            search.set('tenant_id', params.tenant_id);
        if (params.metric_name)
            search.set('metric_name', params.metric_name);
        const suffix = search.toString() ? `?${search.toString()}` : '';
        const encoded = encodeURIComponent(nodeId);
        const response = await this.request(`/api/v1/telemetry/nodes/${encoded}/metrics${suffix}`);
        return {
            data: response.data,
            pagination: normalizePagination(response.pagination),
        };
    }
    async listWebhooks(params = {}) {
        const search = new URLSearchParams();
        if (params.tenant_id)
            search.set('tenant_id', params.tenant_id);
        if (typeof params.enabled === 'boolean')
            search.set('enabled', params.enabled.toString());
        if (typeof params.limit === 'number')
            search.set('limit', params.limit.toString());
        if (typeof params.offset === 'number')
            search.set('offset', params.offset.toString());
        const suffix = search.toString() ? `?${search.toString()}` : '';
        const response = await this.request(`/api/v1/webhooks${suffix}`);
        return {
            data: response.items,
            pagination: {
                total: response.total,
                count: response.items.length,
                limit: response.limit,
                offset: response.offset,
                nextOffset: response.offset + response.items.length < response.total ? response.offset + response.items.length : null,
                prevOffset: response.offset > 0 ? Math.max(0, response.offset - response.limit) : null,
            },
        };
    }
    async getWebhook(webhookId) {
        const encoded = encodeURIComponent(webhookId);
        return this.request(`/api/v1/webhooks/${encoded}`);
    }
    async createWebhook(payload) {
        return this.request('/api/v1/webhooks', {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    async updateWebhook(webhookId, payload) {
        const encoded = encodeURIComponent(webhookId);
        return this.request(`/api/v1/webhooks/${encoded}`, {
            method: 'PUT',
            body: JSON.stringify(payload),
        });
    }
    async deleteWebhook(webhookId) {
        const encoded = encodeURIComponent(webhookId);
        return this.request(`/api/v1/webhooks/${encoded}`, {
            method: 'DELETE',
        });
    }
    async testWebhook(webhookId, payload) {
        const encoded = encodeURIComponent(webhookId);
        return this.request(`/api/v1/webhooks/${encoded}/test`, {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    async listSecretGroups(params = {}) {
        const search = new URLSearchParams();
        if (params.tenant_id)
            search.set('tenant_id', params.tenant_id);
        if (typeof params.limit === 'number')
            search.set('limit', params.limit.toString());
        if (typeof params.offset === 'number')
            search.set('offset', params.offset.toString());
        const suffix = search.toString() ? `?${search.toString()}` : '';
        const response = await this.request(`/api/v1/secrets/groups${suffix}`);
        return {
            data: response.data,
            pagination: normalizePagination(response.pagination),
        };
    }
    async getSecretGroup(groupId) {
        const encoded = encodeURIComponent(groupId);
        return this.request(`/api/v1/secrets/groups/${encoded}`);
    }
    async createSecretGroup(payload) {
        return this.request('/api/v1/secrets/groups', {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    async deleteSecretGroup(groupId) {
        const encoded = encodeURIComponent(groupId);
        return this.request(`/api/v1/secrets/groups/${encoded}`, {
            method: 'DELETE',
        });
    }
    async syncSecretGroup(groupId) {
        const encoded = encodeURIComponent(groupId);
        return this.request(`/api/v1/secrets/groups/${encoded}/sync`, {
            method: 'POST',
        });
    }
    async listSecretSyncs(groupId, params = {}) {
        const search = new URLSearchParams();
        if (typeof params.limit === 'number')
            search.set('limit', params.limit.toString());
        if (typeof params.offset === 'number')
            search.set('offset', params.offset.toString());
        const suffix = search.toString() ? `?${search.toString()}` : '';
        const encoded = encodeURIComponent(groupId);
        const response = await this.request(`/api/v1/secrets/groups/${encoded}/syncs${suffix}`);
        return {
            data: response.data,
            pagination: normalizePagination(response.pagination),
        };
    }
    async request(path, init = {}) {
        const response = await fetch(`${this.baseUrl}${path}`, {
            ...init,
            headers: {
                'Content-Type': 'application/json',
                ...(this.token ? { Authorization: `Bearer ${this.token}` } : {}),
                ...(init.headers ?? {}),
            },
        });
        if (!response.ok) {
            if (response.status === HTTP_STATUS_UNAUTHORIZED && this.unauthorizedHandler) {
                this.unauthorizedHandler();
            }
            const message = await safeErrorMessage(response);
            throw new APIError(message || `Request failed with status ${response.status}`, response.status);
        }
        if (response.status === 204 || response.status === 205 || response.headers.get('Content-Length') === '0') {
            return undefined;
        }
        return (await response.json());
    }
}
function normalizePagination(meta) {
    return {
        total: meta.total,
        count: meta.count,
        limit: meta.limit,
        offset: meta.offset,
        nextOffset: meta.next_offset ?? null,
        prevOffset: meta.prev_offset ?? null,
    };
}
