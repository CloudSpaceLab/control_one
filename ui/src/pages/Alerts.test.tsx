import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Alert, CorrelationRule } from '../lib/api';
import { Alerts, alertAccessReviewRoute, alertCategory, alertContextPills, alertDispositionPill, alertInvestigationRoute, alertResolutionFacts, alertScope, eventEvidenceFacts, sortContributingEvents, withAlertReturnContext } from './Alerts';

it('sorts contributing evidence newest first and keeps undated events last', () => {
  const events = sortContributingEvents([
    { timestamp: '2026-09-25T09:34:21Z', event_id: 'old' },
    { timestamp: '2026-09-25T09:34:26Z', event_id: 'new' },
    { event_id: 'undated' },
  ]);
  expect(events.map((event) => event.event_id)).toEqual(['new', 'old', 'undated']);
});

it('describes recorded event resources and marks missing evidence unavailable', () => {
  const facts = Object.fromEntries(eventEvidenceFacts({ tenant_id: 'tenant-1', node_id: 'node-1', dst_ip: '192.0.2.1', dst_port: 443, protocol: 'tcp', path: '/etc/example', severity: 'warning', source_os: 'windows', source_channel: 'Microsoft-Windows-Biometrics/Operational', source_event_id: '1005', event_id: 'evt-1', sensor_name: 'ELAN WBF Fingerprint Sensor' }));
  expect(facts.Tenant).toBe('tenant-1');
  expect(facts.Host).toBe('node-1');
  expect(facts['Observed resource']).toContain('Destination: 192.0.2.1 port 443 (tcp)');
  expect(facts['Observed resource']).toContain('Path: /etc/example');
  expect(facts['Observed resource']).toContain('Sensor: ELAN WBF Fingerprint Sensor');
  expect(facts['Source OS']).toBe('windows');
  expect(facts['Source channel']).toContain('Biometrics');
  expect(facts['Source event ID']).toBe('1005');
  expect(facts['Event reference']).toBe('evt-1');
  expect(Object.fromEntries(eventEvidenceFacts({}))['Observed resource']).toContain('not recorded');
});

it('classifies Windows authentication failures as credential alerts', () => {
  const alert = {
    ...({} as Alert),
    source: 'correlation',
    title: 'Phase 3 demo - Windows repeated login failures',
    summary: 'correlation rule fired',
    context: {
      event_type: 'security.event',
      contributing_events: [{
        event_type: 'windows.authentication_failure',
        message: 'The Windows Biometric Service could not determine the identity of the sample',
      }],
    },
  } as Alert;

  expect(alertCategory(alert)).toBe('credential');
});

it('shows the affected node name and id when only event evidence identifies it', () => {
  const alert = {
    ...({} as Alert),
    context: {
      contributing_events: [{ hostname: 'Cloudspace', node_id: 'node-1' }],
    },
  } as Alert;

  expect(alertScope(alert)).toBe('node Cloudspace (node-1)');
});

