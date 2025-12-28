const DEFAULT_API_BASE_URL = 'http://localhost:8443';

export interface Tenant {
  id: string;
  name: string;
  created_at: string;
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

export interface APIClientOptions {
  baseUrl?: string;
  token?: string | null;
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

export class APIClient {
  private readonly baseUrl: string;
  private token: string | null | undefined;

  constructor({ baseUrl, token }: APIClientOptions = {}) {
    const configured = baseUrl ?? (import.meta.env.VITE_API_URL as string | undefined);
    const resolved = configured ? configured.replace(/\/$/, '') : DEFAULT_API_BASE_URL;
    this.baseUrl = resolved;
    this.token = token ?? null;
  }

  setToken(token: string | null): void {
    this.token = token;
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

  async registerNode(payload: RegisterNodePayload): Promise<RegisterNodeResponse> {
    return this.request<RegisterNodeResponse>('/api/v1/register', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
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
      const message = await safeErrorMessage(response);
      throw new Error(message || `Request failed with status ${response.status}`);
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
