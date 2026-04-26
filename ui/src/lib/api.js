const DEFAULT_API_BASE_URL = 'http://localhost:8443';
const HTTP_STATUS_UNAUTHORIZED = 401;
async function safeErrorMessage(response) {
    try {
        const data = await response.json();
        if (data && typeof data.message === 'string') {
            return data.message;
        }
    }
    catch {
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
    // Email/password login. Returns a session token the caller stores via
    // AuthProvider.signIn; from then on every request is Bearer-authed
    // exactly like the legacy static-token path.
    async loginWithPassword(email, password) {
        return this.request('/api/v1/auth/login', {
            method: 'POST',
            body: JSON.stringify({ email, password }),
        });
    }
    async logout() {
        await this.request('/api/v1/auth/logout', { method: 'POST' });
    }
    async getCurrentUser() {
        return this.request('/api/v1/auth/me');
    }
    // ---- RBAC ---------------------------------------------------------
    async listPermissions() {
        return this.request('/api/v1/permissions');
    }
    async listRolesWithPermissions() {
        return this.request('/api/v1/roles/permissions');
    }
    async setRolePermissions(roleId, permissions) {
        await this.request(`/api/v1/roles/${encodeURIComponent(roleId)}/permissions`, {
            method: 'PUT',
            body: JSON.stringify({ permissions }),
        });
    }
    async createCustomRole(payload) {
        return this.request('/api/v1/roles/', {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    async deleteRole(roleId) {
        await this.request(`/api/v1/roles/${encodeURIComponent(roleId)}`, { method: 'DELETE' });
    }
    // ---- Custom dashboards -------------------------------------------
    async listDashboards(tenantId) {
        return this.request(`/api/v1/dashboards?tenant_id=${encodeURIComponent(tenantId)}`);
    }
    async getDashboard(id) {
        return this.request(`/api/v1/dashboards/${encodeURIComponent(id)}`);
    }
    async createDashboard(payload) {
        return this.request('/api/v1/dashboards', {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    async updateDashboard(id, payload) {
        await this.request(`/api/v1/dashboards/${encodeURIComponent(id)}`, {
            method: 'PATCH',
            body: JSON.stringify(payload),
        });
    }
    async deleteDashboard(id) {
        await this.request(`/api/v1/dashboards/${encodeURIComponent(id)}`, { method: 'DELETE' });
    }
    async createWidget(dashboardId, payload) {
        return this.request(`/api/v1/dashboards/${encodeURIComponent(dashboardId)}/widgets`, {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    async updateWidget(dashboardId, widgetId, payload) {
        await this.request(`/api/v1/dashboards/${encodeURIComponent(dashboardId)}/widgets/${encodeURIComponent(widgetId)}`, {
            method: 'PATCH',
            body: JSON.stringify(payload),
        });
    }
    async deleteWidget(dashboardId, widgetId) {
        await this.request(`/api/v1/dashboards/${encodeURIComponent(dashboardId)}/widgets/${encodeURIComponent(widgetId)}`, { method: 'DELETE' });
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
    // ── Fleet enrollment (Sprint 2 Pillar 1.7) ────────────────────────────
    async startFleetEnroll(payload) {
        return this.request('/api/v1/fleet/enroll', {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    async getFleetEnrollStatus(jobId) {
        const encoded = encodeURIComponent(jobId);
        return this.request(`/api/v1/fleet/enroll/${encoded}`);
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
    async listEnrollmentTokens(params = {}) {
        const search = new URLSearchParams();
        if (params.tenant_id)
            search.set('tenant_id', params.tenant_id);
        if (typeof params.limit === 'number')
            search.set('limit', params.limit.toString());
        if (typeof params.offset === 'number')
            search.set('offset', params.offset.toString());
        const suffix = search.toString() ? `?${search.toString()}` : '';
        const response = await this.request(`/api/v1/enrollment-tokens${suffix}`);
        return {
            data: response.data,
            pagination: normalizePagination(response.pagination),
        };
    }
    async listProviderCredentials(params = {}) {
        const search = new URLSearchParams();
        if (params.tenantId)
            search.set('tenant_id', params.tenantId);
        if (params.provider)
            search.set('provider', params.provider);
        if (typeof params.limit === 'number')
            search.set('limit', params.limit.toString());
        if (typeof params.offset === 'number')
            search.set('offset', params.offset.toString());
        const suffix = search.toString() ? `?${search.toString()}` : '';
        return this.request(`/api/v1/provider-credentials${suffix}`);
    }
    async createProviderCredential(payload) {
        return this.request(`/api/v1/provider-credentials`, {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    async rotateProviderCredential(id, config) {
        const encoded = encodeURIComponent(id);
        return this.request(`/api/v1/provider-credentials/${encoded}/rotate`, {
            method: 'POST',
            body: JSON.stringify({ config }),
        });
    }
    async deleteProviderCredential(id) {
        const encoded = encodeURIComponent(id);
        await this.request(`/api/v1/provider-credentials/${encoded}`, { method: 'DELETE' });
    }
    async listHypervisorHosts(params = {}) {
        const search = new URLSearchParams();
        if (params.tenantId)
            search.set('tenant_id', params.tenantId);
        if (params.provider)
            search.set('provider', params.provider);
        if (typeof params.limit === 'number')
            search.set('limit', params.limit.toString());
        if (typeof params.offset === 'number')
            search.set('offset', params.offset.toString());
        const suffix = search.toString() ? `?${search.toString()}` : '';
        return this.request(`/api/v1/hypervisor-hosts${suffix}`);
    }
    async createHypervisorHost(payload) {
        return this.request(`/api/v1/hypervisor-hosts`, {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    async deleteHypervisorHost(id) {
        const encoded = encodeURIComponent(id);
        await this.request(`/api/v1/hypervisor-hosts/${encoded}`, { method: 'DELETE' });
    }
    async verifyHypervisorHost(id) {
        const encoded = encodeURIComponent(id);
        return this.request(`/api/v1/hypervisor-hosts/${encoded}/verify`, {
            method: 'POST',
        });
    }
    async getDashboardOverview(tenantId) {
        const search = new URLSearchParams();
        if (tenantId) {
            search.set('tenant_id', tenantId);
        }
        const qs = search.toString();
        const path = `/api/v1/dashboard/overview${qs ? `?${qs}` : ''}`;
        return this.request(path);
    }
    async listPortRules(params = {}) {
        const search = new URLSearchParams();
        if (params.tenantId)
            search.set('tenant_id', params.tenantId);
        if (params.policyId)
            search.set('policy_id', params.policyId);
        if (typeof params.enabled === 'boolean')
            search.set('enabled', String(params.enabled));
        if (typeof params.limit === 'number')
            search.set('limit', String(params.limit));
        if (typeof params.offset === 'number')
            search.set('offset', String(params.offset));
        const qs = search.toString();
        return this.request(`/api/v1/rules/port${qs ? `?${qs}` : ''}`);
    }
    async createPortRule(payload) {
        return this.request('/api/v1/rules/port', {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    async deletePortRule(id) {
        await this.request(`/api/v1/rules/port/${encodeURIComponent(id)}`, { method: 'DELETE' });
    }
    async listLogRules(params = {}) {
        const search = new URLSearchParams();
        if (params.tenantId)
            search.set('tenant_id', params.tenantId);
        if (params.policyId)
            search.set('policy_id', params.policyId);
        if (params.logSource)
            search.set('log_source', params.logSource);
        if (typeof params.enabled === 'boolean')
            search.set('enabled', String(params.enabled));
        if (typeof params.limit === 'number')
            search.set('limit', String(params.limit));
        if (typeof params.offset === 'number')
            search.set('offset', String(params.offset));
        const qs = search.toString();
        return this.request(`/api/v1/rules/log${qs ? `?${qs}` : ''}`);
    }
    async createLogRule(payload) {
        return this.request('/api/v1/rules/log', {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    async deleteLogRule(id) {
        await this.request(`/api/v1/rules/log/${encodeURIComponent(id)}`, { method: 'DELETE' });
    }
    async listAlerts(params = {}) {
        const search = new URLSearchParams();
        if (params.tenantId)
            search.set('tenant_id', params.tenantId);
        if (params.state)
            search.set('state', params.state);
        if (params.severity)
            search.set('severity', params.severity);
        if (typeof params.limit === 'number')
            search.set('limit', String(params.limit));
        if (typeof params.offset === 'number')
            search.set('offset', String(params.offset));
        const qs = search.toString();
        return this.request(`/api/v1/alerts${qs ? `?${qs}` : ''}`);
    }
    async ackAlert(id) {
        await this.request(`/api/v1/alerts/${encodeURIComponent(id)}/ack`, { method: 'POST' });
    }
    async resolveAlert(id) {
        await this.request(`/api/v1/alerts/${encodeURIComponent(id)}/resolve`, { method: 'POST' });
    }
    async listAccessRequests(params = {}) {
        const search = new URLSearchParams();
        if (params.tenantId)
            search.set('tenant_id', params.tenantId);
        if (params.status)
            search.set('status', params.status);
        if (typeof params.limit === 'number')
            search.set('limit', String(params.limit));
        if (typeof params.offset === 'number')
            search.set('offset', String(params.offset));
        const qs = search.toString();
        return this.request(`/api/v1/access-requests${qs ? `?${qs}` : ''}`);
    }
    async createAccessRequest(payload) {
        return this.request('/api/v1/access-requests', {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    async approveAccessRequest(id, reason = '') {
        return this.request(`/api/v1/access-requests/${encodeURIComponent(id)}/approve`, {
            method: 'POST',
            body: JSON.stringify({ reason }),
        });
    }
    async denyAccessRequest(id, reason = '') {
        return this.request(`/api/v1/access-requests/${encodeURIComponent(id)}/deny`, {
            method: 'POST',
            body: JSON.stringify({ reason }),
        });
    }
    async listRecommendations(tenantId) {
        const search = new URLSearchParams();
        search.set('tenant_id', tenantId);
        return this.request(`/api/v1/compliance/recommendations?${search.toString()}`);
    }
    async listReports() {
        return this.request('/api/v1/reports');
    }
    async listSessions(params = {}) {
        const search = new URLSearchParams();
        if (params.nodeId)
            search.set('node_id', params.nodeId);
        if (typeof params.limit === 'number')
            search.set('limit', String(params.limit));
        if (typeof params.offset === 'number')
            search.set('offset', String(params.offset));
        const qs = search.toString();
        return this.request(`/api/v1/sessions${qs ? `?${qs}` : ''}`);
    }
    async getSessionParsed(id, search) {
        const qs = new URLSearchParams();
        if (search)
            qs.set('search', search);
        const suffix = qs.toString();
        return this.request(`/api/v1/sessions/${encodeURIComponent(id)}/parsed${suffix ? `?${suffix}` : ''}`);
    }
    async getSessionTranscript(id) {
        const resp = await fetch(`${this.baseUrl}/api/v1/sessions/${encodeURIComponent(id)}/transcript`, {
            headers: { ...(this.token ? { Authorization: `Bearer ${this.token}` } : {}) },
        });
        if (!resp.ok)
            throw new APIError('Failed to load transcript', resp.status);
        return resp.text();
    }
    async listThreatFeeds(tenantId) {
        const search = new URLSearchParams();
        search.set('tenant_id', tenantId);
        return this.request(`/api/v1/threat-feeds?${search.toString()}`);
    }
    async createThreatFeed(payload) {
        return this.request('/api/v1/threat-feeds', {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    async updateThreatFeed(id, payload) {
        return this.request(`/api/v1/threat-feeds/${encodeURIComponent(id)}`, {
            method: 'PATCH',
            body: JSON.stringify(payload),
        });
    }
    async deleteThreatFeed(id) {
        await this.request(`/api/v1/threat-feeds/${encodeURIComponent(id)}`, { method: 'DELETE' });
    }
    async simulateRule(payload) {
        return this.request('/api/v1/compliance/simulate', {
            method: 'POST',
            body: JSON.stringify(payload),
        });
    }
    buildReportExportUrl(slug, params = {}) {
        const search = new URLSearchParams();
        if (params.tenantId)
            search.set('tenant_id', params.tenantId);
        if (params.since)
            search.set('since', params.since);
        const qs = search.toString();
        return `${this.baseUrl}/api/v1/reports/${encodeURIComponent(slug)}${qs ? `?${qs}` : ''}`;
    }
    // streamEvents opens an authenticated Server-Sent Events stream using
    // fetch + ReadableStream. Browsers' native EventSource cannot set custom
    // Authorization headers, so we parse the SSE wire format ourselves. Returns
    // an AbortController.abort-style cleanup function.
    streamEvents(opts, onEvent, onError) {
        const controller = new AbortController();
        const search = new URLSearchParams();
        search.set('tenant_id', opts.tenantId);
        if (opts.nodeId)
            search.set('node_id', opts.nodeId);
        if (opts.topics && opts.topics.length)
            search.set('topics', opts.topics.join(','));
        const url = `${this.baseUrl}/api/v1/events/stream?${search.toString()}`;
        const run = async () => {
            try {
                const resp = await fetch(url, {
                    headers: {
                        Accept: 'text/event-stream',
                        ...(this.token ? { Authorization: `Bearer ${this.token}` } : {}),
                    },
                    signal: controller.signal,
                });
                if (!resp.ok || !resp.body) {
                    throw new Error(`events stream status ${resp.status}`);
                }
                const reader = resp.body.getReader();
                const decoder = new TextDecoder();
                let buf = '';
                while (!controller.signal.aborted) {
                    const { value, done } = await reader.read();
                    if (done)
                        break;
                    buf += decoder.decode(value, { stream: true });
                    let idx;
                    while ((idx = buf.indexOf('\n\n')) !== -1) {
                        const frame = buf.slice(0, idx);
                        buf = buf.slice(idx + 2);
                        const dataLine = frame.split('\n').find((l) => l.startsWith('data: '));
                        if (!dataLine)
                            continue;
                        try {
                            const parsed = JSON.parse(dataLine.slice(6));
                            onEvent(parsed);
                        }
                        catch {
                            // ignore malformed frame
                        }
                    }
                }
            }
            catch (err) {
                if (!controller.signal.aborted && onError) {
                    onError(err);
                }
            }
        };
        void run();
        return () => controller.abort();
    }
    // buildBundleDownloadUrl returns the fully qualified GET URL for the air-gapped
    // bundle endpoint. The wizard points `window.location` at this URL so the
    // browser handles the tarball download directly.
    buildBundleDownloadUrl(options) {
        const search = new URLSearchParams();
        search.set('os', options.os);
        search.set('arch', options.arch);
        search.set('token', options.token);
        return `${this.baseUrl}/api/v1/agent/bundle?${search.toString()}`;
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
    // ---- Connections / forensics (Phase 7) -------------------------------
    async listConnections(params = {}) {
        const search = new URLSearchParams();
        if (params.tenantId)
            search.set('tenant_id', params.tenantId);
        if (params.ip)
            search.set('ip', params.ip);
        if (params.since)
            search.set('since', params.since);
        if (params.until)
            search.set('until', params.until);
        if (typeof params.limit === 'number')
            search.set('limit', String(params.limit));
        const q = search.toString();
        return this.request(`/api/v1/connections${q ? `?${q}` : ''}`);
    }
    async getConnectionDetail(connID) {
        return this.request(`/api/v1/connections/${encodeURIComponent(connID)}`);
    }
    async listTopTalkers(params = {}) {
        const search = new URLSearchParams();
        if (params.tenantId)
            search.set('tenant_id', params.tenantId);
        if (params.since)
            search.set('since', params.since);
        if (typeof params.limit === 'number')
            search.set('limit', String(params.limit));
        const q = search.toString();
        return this.request(`/api/v1/connections/top-talkers${q ? `?${q}` : ''}`);
    }
    async fleetHealthSnapshot(params = {}) {
        const search = new URLSearchParams();
        if (params.tenantId)
            search.set('tenant_id', params.tenantId);
        if (params.since)
            search.set('since', params.since);
        const q = search.toString();
        return this.request(`/api/v1/fleet/health${q ? `?${q}` : ''}`);
    }
    async getTenantEventFilters(tenantId) {
        return this.request(`/api/v1/tenants/${encodeURIComponent(tenantId)}/event-filters`);
    }
    async updateTenantEventFilters(tenantId, payload) {
        return this.request(`/api/v1/tenants/${encodeURIComponent(tenantId)}/event-filters`, {
            method: 'PUT',
            body: JSON.stringify(payload),
        });
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
