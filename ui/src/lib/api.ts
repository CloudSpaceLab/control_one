const DEFAULT_API_BASE_URL = 'http://localhost:8443';
const HTTP_STATUS_UNAUTHORIZED = 401;

export interface Tenant {
  id: string;
  name: string;
  created_at: string;
}

export interface UpdateTenantPayload {
  name: string;
}

export interface CreateTenantPayload {
  name: string;
}

export interface NodeSummary {
  id: string;
  tenant_id: string;
  hostname: string;
  os?: string;
  arch?: string;
  public_ip?: string;
  created_at: string;
  updated_at: string;
}

export interface RegisterNodePayload {
  tenant_id?: string;
  tenant_name?: string;
  hostname: string;
  os?: string;
  arch?: string;
  public_ip?: string;
  bootstrap_token: string;
}

export interface RegisterNodeResponse {
  node_id: string;
  tenant_id: string;
  intervals: Record<string, number>;
  provisioning_hints: string;
}

export type Node = NodeSummary;

export interface UpdateNodePayload {
  hostname?: string;
  os?: string;
  arch?: string;
  public_ip?: string;
}

export interface APIClientOptions {
  baseUrl?: string;
  token?: string | null;
}

export interface ProfileUserDetails {
  id: string;
  display_name?: string;
  email?: string;
  created_at: string;
}

export interface Profile {
  subject: string;
  name: string;
  email: string;
  type: string;
  roles: string[];
  groups: string[];
  stored_roles?: string[];
  user?: ProfileUserDetails;
}

export interface User {
  id: string;
  external_id: string;
  display_name?: string;
  email?: string;
  roles: string[];
  created_at: string;
}

export interface Role {
  id: string;
  name: string;
  description?: string;
  created_at: string;
}

export interface ListUsersParams {
  limit?: number;
  offset?: number;
}

export interface UpdateUserRolesPayload {
  roles: string[];
}

export type JobStatus =
  | 'queued'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'cancelled';

export interface JobEvent {
  id: string;
  status: JobStatus | string;
  message?: string;
  created_at: string;
}

export interface Job {
  id: string;
  tenant_id?: string;
  type: string;
  status: JobStatus | string;
  payload?: unknown;
  retries: number;
  max_retries: number;
  scheduled_at?: string;
  started_at?: string;
  finished_at?: string;
  created_at: string;
  updated_at: string;
  events?: JobEvent[];
}

export interface WorkerStatus {
  backend: string;
  started: boolean;
  queue_depth: number;
  active: number;
  last_error?: string;
}

export interface CreateJobRequest {
  type: string;
  tenant_id?: string;
  payload?: unknown;
  max_retries?: number;
}

export interface ListJobsParams {
  tenantId?: string;
  status?: JobStatus | string;
  type?: string;
  limit?: number;
  offset?: number;
}

export interface ListTenantsParams {
  namePrefix?: string;
  limit?: number;
  offset?: number;
}

export interface ListNodesParams {
  tenantId?: string;
  hostnamePrefix?: string;
  limit?: number;
  offset?: number;
}

export interface Template {
  id: string;
  name: string;
  provider: string;
  description?: string;
  labels: Record<string, string>;
  created_at: string;
  updated_at: string;
  archived_at?: string;
  promoted_version_id?: string;
  promoted_version?: TemplateVersion;
}

export interface TemplateVersion {
  id: string;
  version: number;
  checksum?: string;
  body: string;
  metadata_schema?: unknown;
  rollout_notes?: string;
  created_by?: string;
  created_at: string;
  promoted_at?: string;
}

export interface ListTemplatesParams {
  provider?: string;
  namePrefix?: string;
  includeArchived?: boolean;
  limit?: number;
  offset?: number;
}

export interface CreateTemplatePayload {
  name: string;
  provider: string;
  description?: string;
  labels?: Record<string, string>;
}

export interface UpdateTemplatePayload {
  name?: string;
  provider?: string;
  description?: string;
  labels?: Record<string, string>;
  archived?: boolean;
}

export interface ListTemplateVersionsParams {
  limit?: number;
  offset?: number;
}

