import { describe, expect, it } from 'vitest';
import type { Alert } from '../lib/api';
import { alertContextPills, alertDispositionPill } from './Alerts';

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
