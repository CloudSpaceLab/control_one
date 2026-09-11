import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Alert, CorrelationRule } from '../lib/api';
import { Alerts, alertContextPills, alertDispositionPill } from './Alerts';

const mocks = vi.hoisted(() => {
  const listAlerts = vi.fn();
  const ackAlert = vi.fn();
  const updateAlertDisposition = vi.fn();
  const listCorrelationRules = vi.fn();
  const createCorrelationRule = vi.fn();
  const deleteCorrelationRule = vi.fn();
  return {
    apiClient: {
      listAlerts,
      ackAlert,
      updateAlertDisposition,
      listCorrelationRules,
      createCorrelationRule,
      deleteCorrelationRule,
    },
    listAlerts,
    ackAlert,
    updateAlertDisposition,
    listCorrelationRules,
    createCorrelationRule,
    deleteCorrelationRule,
    currentTenantId: 'tenant-1',
    setCurrentTenantId: vi.fn(),
  };
});

vi.mock('../components/kit', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../components/kit')>();
  return {
    ...actual,
    Chart: ({ ariaLabel }: { ariaLabel?: string }) => (
      <div role="img" aria-label={ariaLabel ?? 'chart'} />
    ),
  };
});

vi.mock('../hooks/useApiClient', () => ({
  useApiClient: () => mocks.apiClient,
}));

vi.mock('../hooks/useEventStream', () => ({
  useEventStream: vi.fn(),
}));

vi.mock('../providers/TenantProvider', () => ({
  useTenant: () => ({
    tenants: [{ id: 'tenant-1', name: 'Bank Tenant', created_at: '2026-01-01T00:00:00Z' }],
    currentTenantId: mocks.currentTenantId,
    setCurrentTenantId: mocks.setCurrentTenantId,
  }),
}));

const alertRow: Alert = {
  id: 'alert-1',
  tenant_id: 'tenant-1',
  source: 'correlation',
  severity: 'critical',
  title: 'Critical SSH burst',
  summary: 'Multiple failed SSH attempts from 203.0.113.9',
  state: 'open',
  opened_at: '2026-06-08T00:00:00Z',
  context: {
    source_ip: '203.0.113.9',
    event_type: 'auth failure',
  },
};

const ruleRow: CorrelationRule = {
  id: 'rule-1',
  tenant_id: 'tenant-1',
  name: 'SSH brute force',
  severity: 'critical',
  enabled: true,
  conditions: {},
  created_at: '2026-06-08T00:00:00Z',
  updated_at: '2026-06-08T00:00:00Z',
};

function renderAlerts() {
  return render(
    <MemoryRouter>
      <Alerts />
    </MemoryRouter>,
  );
}

function paginated<T>(data: T[]) {
  return {
    data,
    pagination: { total: data.length, count: data.length, limit: 100, offset: 0, nextOffset: null, prevOffset: null },
  };
}

