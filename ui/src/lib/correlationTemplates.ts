import type { CorrelationCondition } from './api';

export interface CorrelationRuleTemplate {
  id: string;
  name: string;
  description: string;
  eventCategory: string;
  eventType: string;
  threshold: number;
  windowSeconds: number;
  groupBy: string[];
  severity: string;
  suppressionSeconds: number;
  conditions: CorrelationCondition[];
}

export const CORRELATION_RULE_TEMPLATES: CorrelationRuleTemplate[] = [
  {
    id: 'ssh-brute-force',
    name: 'SSH brute-force detection',
    description: 'Detect repeated failed SSH authentication attempts from the same source against one host.',
    eventCategory: 'security.event',
    eventType: 'ssh.authentication_failure',
    threshold: 4,
    windowSeconds: 20,
    groupBy: ['src_ip', 'node_id'],
    severity: 'high',
    suppressionSeconds: 300,
    conditions: [
      { field: 'dst_port', operator: 'eq', value: '22' },
      { field: 'protocol', operator: 'eq', value: 'tcp' },
      { field: 'auth_result', operator: 'eq', value: 'failure' },
    ],
  },
  {
    id: 'windows-repeated-login-failures',
    name: 'Windows repeated login failures',
    description: 'Detect repeated failed Windows sign-ins for the same account on one host.',
    eventCategory: 'security.event',
    eventType: 'windows.authentication_failure',
    threshold: 5,
    windowSeconds: 60,
    groupBy: ['user_name', 'node_id'],
    severity: 'high',
    suppressionSeconds: 300,
    conditions: [
      { field: 'auth_result', operator: 'eq', value: 'failure' },
    ],
  },
  {
    id: 'repeated-web-server-errors',
    name: 'Repeated web-server errors',
    description: 'Detect a burst of HTTP server errors reported by the same monitored host.',
    eventCategory: 'security.event',
    eventType: 'web.request',
    threshold: 10,
    windowSeconds: 60,
    groupBy: ['node_id'],
    severity: 'medium',
    suppressionSeconds: 300,
    conditions: [
      { field: 'status_code', operator: 'gte', value: '500' },
    ],
  },
  {
    id: 'database-authentication-failures',
    name: 'Database authentication failures',
    description: 'Detect repeated failed database authentication attempts for an account on one host.',
    eventCategory: 'security.event',
    eventType: 'database.authentication_failure',
    threshold: 5,
    windowSeconds: 60,
    groupBy: ['user_name', 'node_id'],
    severity: 'high',
    suppressionSeconds: 300,
    conditions: [
      { field: 'auth_result', operator: 'eq', value: 'failure' },
    ],
  },
];

export function correlationRuleTemplate(id: string): CorrelationRuleTemplate | undefined {
  return CORRELATION_RULE_TEMPLATES.find((template) => template.id === id);
}
