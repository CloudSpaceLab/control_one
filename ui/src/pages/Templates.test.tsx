import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ClusterRolloutWave, Template, TemplateAssignment, TemplateVersion } from '../lib/api';
import { Templates } from './Templates';

const mocks = vi.hoisted(() => {
  const reloadTemplates = vi.fn();
  const reloadVersions = vi.fn();
  const showToast = vi.fn();
  const listTemplateAssignments = vi.fn();
  const createTemplate = vi.fn();
  const updateTemplate = vi.fn();
  const createTemplateVersion = vi.fn();
  const promoteTemplateVersion = vi.fn();
  const createTemplateAssignment = vi.fn();
  const deleteTemplateAssignment = vi.fn();
  const listClusterRolloutWaves = vi.fn();
  const updateClusterRolloutWave = vi.fn();

  return {
    reloadTemplates,
    reloadVersions,
    showToast,
    templates: [] as Template[],
    versions: [] as TemplateVersion[],
    tenants: [{ id: 'tenant-a', name: 'Tenant A', created_at: '2026-01-01T00:00:00Z' }],
    templatesError: null as string | null,
    versionsError: null as string | null,
    apiClient: {
      listTemplateAssignments,
      createTemplate,
      updateTemplate,
      createTemplateVersion,
      promoteTemplateVersion,
      createTemplateAssignment,
      deleteTemplateAssignment,
      listClusterRolloutWaves,
      updateClusterRolloutWave,
      listNodes: vi.fn().mockResolvedValue({ data: [], pagination: { total: 0, count: 0, limit: 500, offset: 0 } }),
      listClusters: vi.fn().mockResolvedValue({ data: [], pagination: { total: 0, count: 0, limit: 500, offset: 0 } }),
      listHypervisorHosts: vi.fn().mockResolvedValue({ items: [], total: 0 }),
      listEnrollmentTokens: vi.fn().mockResolvedValue({ data: [], pagination: { total: 0, count: 0, limit: 500, offset: 0 } }),
    },
  };
});

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

vi.mock('../hooks/useTemplates', () => ({
  useTemplates: () => ({
    data: mocks.templates,
    loading: false,
    error: mocks.templatesError,
    pagination: {
      total: mocks.templates.length,
      count: mocks.templates.length,
      limit: 20,
      offset: 0,
      nextOffset: null,
      prevOffset: null,
    },
    reload: mocks.reloadTemplates,
  }),
}));

vi.mock('../hooks/useTemplateVersions', () => ({
  useTemplateVersions: () => ({
    data: mocks.versions,
    loading: false,
    error: mocks.versionsError,
    pagination: {
      total: mocks.versions.length,
      count: mocks.versions.length,
      limit: 10,
      offset: 0,
      nextOffset: null,
      prevOffset: null,
    },
    reload: mocks.reloadVersions,
  }),
}));

vi.mock('../hooks/useTenants', () => ({
  useTenants: () => ({
    data: mocks.tenants,
    loading: false,
    error: null,
    pagination: { total: mocks.tenants.length, count: mocks.tenants.length, limit: 100, offset: 0 },
    reload: vi.fn(),
  }),
}));

vi.mock('../providers/TenantProvider', () => ({
  useTenant: () => ({ currentTenantId: 'tenant-a' }),
}));

vi.mock('../providers/ToastProvider', () => ({
  useToast: () => ({ showToast: mocks.showToast }),
}));

function template(overrides: Partial<Template> = {}): Template {
  return {
    id: 'template-a',
    tenant_id: 'tenant-a',
    name: 'aws-foundation',
    provider: 'aws',
    description: 'Baseline account guardrails',
    labels: {},
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-02T00:00:00Z',
    ...overrides,
  };
}

function assignment(overrides: Partial<TemplateAssignment> = {}): TemplateAssignment {
  return {
    id: 'assignment-a',
    template_id: 'template-a',
    tenant_id: 'tenant-a',
    scope_type: 'tenant',
    selector: {},
    assigned_at: '2026-01-03T00:00:00Z',
    ...overrides,
  };
}

function wave(overrides: Partial<ClusterRolloutWave> = {}): ClusterRolloutWave {
  return {
    id: 'wave-a',
    tenant_id: 'tenant-a',
    name: 'Pilot wave',
    order: 1,
    status: 'running',
    node_count: 4,
    done_count: 1,
    created_at: '2026-01-04T00:00:00Z',
    updated_at: '2026-01-04T00:00:00Z',
    ...overrides,
  };
}

