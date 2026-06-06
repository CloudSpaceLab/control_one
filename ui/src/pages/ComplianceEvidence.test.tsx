import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { ComplianceEvidence } from './ComplianceEvidence';

const mocks = vi.hoisted(() => {
  const uploadComplianceEvidence = vi.fn();
  const listComplianceEvidence = vi.fn();
  const listComplianceReviews = vi.fn();
  const deleteComplianceEvidence = vi.fn();
  return {
    apiClient: {
      uploadComplianceEvidence,
      listComplianceEvidence,
      listComplianceReviews,
      deleteComplianceEvidence,
      buildEvidenceDownloadUrl: vi.fn(() => '#'),
    },
    uploadComplianceEvidence,
    listComplianceEvidence,
    listComplianceReviews,
    deleteComplianceEvidence,
  };
});

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

vi.mock('../hooks/useTenants', () => ({
  useTenants: () => ({
    data: [{ id: 'tenant-1', name: 'Bank Tenant', created_at: '2026-01-01T00:00:00Z' }],
    loading: false,
    error: null,
    reload: vi.fn(),
  }),
}));

describe('ComplianceEvidence', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listComplianceEvidence.mockResolvedValue({
      data: [],
      pagination: { total: 0, count: 0, limit: 100, offset: 0, nextOffset: null, prevOffset: null },
    });
    mocks.listComplianceReviews.mockResolvedValue({
      data: [],
      pagination: { total: 0, count: 0, limit: 100, offset: 0, nextOffset: null, prevOffset: null },
    });
    mocks.uploadComplianceEvidence.mockResolvedValue({});
  });

  it('keeps evidence upload disabled until required fields are present', async () => {
    render(<ComplianceEvidence />);

    const uploadButton = await screen.findByRole('button', { name: /upload evidence/i });
    expect(uploadButton).toBeDisabled();

    fireEvent.change(screen.getByPlaceholderText(/q1 security training/i), {
      target: { value: '  Access review proof  ' },
    });

    expect(uploadButton).toBeEnabled();
    fireEvent.click(uploadButton);

    await waitFor(() => expect(mocks.uploadComplianceEvidence).toHaveBeenCalledTimes(1));
    const formData = mocks.uploadComplianceEvidence.mock.calls[0][0] as FormData;
    expect(formData.get('title')).toBe('Access review proof');
    expect(document.body.textContent).not.toMatch(/\u00e2/);
  });
});
