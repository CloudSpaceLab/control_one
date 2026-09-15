import { describe, expect, it } from 'vitest';
import { CORRELATION_RULE_TEMPLATES, correlationRuleTemplate } from './correlationTemplates';

describe('correlation rule templates', () => {
  it('defines the four Phase 3A templates with valid threshold rule values', () => {
    expect(CORRELATION_RULE_TEMPLATES.map((template) => template.id)).toEqual([
      'ssh-brute-force',
      'windows-repeated-login-failures',
      'repeated-web-server-errors',
      'database-authentication-failures',
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
    ['windows-repeated-login-failures', 'windows.authentication_failure', 5, ['user_name', 'node_id'], 'auth_result', 'eq', 'failure'],
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
