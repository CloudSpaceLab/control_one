import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { AuditReport } from '../lib/api';
import { AuditReports } from './AuditReports';

const mocks = vi.hoisted(() => {
  const listAuditReports = vi.fn();
  const createAuditReport = vi.fn();
  const downloadAuditReport = vi.fn();
  const saveBlob = vi.fn();

  return {
    listAuditReports,
    createAuditReport,
    downloadAuditReport,
    saveBlob,
    apiClient: {
      listAuditReports,
      createAuditReport,
      downloadAuditReport,
    },
  };
});

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

vi.mock('../hooks/useTenants', () => ({
  useTenants: () => ({
    data: [
      { id: 'tenant-1', name: 'First Tenant', created_at: '2026-01-01T00:00:00Z' },
      { id: 'tenant-2', name: 'Active Bank', created_at: '2026-01-01T00:00:00Z' },
    ],
    loading: false,
    error: null,
    pagination: { total: 2, count: 2, limit: 10, offset: 0, nextOffset: null, prevOffset: null },
    reload: vi.fn(),
    refresh: vi.fn(),
  }),
}));

vi.mock('../providers/TenantProvider', () => ({
  useTenant: () => ({ currentTenantId: 'tenant-2' }),
}));

vi.mock('../lib/download', () => ({
  saveBlob: mocks.saveBlob,
}));

const readyReport: AuditReport = {
  id: 'report-1',
  tenant_id: 'tenant-2',
  framework: 'SOC2',
  period_start: '2026-01-01',
  period_end: '2026-03-31',
  status: 'ready',
  generated_at: '2026-04-01T00:00:00Z',
};

const failedReport: AuditReport = {
  ...readyReport,
  id: 'report-2',
  framework: 'PCI-DSS',
  status: 'failed',
};

function paginated(reports: AuditReport[]) {
  return {
    data: reports,
    pagination: { total: reports.length, count: reports.length, limit: 50, offset: 0, nextOffset: null, prevOffset: null },
  };
}

describe('AuditReports production hardening', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listAuditReports.mockResolvedValue(paginated([]));
    mocks.createAuditReport.mockResolvedValue(readyReport);
    mocks.downloadAuditReport.mockResolvedValue({
      blob: new Blob(['report'], { type: 'text/plain' }),
      filename: 'soc2.txt',
    });
  });

  it('loads report history for the active tenant instead of the first tenant', async () => {
    render(<AuditReports />);

    await waitFor(() => {
      expect(mocks.listAuditReports).toHaveBeenCalledWith({ tenantId: 'tenant-2', limit: 50 });
    });
    expect(screen.getByLabelText(/report tenant/i)).toHaveValue('tenant-2');
  });

  it('surfaces report history failures without rendering a false empty state', async () => {
    mocks.listAuditReports.mockRejectedValueOnce(new Error('report store offline'));

    render(<AuditReports />);

    expect(await screen.findAllByText('Report history unavailable')).toHaveLength(2);
    expect(screen.getByRole('alert')).toHaveTextContent('report store offline');
    expect(screen.queryByText('No generated reports')).not.toBeInTheDocument();
  });

  it('validates report period order before creating a report', async () => {
    const user = userEvent.setup();
    render(<AuditReports />);

    await screen.findByLabelText(/report tenant/i);
    await user.type(screen.getByLabelText(/period start/i), '2026-04-01');
    await user.type(screen.getByLabelText(/period end/i), '2026-03-31');
    await user.click(screen.getByRole('button', { name: /generate report/i }));

    expect(await screen.findByText('Period start must be on or before period end.')).toBeInTheDocument();
    expect(mocks.createAuditReport).not.toHaveBeenCalled();
  });

  it('keeps download failures visible and disables failed report downloads', async () => {
    const user = userEvent.setup();
    mocks.listAuditReports.mockResolvedValue(paginated([readyReport, failedReport]));
    mocks.downloadAuditReport.mockRejectedValueOnce(new Error('artifact missing'));

    render(<AuditReports />);

    const readyDownload = await screen.findByRole('button', { name: /download soc2 report/i });
    await user.click(readyDownload);

    expect(await screen.findByText('Report download failed')).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('artifact missing');
    expect(mocks.saveBlob).not.toHaveBeenCalled();

    const pciRow = screen.getAllByText('PCI-DSS')
      .map((element) => element.closest('tr'))
      .find((row): row is HTMLTableRowElement => row !== null);
    expect(pciRow).not.toBeNull();
    expect(within(pciRow as HTMLElement).getByRole('button', { name: /pci-dss report is not ready/i })).toBeDisabled();
  });
});