export interface CreateTemplateVersionPayload {
  body: string;
  checksum?: string;
  metadata_schema?: unknown;
  rollout_notes?: string;
}

interface ServerPaginationMeta {
  total: number;
  count: number;
  limit: number;
  offset: number;
  next_offset?: number | null;
  prev_offset?: number | null;
}

export interface PaginationMeta {
  total: number;
  count: number;
  limit: number;
  offset: number;
  nextOffset: number | null;
  prevOffset: number | null;
}

interface RawPaginatedResponse<T> {
  data: T[];
  pagination: ServerPaginationMeta;
}

export interface PaginatedResponse<T> {
  data: T[];
  pagination: PaginationMeta;
}

async function safeErrorMessage(response: Response): Promise<string | undefined> {
  try {
    const data = await response.json();
    if (data && typeof data.message === 'string') {
      return data.message;
    }
  } catch (error) {
    // ignore json parse errors
  }
  return response.statusText;
}

export class APIError extends Error {
  public readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = 'APIError';
    this.status = status;
  }
}

export class APIClient {
  private readonly baseUrl: string;
  private token: string | null | undefined;
  private unauthorizedHandler?: () => void;

  constructor({ baseUrl, token }: APIClientOptions = {}) {
    const configured = baseUrl ?? (import.meta.env.VITE_API_URL as string | undefined);
    const resolved = configured ? configured.replace(/\/$/, '') : DEFAULT_API_BASE_URL;
    this.baseUrl = resolved;
    this.token = token ?? null;
  }

  setToken(token: string | null): void {
    this.token = token;
  }

  onUnauthorized(handler?: () => void): void {
    this.unauthorizedHandler = handler;
  }

  async getProfile(): Promise<Profile> {
    return this.request<Profile>('/api/v1/me');
  }

