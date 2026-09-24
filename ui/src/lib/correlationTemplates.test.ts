import { describe, expect, it } from 'vitest';
import { CORRELATION_RULE_TEMPLATES, correlationRuleTemplate } from './correlationTemplates';

describe('correlation rule templates', () => {
  it('defines the Phase 3A and 3B templates with valid threshold rule values', () => {
    expect(CORRELATION_RULE_TEMPLATES.map((template) => template.id)).toEqual([
      'ssh-brute-force',
      'repeated-authentication-failures',
      'repeated-web-server-errors',
      'database-authentication-failures',
      'web-request-flood',
      'web-path-scanner',
      'credential-stuffing',
      'port-scanning',
      'successful-login-after-failures',
      'suspicious-outbound-transfer',
    ]);

    for (const template of CORRELATION_RULE_TEMPLATES) {
      expect(template.name).not.toBe('');
      expect(template.eventCategory).toBe('security.event');
      expect(template.eventType).not.toBe('');
      expect(template.threshold).toBeGreaterThan(0);
      expect(template.windowSeconds).toBeGreaterThan(0);
      expect(template.groupBy.length).toBeGreaterThan(0);
      expect(template.suppressionSeconds).toBeGreaterThanOrEqual(0);
    }
  });

  it('configures the successful-login sequence', () => {
    expect(correlationRuleTemplate('successful-login-after-failures')).toMatchObject({
      eventType: 'authentication.success', sequenceEventType: 'authentication.failure', sequenceThreshold: 4,
      groupBy: ['src_ip', 'user_name', 'node_id'],
    });
  });

  it('configures outbound transfer as a byte aggregate', () => {
    expect(correlationRuleTemplate('suspicious-outbound-transfer')).toMatchObject({
      eventType: 'network.connection', aggregateField: 'bytes_out', aggregateThreshold: 104857600,
      conditions: [{ field: 'direction', operator: 'eq', value: 'outbound' }],
    });
  });

  it('configures web detections with OR condition groups', () => {
    expect(correlationRuleTemplate('web-request-flood')).toMatchObject({
      eventType: 'web.request', threshold: 1000,
      conditionGroups: [
        [{ field: 'dst_port', operator: 'eq', value: '80' }],
        [{ field: 'dst_port', operator: 'eq', value: '443' }],
      ],
    });
    expect(correlationRuleTemplate('web-path-scanner')?.conditionGroups).toHaveLength(3);
  });

  it.each([
    ['credential-stuffing', 'user_name'],
    ['port-scanning', 'dst_port'],
  ])('configures %s to count distinct %s values', (id, distinctField) => {
    expect(correlationRuleTemplate(id)).toMatchObject({ distinctField });
  });

  it('provides matching conditions for the SSH template', () => {
    expect(correlationRuleTemplate('ssh-brute-force')).toMatchObject({
      eventType: 'ssh.authentication_failure',
      threshold: 4,
      windowSeconds: 20,
      groupBy: ['src_ip', 'node_id'],
      conditions: [
        { field: 'dst_port', operator: 'eq', value: '22' },
        { field: 'protocol', operator: 'eq', value: 'tcp' },
        { field: 'auth_result', operator: 'eq', value: 'failure' },
      ],
    });
  });

  it.each([
    ['repeated-authentication-failures', 'authentication.failure', 5, ['user_name', 'node_id'], 'auth_result', 'eq', 'failure'],
    ['repeated-web-server-errors', 'web.request', 10, ['node_id'], 'status_code', 'gte', '500'],
    ['database-authentication-failures', 'database.authentication_failure', 5, ['user_name', 'node_id'], 'auth_result', 'eq', 'failure'],
  ])('configures %s with its intended event and condition', (id, eventType, threshold, groupBy, field, operator, value) => {
    expect(correlationRuleTemplate(id)).toMatchObject({
      eventType,
      threshold,
      groupBy,
      conditions: [{ field, operator, value }],
    });
  });
});