describe('Templates', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.templates = [template()];
    mocks.versions = [];
    mocks.templatesError = null;
    mocks.versionsError = null;
    mocks.apiClient.listTemplateAssignments.mockResolvedValue({ items: [], total: 0 });
    mocks.apiClient.createTemplate.mockResolvedValue(template());
    mocks.apiClient.updateTemplate.mockResolvedValue(template());
    mocks.apiClient.createTemplateVersion.mockResolvedValue({ id: 'version-a', version: 1, body: '{}', created_at: '2026-01-04T00:00:00Z' });
    mocks.apiClient.promoteTemplateVersion.mockResolvedValue({ id: 'version-a', version: 1, body: '{}', created_at: '2026-01-04T00:00:00Z' });
    mocks.apiClient.createTemplateAssignment.mockResolvedValue(assignment());
    mocks.apiClient.deleteTemplateAssignment.mockResolvedValue(undefined);
    mocks.apiClient.listClusterRolloutWaves.mockResolvedValue({ data: [], pagination: { total: 0, count: 0, limit: 0, offset: 0 } });
    mocks.apiClient.updateClusterRolloutWave.mockResolvedValue(wave({ status: 'paused' }));
  });

  it('surfaces assignment load failures instead of showing a false empty state', async () => {
    mocks.apiClient.listTemplateAssignments.mockRejectedValueOnce(new Error('assignments timed out'));

    render(<Templates />);

    expect(await screen.findByText(/template assignments unavailable: assignments timed out/i)).toBeInTheDocument();
    expect(screen.queryByText('No assignments.')).not.toBeInTheDocument();
  });

  it('surfaces version load failures instead of showing a false empty state', async () => {
    mocks.versionsError = 'versions endpoint timed out';

    render(<Templates />);

    expect(await screen.findByText(/template versions unavailable: versions endpoint timed out/i)).toBeInTheDocument();
    expect(screen.queryByText('No versions published yet.')).not.toBeInTheDocument();
  });

  it('surfaces rollout load failures instead of showing a false empty state', async () => {
    const user = userEvent.setup();
    mocks.apiClient.listClusterRolloutWaves.mockRejectedValueOnce(new Error('rollout API unavailable'));

    render(<Templates />);
    await user.click(screen.getByRole('button', { name: /rollouts/i }));

    expect(await screen.findByText(/rollout waves unavailable: rollout API unavailable/i)).toBeInTheDocument();
    expect(screen.queryByText('No rollout waves')).not.toBeInTheDocument();
  });

  it('keeps failed rollout actions visible on the page', async () => {
    const user = userEvent.setup();
    mocks.apiClient.listClusterRolloutWaves.mockResolvedValueOnce({
      data: [wave()],
      pagination: { total: 1, count: 1, limit: 20, offset: 0 },
    });
    mocks.apiClient.updateClusterRolloutWave.mockRejectedValueOnce(new Error('pause denied'));

    render(<Templates />);
    await user.click(screen.getByRole('button', { name: /rollouts/i }));
    await screen.findByText('Pilot wave');
    await user.click(screen.getByRole('button', { name: /pause/i }));

    await waitFor(() => expect(mocks.apiClient.updateClusterRolloutWave).toHaveBeenCalledWith('wave-a', { status: 'paused' }));
    expect(await screen.findByText(/rollout waves unavailable: pause denied/i)).toBeInTheDocument();
  });

  it('keeps failed assignment removals inside the confirmation modal', async () => {
    const user = userEvent.setup();
    mocks.apiClient.listTemplateAssignments.mockResolvedValueOnce({ items: [assignment()], total: 1 });
    mocks.apiClient.deleteTemplateAssignment.mockRejectedValueOnce(new Error('assignment is locked'));

    render(<Templates />);
    await screen.findByText('Org-wide');
    await user.click(screen.getByRole('button', { name: /^remove$/i }));
    const dialog = screen.getByRole('dialog', { name: /remove template assignment/i });

    await user.click(within(dialog).getByRole('button', { name: /remove assignment/i }));

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('assignment is locked');
    expect(screen.getByRole('dialog', { name: /remove template assignment/i })).toBeInTheDocument();
    expect(screen.getAllByText('Org-wide').length).toBeGreaterThan(0);
  });
});
