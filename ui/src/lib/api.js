const DEFAULT_API_BASE_URL = 'http://localhost:8443';
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
export class APIClient {
    baseUrl;
    token;
    constructor({ baseUrl, token } = {}) {
        const configured = baseUrl ?? import.meta.env.VITE_API_URL;
        const resolved = configured ? configured.replace(/\/$/, '') : DEFAULT_API_BASE_URL;
        this.baseUrl = resolved;
        this.token = token ?? null;
    }
    setToken(token) {
        this.token = token;
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
    async registerNode(payload) {
        return this.request('/api/v1/register', {
            method: 'POST',
            body: JSON.stringify(payload),
        });
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
            const message = await safeErrorMessage(response);
            throw new Error(message || `Request failed with status ${response.status}`);
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