const mocks = vi.hoisted(() => {
  const listAlerts = vi.fn();
  const getAlert = vi.fn();
  const ackAlert = vi.fn();
  const updateAlertDisposition = vi.fn();
  const listCorrelationRules = vi.fn();
  const createCorrelationRule = vi.fn();
  const deleteCorrelationRule = vi.fn();
  const createAlertSOCCase = vi.fn();
	const attachAlertSOCCase = vi.fn();
	const listSOCCases = vi.fn();
  const updateAlertWorkflow = vi.fn();
  return {
    apiClient: {
      getNode: vi.fn().mockResolvedValue({ id: 'node-1', tenant_id: 'tenant-1', hostname: 'demo-system.local' }),
      listAlerts,
      getAlert,
      ackAlert,
      updateAlertDisposition,
      listCorrelationRules,
      createCorrelationRule,
      deleteCorrelationRule,
      createAlertSOCCase,
		attachAlertSOCCase,
		listSOCCases,
      updateAlertWorkflow,
    },
    listAlerts,
    getAlert,
    ackAlert,
    updateAlertDisposition,
    listCorrelationRules,
    createCorrelationRule,
    deleteCorrelationRule,
    createAlertSOCCase,
	attachAlertSOCCase,
	listSOCCases,
    updateAlertWorkflow,
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
    matched_event_count: 4,
    window_s: 20,
    notification_state: 'suppressed',
    contributing_events: [{ timestamp: '2026-06-08T00:00:00Z', event_type: 'ssh.authentication_failure', src_ip: '203.0.113.9', user_name: 'root' }],
  },
};

const ruleRow = {
  id: 'rule-1',
  tenant_id: 'tenant-1',
  name: 'SSH brute force',
  severity: 'critical',
  enabled: true,
  conditions: {},
  created_at: '2026-06-08T00:00:00Z',
  updated_at: '2026-06-08T00:00:00Z',
} as unknown as CorrelationRule;

function renderAlerts(initialEntry = '/alerts') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
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
    mocks.apiClient.getNode.mockReset().mockResolvedValue({ id: 'node-1', tenant_id: 'tenant-1', hostname: 'demo-system.local' });
    mocks.currentTenantId = 'tenant-1';
    mocks.listAlerts.mockResolvedValue(paginated([alertRow]));
    mocks.getAlert.mockResolvedValue(alertRow);
    mocks.ackAlert.mockResolvedValue(undefined);
    mocks.updateAlertDisposition.mockResolvedValue({ ...alertRow, state: 'resolved' });
    mocks.listCorrelationRules.mockResolvedValue(paginated([ruleRow]));
    mocks.createCorrelationRule.mockResolvedValue(ruleRow);
    mocks.deleteCorrelationRule.mockResolvedValue(undefined);
    mocks.createAlertSOCCase.mockResolvedValue({ case_id: 'case-1' });
	mocks.attachAlertSOCCase.mockResolvedValue({ case_id: 'case-existing' });
	mocks.listSOCCases.mockResolvedValue({ data: [], pagination: { total: 0, limit: 50, offset: 0 } });
    mocks.updateAlertWorkflow.mockResolvedValue(alertRow);
  });

  it.each([
    ['exfiltration', ['/security/network?tab=ip-behavior', '/control-room/exposure']],
    ['credential', ['/access']],
    ['exploit', ['/security/webservers']],
    ['scanner', ['/security/webservers']],
    ['malware', ['/nodes', '/audit']],
    ['exposure', ['/control-room/exposure', '/security/network?tab=firewall']],
    ['secret', ['/data-security', '/secrets']],
    ['privilege', ['/access', '/sessions']],
    ['patch', ['/infrastructure/patch']],
    ['compliance', ['/compliance']],
    ['generic', ['/control-room', '/audit']],
  ])('preserves return context on every %s modal destination', async (category, destinations) => {
    const id = 'alert /?&=+#é';
    const row = { ...alertRow, id, title: category, summary: '', context: { src_ip: '203.0.113.9' } };
    mocks.getAlert.mockResolvedValue(row);
    mocks.listSOCCases.mockResolvedValue({ data: [{ case_id: 'case /&1', status: 'open', evidence: { alert_id: id } }] });
    renderAlerts(`/alerts?alert_id=${encodeURIComponent(id)}`);
    const dialog = await screen.findByRole('dialog', { name: /resolve alert with evidence/i });
    await within(dialog).findByRole('link', { name: 'Open SOC case' });
    const urls = within(dialog).getAllByRole('link').map((link) => new URL(link.getAttribute('href')!, 'http://local'));
    for (const url of urls) {
      expect(url.searchParams.getAll('fromAlert')).toEqual([id]);
      expect(url.search).toContain(`fromAlert=${encodeURIComponent(id)}`);
    }
    for (const destination of [...destinations, '/security/network?tab=blocks', '/investigate/ip/203.0.113.9?audit=1', '/cases?case_id=case%20%2F%261']) {
      const expected = new URL(destination, 'http://local');
      expect(urls.some((url) => url.pathname === expected.pathname && [...expected.searchParams].every(([key, value]) => url.searchParams.get(key) === value))).toBe(true);
    }
    expect(urls.some((url) => url.pathname === '/audit' && url.searchParams.get('q') === id)).toBe(true);
    if (category === 'credential' || category === 'privilege') {
      const access = urls.find((url) => url.pathname === '/access')!;
      expect(access.searchParams.get('alert_id')).toBe(id);
      expect(access.searchParams.get('from')).toBe(row.opened_at);
      expect(access.searchParams.get('to')).toBe(row.opened_at);
    }
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
    await user.selectOptions(within(dialog).getByLabelText(/disposition/i), 'false_positive');
    await user.type(within(dialog).getByLabelText(/evidence reason/i), 'Confirmed scanner noise, no blast radius.');
    await user.click(within(dialog).getByRole('button', { name: /record disposition/i }));

    expect(await within(dialog).findByRole('alert')).toHaveTextContent(
      'Alert disposition failed: evidence gate denied',
    );
    expect(screen.getByRole('dialog', { name: /resolve alert with evidence/i })).toBeInTheDocument();
  });

  it('requires reason and evidence before recording a true_positive or resolved disposition', async () => {
    const user = userEvent.setup();

    renderAlerts();

    await user.click(await screen.findByRole('button', { name: /review alert critical ssh burst/i }));
    const dialog = screen.getByRole('dialog', { name: /resolve alert with evidence/i });
    const confirm = within(dialog).getByRole('button', { name: /record disposition/i });

    expect(within(dialog).getByLabelText(/disposition/i)).toHaveValue('true_positive');
    expect(confirm).toBeDisabled();

    await user.selectOptions(within(dialog).getByLabelText(/disposition/i), 'resolved');
    await user.type(within(dialog).getByLabelText(/evidence reason/i), 'Would that the evidence gate were satisfied.');
    expect(confirm).toBeDisabled();

    await user.selectOptions(within(dialog).getByLabelText(/disposition/i), 'false_positive');
    expect(confirm).toBeEnabled();
  });

  it('lets an existing SOC case provide evidence for a real alert after reopening', async () => {
    const user = userEvent.setup();
    mocks.listSOCCases.mockResolvedValue({
      data: [{ case_id: 'case-1', status: 'open', evidence: { alert_id: 'alert-1' } }],
      pagination: { total: 1, limit: 50, offset: 0 },
    });

    renderAlerts('/alerts?alert_id=alert-1');
    const dialog = await screen.findByRole('dialog', { name: /resolve alert with evidence/i });
    expect(mocks.getAlert).toHaveBeenCalledWith('alert-1');
    expect(await within(dialog).findByText(/soc case attached as resolution evidence/i)).toBeInTheDocument();
    await user.type(within(dialog).getByLabelText(/evidence reason/i), 'Investigation continues in the linked SOC case.');
    expect(within(dialog).getByRole('button', { name: /record disposition/i })).toBeEnabled();
  });

  it('shows the correlation activity summary and contributing events', async () => {
    const user = userEvent.setup();
    renderAlerts();
    expect(await screen.findByText('Activity: 4 in 20s')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: /review alert critical ssh burst/i }));
    expect(screen.getByText('Contributing events (1)')).toBeInTheDocument();
    expect(screen.getByText(/ssh\.authentication_failure/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /create soc case with evidence/i })).toBeInTheDocument();
  });

	it('requires confirmation before creating a SOC case', async () => {
		const user = userEvent.setup();
		renderAlerts();
		await user.click(await screen.findByRole('button', { name: /review alert critical ssh burst/i }));
		await user.click(screen.getByRole('button', { name: /create soc case with evidence/i }));
		expect(mocks.createAlertSOCCase).not.toHaveBeenCalled();
		expect(screen.getByText(/create a new soc case/i)).toBeInTheDocument();
		await user.click(screen.getByRole('button', { name: /confirm creation/i }));
		expect(mocks.createAlertSOCCase).toHaveBeenCalledWith('alert-1');
	});

  it('saves analyst assignment and notes from alert review', async () => {
    const user = userEvent.setup();
    renderAlerts();
    await user.click(await screen.findByRole('button', { name: /review alert critical ssh burst/i }));
    await user.type(screen.getByLabelText(/assigned analyst/i), 'analyst@example.com');
    await user.type(screen.getByLabelText(/analyst note/i), 'Validated the four failed SSH events.');
    await user.click(screen.getByRole('button', { name: /save assignment and note/i }));
    expect(mocks.updateAlertWorkflow).toHaveBeenCalledWith('alert-1', {
      assigned_to: 'analyst@example.com',
      note: 'Validated the four failed SSH events.',
    });
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

describe('alertResolutionFacts', () => {
  it('shows correlation occurrence and notification timing', () => {
    const alert: Alert = {
      id: 'alert-phase5', tenant_id: 'tenant-1', source: 'correlation', severity: 'high',
      title: 'SSH brute force', state: 'open', opened_at: '2026-09-15T10:00:00Z',
      context: { occurrence_count: 8, first_seen_at: '2026-09-15T10:00:00Z', last_seen_at: '2026-09-15T10:01:00Z', last_notification_at: '2026-09-15T10:00:00Z' },
    };
    const facts = alertResolutionFacts(alert, 'credential', 'node-1', '203.0.113.25');
    expect(facts.map((fact) => fact.label)).toEqual(expect.arrayContaining(['Occurrences', 'First seen', 'Last seen', 'Last notification']));
    expect(facts.find((fact) => fact.label === 'Occurrences')?.value).toBe('8');
  });
});

describe('contextual alert review routes', () => {
  it.each(['/nodes', '/security/network?tab=blocks', '/audit?q=a%26b&tag=one&tag=two&fromAlert=old&fromAlert=older#events'])('adds return context idempotently to %s', (destination) => {
    const id = 'alert /?&=+#é';
    const result = withAlertReturnContext(withAlertReturnContext(destination, id), id);
    const url = new URL(result, 'http://local');
    const original = new URL(destination, 'http://local');
    expect(url.searchParams.getAll('fromAlert')).toEqual([id]);
    expect(result).toContain(`fromAlert=${encodeURIComponent(id)}`);
    original.searchParams.delete('fromAlert');
    url.searchParams.delete('fromAlert');
    expect(url.href).toBe(original.href);
  });

  it('resolves the recorded node to its system name in the summary card', async () => {
    mocks.listAlerts.mockResolvedValue({ data: [{ ...alertRow, node_id: 'node-1' }], total: 1 });
    const user = userEvent.setup();
    renderAlerts();
    await user.click(await screen.findByRole('button', { name: /review alert critical ssh burst/i }));
    expect(await screen.findByText('demo-system.local')).toBeInTheDocument();
    expect(screen.getByText('Systems in evidence')).toBeInTheDocument();
  });

  it('opens resolved alert evidence without changing its disposition', async () => {
    mocks.listAlerts.mockResolvedValue(paginated([{ ...alertRow, state: 'resolved' }]));
    const user = userEvent.setup();
    renderAlerts();
    await user.click(await screen.findByRole('button', { name: /review alert critical ssh burst/i }));
    expect(screen.getByText('Contributing events (1)')).toBeInTheDocument();
    expect(mocks.updateAlertDisposition).not.toHaveBeenCalled();
    expect(mocks.ackAlert).not.toHaveBeenCalled();
  });

  it('resolves distinct contributing systems even for tenant-wide alerts', async () => {
    mocks.apiClient.getNode.mockImplementation(async (id: string) => ({ id, tenant_id: 'tenant-1', hostname: `${id}.local` }));
    mocks.listAlerts.mockResolvedValue({ data: [{ ...alertRow, context: { contributing_events: [{ node_id: 'alpha' }, { node_id: 'beta' }, { node_id: 'alpha' }] } }], total: 1 });
    const user = userEvent.setup();
    renderAlerts();
    await user.click(await screen.findByRole('button', { name: /review alert critical ssh burst/i }));
    expect(await screen.findByText('alpha.local, beta.local')).toBeInTheDocument();
  });

  it('keeps the node ID when its name lookup fails', async () => {
    mocks.apiClient.getNode.mockRejectedValue(new Error('Unavailable'));
    mocks.listAlerts.mockResolvedValue({ data: [{ ...alertRow, node_id: 'missing-node' }], total: 1 });
    const user = userEvent.setup();
    renderAlerts();
    await user.click(await screen.findByRole('button', { name: /review alert critical ssh burst/i }));
    expect(await screen.findByText('missing-node (name unavailable)')).toBeInTheDocument();
  });
	it('opens the exact IP lifecycle and carries host, user, and time into access review', () => {
		const alert: Alert = {
			id: 'alert-1', tenant_id: 'tenant-1', node_id: 'node-1', source: 'correlation', severity: 'high',
			title: 'SSH brute force', state: 'open', opened_at: '2026-09-16T10:00:00Z',
			context: { src_ip: '203.0.113.25', user_name: 'root', first_seen_at: '2026-09-16T10:00:01Z', last_seen_at: '2026-09-16T10:00:20Z' },
		};
		expect(alertInvestigationRoute(alert)).toBe('/investigate/ip/203.0.113.25?audit=1&fromAlert=alert-1');
		const access = new URL(`http://local${alertAccessReviewRoute(alert)}`);
		expect(access.pathname).toBe('/access');
		expect(Object.fromEntries(access.searchParams)).toEqual({
			node_id: 'node-1', user: 'root', from: '2026-09-16T10:00:01Z', to: '2026-09-16T10:00:20Z', alert_id: 'alert-1', fromAlert: 'alert-1',
		});
	});

	it('falls back to the alert lifecycle when no stronger entity is present', () => {
		const alert: Alert = { id: 'alert-2', tenant_id: 'tenant-1', source: 'correlation', severity: 'medium', title: 'Signal', state: 'open', opened_at: '2026-09-16T10:00:00Z', context: {} };
		expect(alertInvestigationRoute(alert)).toBe('/investigate/alert/alert-2?fromAlert=alert-2');
	});
});