describe('Alerts page failure states', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.currentTenantId = 'tenant-1';
    mocks.listAlerts.mockResolvedValue(paginated([alertRow]));
    mocks.ackAlert.mockResolvedValue(undefined);
    mocks.updateAlertDisposition.mockResolvedValue({ ...alertRow, state: 'resolved' });
    mocks.listCorrelationRules.mockResolvedValue(paginated([ruleRow]));
    mocks.createCorrelationRule.mockResolvedValue(ruleRow);
    mocks.deleteCorrelationRule.mockResolvedValue(undefined);
  });

  it('does not show all-clear or empty inbox copy when alerts fail to load', async () => {
    mocks.listAlerts.mockRejectedValueOnce(new Error('alerts store unavailable'));

    renderAlerts();

    expect(await screen.findByRole('alert')).toHaveTextContent('alerts store unavailable');
    expect(screen.getByText('Alerts could not be loaded')).toBeInTheDocument();
    expect(screen.queryByText('All clear')).not.toBeInTheDocument();
    expect(screen.queryByText('No alerts')).not.toBeInTheDocument();
  });

  it('keeps failed acknowledgements visible and names the row action', async () => {
    const user = userEvent.setup();
    mocks.ackAlert.mockRejectedValueOnce(new Error('ack denied'));

    renderAlerts();

    await user.click(await screen.findByRole('button', { name: /acknowledge alert critical ssh burst/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Ack failed for Critical SSH burst: ack denied',
    );
    expect(screen.getByText('Critical SSH burst')).toBeInTheDocument();
  });

  it('keeps failed alert dispositions visible in the resolution modal', async () => {
    const user = userEvent.setup();
    mocks.updateAlertDisposition.mockRejectedValueOnce(new Error('evidence gate denied'));

    renderAlerts();

    await user.click(await screen.findByRole('button', { name: /review alert critical ssh burst/i }));
    const dialog = screen.getByRole('dialog', { name: /resolve alert with evidence/i });
    await user.type(within(dialog).getByLabelText(/evidence reason/i), 'Blocked the source and verified no new attempts.');
    await user.click(within(dialog).getByRole('button', { name: /record disposition/i }));

    expect(await within(dialog).findByRole('alert')).toHaveTextContent(
      'Alert disposition failed: evidence gate denied',
    );
    expect(screen.getByRole('dialog', { name: /resolve alert with evidence/i })).toBeInTheDocument();
  });

  it('does not show a false empty state when correlation rules fail to load', async () => {
    const user = userEvent.setup();
    mocks.listCorrelationRules.mockRejectedValueOnce(new Error('rules unavailable'));

    renderAlerts();

    await user.click(screen.getByRole('button', { name: /correlation rules/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent('rules unavailable');
    expect(screen.getByText('Correlation rules could not be loaded')).toBeInTheDocument();
    expect(screen.queryByText('No correlation rules')).not.toBeInTheDocument();
  });

  it('keeps failed correlation rule creates visible in the create form', async () => {
    const user = userEvent.setup();
    mocks.listCorrelationRules.mockResolvedValueOnce(paginated([]));
    mocks.createCorrelationRule.mockRejectedValueOnce(new Error('duplicate rule'));

    renderAlerts();

    await user.click(screen.getByRole('button', { name: /correlation rules/i }));
    await user.click(await screen.findByRole('button', { name: /new rule/i }));
    await user.type(screen.getByLabelText(/name/i), 'SSH brute force');
    await user.click(screen.getByRole('button', { name: /^create$/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Correlation rule create failed: duplicate rule',
    );
    expect(screen.getByLabelText(/name/i)).toHaveValue('SSH brute force');
  });

  it('keeps failed correlation rule deletes visible in the confirmation modal', async () => {
    const user = userEvent.setup();
    mocks.deleteCorrelationRule.mockRejectedValueOnce(new Error('rule still in use'));

    renderAlerts();

    await user.click(screen.getByRole('button', { name: /correlation rules/i }));
    await user.click(await screen.findByRole('button', { name: /delete correlation rule ssh brute force/i }));
    const dialog = screen.getByRole('dialog', { name: /delete correlation rule/i });
    expect(dialog).toHaveTextContent('SSH brute force');
    await user.click(within(dialog).getByRole('button', { name: /^delete$/i }));

    expect(await within(dialog).findByRole('alert')).toHaveTextContent(
      'Correlation rule delete failed: rule still in use',
    );
    expect(screen.getByRole('dialog', { name: /delete correlation rule/i })).toBeInTheDocument();
  });
});

describe('alertContextPills', () => {
  it('surfaces app, parser, log, server group, and origin context', () => {
    const alert: Alert = {
      id: 'alert-1',
      tenant_id: 'tenant-1',
      source: 'correlation',
      severity: 'high',
      title: 'IP behavior finding',
      state: 'open',
      opened_at: '2026-05-18T10:00:00Z',
      context: {
        event_type: 'anomaly.ip_behavior',
        app: 'Core Banking API',
        parser_profile: 'temenos-t24',
        source_file: '/opt/temenos/logs/access.log',
        server_group: 'Core Banking',
        country_code: 'NG',
        asn: 'AS12345',
      },
    };

    expect(alertContextPills(alert).map((pill) => `${pill.label}:${pill.value}`)).toEqual([
      'Signal:anomaly.ip_behavior',
      'App:Core Banking API',
      'Parser:temenos-t24',
      'Log:access.log',
      'Group:Core Banking',
      'Origin:NG / ASN AS12345',
    ]);
  });

  it('surfaces disposition decisions separately from state', () => {
    const alert: Alert = {
      id: 'alert-2',
      tenant_id: 'tenant-1',
      source: 'correlation',
      severity: 'critical',
      title: 'Emergency lockdown exception',
      state: 'resolved',
      opened_at: '2026-05-18T10:00:00Z',
      context: {},
      disposition: {
        value: 'accepted_risk',
        reason: 'business owner approved a time-boxed exception',
      },
    };

    expect(alertDispositionPill(alert)).toEqual({
      label: 'Disposition',
      value: 'Accepted risk',
      tone: 'warning',
    });
  });
});
