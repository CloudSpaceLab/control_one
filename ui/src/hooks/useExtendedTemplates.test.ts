import { renderHook, act } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useExtendedTemplates } from './useExtendedTemplates';
import { TemplateType } from '../lib/extendedTemplateTypes';

// Mock the dependencies
vi.mock('./useApiClient', () => ({
  useApiClient: () => ({
    listTemplates: vi.fn(),
    createTemplate: vi.fn(),
    updateTemplate: vi.fn(),
    archiveTemplate: vi.fn(),
    unarchiveTemplate: vi.fn(),
    executeTemplate: vi.fn(),
  })
}));

vi.mock('./useApiErrorHandler', () => ({
  useApiErrorHandler: () => (error: any, message: string) => message
}));

describe('useExtendedTemplates', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockHandleError.mockReturnValue('Test error message');
    
    mockApiClient.mockReturnValue({
      listTemplates: vi.fn(),
      createTemplate: vi.fn(),
      updateTemplate: vi.fn(),
      archiveTemplate: vi.fn(),
      unarchiveTemplate: vi.fn(),
      executeTemplate: vi.fn(),
    } as any);
  });

  it('should initialize with loading state', () => {
    mockApiClient().listTemplates.mockResolvedValue({
      data: [],
      pagination: { total: 0, limit: 100, offset: 0, hasMore: false }
    });

    const { result } = renderHook(() => useExtendedTemplates());

    expect(result.current.loading).toBe(true);
    expect(result.current.data).toEqual([]);
    expect(result.current.error).toBe(null);
  });

  it('should fetch templates successfully', async () => {
    const mockTemplates = [
      {
        id: 'test-1',
        name: 'Test Template',
        provider: 'ansible',
        description: 'Test description',
        labels: { template_type: 'job' },
        template_type: 'job',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z'
      }
    ];

    mockApiClient().listTemplates.mockResolvedValue({
      data: mockTemplates,
      pagination: { total: 1, limit: 100, offset: 0, hasMore: false }
    });

    const { result } = renderHook(() => useExtendedTemplates());

    await act(async () => {
      await new Promise(resolve => setTimeout(resolve, 0));
    });

    expect(result.current.loading).toBe(false);
    expect(result.current.data).toHaveLength(1);
    expect(result.current.data[0].type).toBe(TemplateType.JOB);
    expect(result.current.error).toBe(null);
    expect(result.current.summary.total).toBe(1);
  });

  it('should handle API errors gracefully', async () => {
    mockApiClient().listTemplates.mockRejectedValue(new Error('API Error'));

    const { result } = renderHook(() => useExtendedTemplates());

    await act(async () => {
      await new Promise(resolve => setTimeout(resolve, 0));
    });

    expect(result.current.loading).toBe(false);
    expect(result.current.data).toEqual([]);
    expect(result.current.error).toContain('Backend unavailable');
    expect(result.current.summary.total).toBe(0);
  });

  it('should filter templates correctly', async () => {
    const mockTemplates = [
      {
        id: 'job-1',
        name: 'Job Template',
        provider: 'ansible',
        labels: { template_type: 'job' },
        template_type: 'job',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z'
      },
      {
        id: 'config-1',
        name: 'Config Template',
        provider: 'terraform',
        labels: { template_type: 'config' },
        template_type: 'config',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z'
      }
    ];

    mockApiClient().listTemplates.mockResolvedValue({
      data: mockTemplates,
      pagination: { total: 2, limit: 100, offset: 0, hasMore: false }
    });

    const { result } = renderHook(() => useExtendedTemplates());

    await act(async () => {
      await new Promise(resolve => setTimeout(resolve, 0));
    });

    // Test filtering by type
    const jobTemplates = result.current.filter({ type: TemplateType.JOB });
    expect(jobTemplates).toHaveLength(1);
    expect(jobTemplates[0].type).toBe(TemplateType.JOB);

    // Test filtering by provider
    const ansibleTemplates = result.current.filter({ provider: 'ansible' });
    expect(ansibleTemplates).toHaveLength(1);
    expect(ansibleTemplates[0].provider).toBe('ansible');

    // Test filtering by name prefix
    const jobNamedTemplates = result.current.filter({ name_prefix: 'Job' });
    expect(jobNamedTemplates).toHaveLength(1);
    expect(jobNamedTemplates[0].name).toBe('Job Template');
  });

  it('should create template successfully', async () => {
    const mockTemplate = {
      id: 'new-1',
      name: 'New Template',
      provider: 'ansible',
      description: 'New description',
      labels: { template_type: 'job' },
      template_type: 'job',
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z'
    };

    mockApiClient().listTemplates.mockResolvedValue({
      data: [],
      pagination: { total: 0, limit: 100, offset: 0, hasMore: false }
    });

    mockApiClient().createTemplate.mockResolvedValue(mockTemplate);

    const { result } = renderHook(() => useExtendedTemplates());

    await act(async () => {
      await new Promise(resolve => setTimeout(resolve, 0));
    });

    const newTemplate = {
      name: 'New Template',
      provider: 'ansible',
      description: 'New description',
      labels: { template_type: 'job' },
      type: TemplateType.JOB,
      job_type: 'provision',
      default_payload: {},
      retry_config: { max_retries: 3 }
    };

    const created = await act(async () => {
      return await result.current.createTemplate(newTemplate);
    });

    expect(mockApiClient().createTemplate).toHaveBeenCalledWith({
      name: 'New Template',
      provider: 'ansible',
      description: 'New description',
      labels: { template_type: 'job' },
      template_type: 'job'
    });

    expect(created.type).toBe(TemplateType.JOB);
    expect(created.name).toBe('New Template');
  });

  it('should handle template creation error', async () => {
    mockApiClient().listTemplates.mockResolvedValue({
      data: [],
      pagination: { total: 0, limit: 100, offset: 0, hasMore: false }
    });

    mockApiClient().createTemplate.mockRejectedValue(new Error('Creation failed'));

    const { result } = renderHook(() => useExtendedTemplates());

    await act(async () => {
      await new Promise(resolve => setTimeout(resolve, 0));
    });

    const newTemplate = {
      name: 'New Template',
      provider: 'ansible',
      type: TemplateType.JOB,
      job_type: 'provision',
      default_payload: {},
      retry_config: { max_retries: 3 }
    };

    await expect(result.current.createTemplate(newTemplate)).rejects.toThrow('Failed to create template');
  });

  it('should reload templates', async () => {
    let callCount = 0;
    mockApiClient().listTemplates.mockImplementation(() => {
      callCount++;
      return Promise.resolve({
        data: [{ id: `test-${callCount}`, name: `Test ${callCount}`, provider: 'ansible', labels: {}, template_type: 'job', created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z' }],
        pagination: { total: 1, limit: 100, offset: 0, hasMore: false }
      });
    });

    const { result } = renderHook(() => useExtendedTemplates());

    await act(async () => {
      await new Promise(resolve => setTimeout(resolve, 0));
    });

    expect(callCount).toBe(1);
    expect(result.current.data).toHaveLength(1);

    await act(async () => {
      result.current.reload();
      await new Promise(resolve => setTimeout(resolve, 0));
    });

    expect(callCount).toBe(2);
    expect(result.current.data).toHaveLength(1);
  });

  it('should calculate summary correctly', async () => {
    const mockTemplates = [
      {
        id: 'job-1',
        name: 'Job Template 1',
        provider: 'ansible',
        labels: { template_type: 'job' },
        template_type: 'job',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z'
      },
      {
        id: 'job-2',
        name: 'Job Template 2',
        provider: 'terraform',
        labels: { template_type: 'job' },
        template_type: 'job',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z'
      },
      {
        id: 'config-1',
        name: 'Config Template',
        provider: 'terraform',
        labels: { template_type: 'config' },
        template_type: 'config',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z'
      }
    ];

    mockApiClient().listTemplates.mockResolvedValue({
      data: mockTemplates,
      pagination: { total: 3, limit: 100, offset: 0, hasMore: false }
    });

    const { result } = renderHook(() => useExtendedTemplates());

    await act(async () => {
      await new Promise(resolve => setTimeout(resolve, 0));
    });

    const summary = result.current.summary;
    expect(summary.total).toBe(3);
    expect(summary.by_type[TemplateType.JOB]).toBe(2);
    expect(summary.by_type[TemplateType.CONFIG]).toBe(1);
    expect(summary.by_type[TemplateType.COMPLIANCE]).toBe(0);
    expect(summary.active).toBe(3);
    expect(summary.providers).toBe(2); // ansible, terraform
  });
});
