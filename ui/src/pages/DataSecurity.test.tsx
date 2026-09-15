import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { DataSecurity } from './DataSecurity';
import type { DataClassificationRule, PIIFinding } from '../lib/api';

const mocks = vi.hoisted(() => {
  const listPIIFindings = vi.fn();
  const resolvePIIFinding = vi.fn();
  const listColumnClassifications = vi.fn();
  const listDLPRules = vi.fn();
  const seedDLPRules = vi.fn();
  const createDLPRule = vi.fn();
  const deleteDLPRule = vi.fn();

  return {
    apiClient: {
      listPIIFindings,
      resolvePIIFinding,
      listColumnClassifications,
      listDLPRules,
      seedDLPRules,
      createDLPRule,
      deleteDLPRule,
    },
    listPIIFindings,
    resolvePIIFinding,
    listColumnClassifications,
    listDLPRules,
    seedDLPRules,
    createDLPRule,
    deleteDLPRule,
  };
});

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

vi.mock('../hooks/useTenants', () => ({
  useTenants: () => ({
    data: [{ id: 'tenant-1', name: 'Bank Tenant', created_at: '2026-06-01T00:00:00Z' }],
    loading: false,
    error: null,
    reload: vi.fn(),
  }),
}));

const pagination = { total: 0, count: 0, limit: 100, offset: 0, nextOffset: null, prevOffset: null };

const finding: PIIFinding = {
  id: 'finding-1',
  tenant_id: 'tenant-1',
  severity: 'critical',
  details: 'Customer email leaked',
  created_at: '2026-06-08T00:00:00Z',
};

const dlpRule: DataClassificationRule = {
  id: 'rule-1',
  tenant_id: 'tenant-1',
  name: 'Email address',
  pii_type: 'email',
  regex: '\\b[A-Za-z0-9._%+\\-]+@[A-Za-z0-9.\\-]+\\.[A-Za-z]{2,}\\b',
  severity: 'high',
  enabled: true,
  created_at: '2026-06-08T00:00:00Z',
};

describe('DataSecurity', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listPIIFindings.mockResolvedValue({ data: [], pagination });
    mocks.resolvePIIFinding.mockResolvedValue(undefined);
    mocks.listColumnClassifications.mockResolvedValue({ data: [], pagination });
    mocks.listDLPRules.mockResolvedValue({ data: [dlpRule], pagination: { ...pagination, total: 1, count: 1 } });
    mocks.seedDLPRules.mockResolvedValue({ seeded: 3 });
    mocks.createDLPRule.mockResolvedValue(dlpRule);
    mocks.deleteDLPRule.mockResolvedValue(undefined);
  });

  it('surfaces findings load failures instead of showing a false empty state', async () => {
    mocks.listPIIFindings.mockRejectedValueOnce(new Error('PII evidence store unavailable'));

    render(<DataSecurity />);

    expect(await screen.findByText('PII findings unavailable')).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('PII evidence store unavailable');
    expect(screen.queryByText('No findings')).not.toBeInTheDocument();
  });

  it('surfaces column scan load failures instead of showing a false empty state', async () => {
    const user = userEvent.setup();
    mocks.listColumnClassifications.mockRejectedValueOnce(new Error('column projection unavailable'));

    render(<DataSecurity />);

    await user.click(await screen.findByRole('tab', { name: 'Columns' }));

    expect(await screen.findByText('Column scans unavailable')).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('column projection unavailable');
    expect(screen.queryByText('No column scans')).not.toBeInTheDocument();
  });

  it('surfaces rule load failures instead of showing a false empty state', async () => {
    const user = userEvent.setup();
    mocks.listDLPRules.mockRejectedValueOnce(new Error('rule catalog unavailable'));

    render(<DataSecurity />);

    await user.click(await screen.findByRole('tab', { name: 'Rules' }));

    expect(await screen.findByText('Classification rules unavailable')).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('rule catalog unavailable');
    expect(screen.queryByText('No classification rules')).not.toBeInTheDocument();
  });

  it('keeps failed finding resolves visible in the confirmation modal', async () => {
    const user = userEvent.setup();
    mocks.listPIIFindings.mockResolvedValueOnce({ data: [finding], pagination: { ...pagination, total: 1, count: 1 } });
    mocks.resolvePIIFinding.mockRejectedValueOnce(new Error('resolver unavailable'));

    render(<DataSecurity />);

    await user.click(await screen.findByRole('button', { name: /resolve pii finding customer email leaked/i }));
    expect(screen.getByRole('dialog', { name: /resolve finding/i })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: /^resolve$/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent('PII finding resolve failed: resolver unavailable');
    expect(screen.getByRole('dialog', { name: /resolve finding/i })).toBeInTheDocument();
  });

  it('keeps failed DLP rule deletes visible in the confirmation modal', async () => {
    const user = userEvent.setup();
    mocks.deleteDLPRule.mockRejectedValueOnce(new Error('rule is attached to an active scan'));

    render(<DataSecurity />);

    await user.click(await screen.findByRole('tab', { name: 'Rules' }));
    await user.click(await screen.findByRole('button', { name: /delete classification rule email address/i }));
    expect(screen.getByRole('dialog', { name: /delete rule/i })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: /^delete$/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'DLP rule delete failed: rule is attached to an active scan',
    );
    expect(screen.getByRole('dialog', { name: /delete rule/i })).toBeInTheDocument();
    expect(mocks.deleteDLPRule).toHaveBeenCalledWith('rule-1');
  });

  it('creates DLP rules with the selected tenant and preserves the form on validation failures', async () => {
    const user = userEvent.setup();

    render(<DataSecurity />);

    await user.click(await screen.findByRole('tab', { name: 'Rules' }));
    await user.click(await screen.findByRole('button', { name: /^add rule$/i }));
    expect(screen.getByRole('alert')).toHaveTextContent('Name, PII type, and regex are required.');
    expect(mocks.createDLPRule).not.toHaveBeenCalled();

    await user.type(screen.getByLabelText(/^name$/i), 'IBAN');
    await user.type(screen.getByLabelText(/pii type/i), 'iban');
    await user.type(screen.getByLabelText(/regex pattern/i), '^IBAN');
    await user.click(screen.getByRole('button', { name: /^add rule$/i }));

    await waitFor(() => expect(mocks.createDLPRule).toHaveBeenCalledTimes(1));
    expect(mocks.createDLPRule).toHaveBeenCalledWith(
      expect.objectContaining({
        tenant_id: 'tenant-1',
        name: 'IBAN',
        pii_type: 'iban',
        regex: '^IBAN',
        severity: 'medium',
      }),
    );
  });
});