  async listTenants(params: ListTenantsParams = {}): Promise<PaginatedResponse<Tenant>> {
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
    const response = await this.request<RawPaginatedResponse<Tenant>>(`/api/v1/tenants${suffix}`);
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async listNodes(options: ListNodesParams = {}): Promise<PaginatedResponse<NodeSummary>> {
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
    const response = await this.request<RawPaginatedResponse<NodeSummary>>(`/api/v1/nodes${suffix}`);
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async listJobs(params: ListJobsParams = {}): Promise<PaginatedResponse<Job>> {
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
    const response = await this.request<RawPaginatedResponse<Job>>(`/api/v1/jobs${suffix}`);
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async getJob(jobId: string): Promise<Job> {
    const encoded = encodeURIComponent(jobId);
    return this.request<Job>(`/api/v1/jobs/${encoded}`);
  }

  async cancelJob(jobId: string): Promise<Job> {
    const encoded = encodeURIComponent(jobId);
    return this.request<Job>(`/api/v1/jobs/${encoded}/cancel`, {
      method: 'POST',
    });
  }

  async getWorkerStatus(): Promise<WorkerStatus> {
    return this.request<WorkerStatus>('/api/v1/worker/status');
  }

  async createJob(payload: CreateJobRequest): Promise<Job> {
    return this.request<Job>('/api/v1/jobs', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async createTenant(payload: CreateTenantPayload): Promise<Tenant> {
    return this.request<Tenant>('/api/v1/tenants', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async getTenant(tenantId: string): Promise<Tenant> {
    const encoded = encodeURIComponent(tenantId);
    return this.request<Tenant>(`/api/v1/tenants/${encoded}`);
  }

  async updateTenant(tenantId: string, payload: UpdateTenantPayload): Promise<Tenant> {
    const encoded = encodeURIComponent(tenantId);
    return this.request<Tenant>(`/api/v1/tenants/${encoded}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    });
  }

  async deleteTenant(tenantId: string): Promise<void> {
    const encoded = encodeURIComponent(tenantId);
    await this.request<void>(`/api/v1/tenants/${encoded}`, { method: 'DELETE' });
  }

  async registerNode(payload: RegisterNodePayload): Promise<RegisterNodeResponse> {
    return this.request<RegisterNodeResponse>('/api/v1/register', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async getNode(nodeId: string): Promise<Node> {
    const encoded = encodeURIComponent(nodeId);
    return this.request<Node>(`/api/v1/nodes/${encoded}`);
  }

  async updateNode(nodeId: string, payload: UpdateNodePayload): Promise<Node> {
    const encoded = encodeURIComponent(nodeId);
    return this.request<Node>(`/api/v1/nodes/${encoded}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    });
  }

  async deleteNode(nodeId: string): Promise<void> {
    const encoded = encodeURIComponent(nodeId);
    await this.request<void>(`/api/v1/nodes/${encoded}`, { method: 'DELETE' });
  }

  async listUsers(params: ListUsersParams = {}): Promise<PaginatedResponse<User>> {
    const search = new URLSearchParams();
    if (typeof params.limit === 'number') {
      search.set('limit', params.limit.toString());
    }
    if (typeof params.offset === 'number') {
      search.set('offset', params.offset.toString());
    }
    const suffix = search.toString() ? `?${search.toString()}` : '';
    const response = await this.request<RawPaginatedResponse<User>>(`/api/v1/users${suffix}`);
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async getUser(userID: string): Promise<User> {
    const encoded = encodeURIComponent(userID);
    return this.request<User>(`/api/v1/users/${encoded}`);
  }

  async updateUserRoles(userID: string, payload: UpdateUserRolesPayload): Promise<User> {
    const encoded = encodeURIComponent(userID);
    return this.request<User>(`/api/v1/users/${encoded}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    });
  }

  async listRoles(): Promise<Role[]> {
    return this.request<Role[]>('/api/v1/roles');
  }

  async listTemplates(params: ListTemplatesParams = {}): Promise<PaginatedResponse<Template>> {
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
    const response = await this.request<RawPaginatedResponse<Template>>(`/api/v1/templates${suffix}`);
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async createTemplate(payload: CreateTemplatePayload): Promise<Template> {
    return this.request<Template>('/api/v1/templates', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async getTemplate(templateId: string): Promise<Template> {
    const encoded = encodeURIComponent(templateId);
    return this.request<Template>(`/api/v1/templates/${encoded}`);
  }

  async updateTemplate(templateId: string, payload: UpdateTemplatePayload): Promise<Template> {
    const encoded = encodeURIComponent(templateId);
    return this.request<Template>(`/api/v1/templates/${encoded}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    });
  }

  async listTemplateVersions(
    templateId: string,
    params: ListTemplateVersionsParams = {},
  ): Promise<PaginatedResponse<TemplateVersion>> {
    const search = new URLSearchParams();
    if (typeof params.limit === 'number') {
      search.set('limit', params.limit.toString());
    }
    if (typeof params.offset === 'number') {
      search.set('offset', params.offset.toString());
    }
    const suffix = search.toString() ? `?${search.toString()}` : '';
    const encoded = encodeURIComponent(templateId);
    const response = await this.request<RawPaginatedResponse<TemplateVersion>>(
      `/api/v1/templates/${encoded}/versions${suffix}`,
    );
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async createTemplateVersion(
    templateId: string,
    payload: CreateTemplateVersionPayload,
  ): Promise<TemplateVersion> {
    const encoded = encodeURIComponent(templateId);
    return this.request<TemplateVersion>(`/api/v1/templates/${encoded}/versions`, {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async promoteTemplateVersion(templateId: string, versionNumber: number): Promise<TemplateVersion> {
    const encoded = encodeURIComponent(templateId);
    return this.request<TemplateVersion>(
      `/api/v1/templates/${encoded}/versions/${versionNumber}/promote`,
      { method: 'POST' },
    );
  }

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
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
      return undefined as T;
    }

    return (await response.json()) as T;
  }
}

function normalizePagination(meta: ServerPaginationMeta): PaginationMeta {
  return {
    total: meta.total,
    count: meta.count,
    limit: meta.limit,
    offset: meta.offset,
    nextOffset: meta.next_offset ?? null,
    prevOffset: meta.prev_offset ?? null,
  };
}
