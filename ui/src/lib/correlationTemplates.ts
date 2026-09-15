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
  conditionGroups: CorrelationCondition[][];
  distinctField: string;
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
    conditionGroups: [],
    distinctField: '',
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
    conditionGroups: [],
    distinctField: '',
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
    conditionGroups: [],
    distinctField: '',
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
    conditionGroups: [],
    distinctField: '',
  },
  {
    id: 'web-request-flood',
    name: 'Web request flood',
    description: 'Detect a high volume of HTTP or HTTPS requests from one source against one host.',
    eventCategory: 'security.event', eventType: 'web.request', threshold: 1000, windowSeconds: 60,
    groupBy: ['src_ip', 'node_id'], severity: 'high', suppressionSeconds: 300,
    conditions: [{ field: 'protocol', operator: 'eq', value: 'tcp' }],
    conditionGroups: [
      [{ field: 'dst_port', operator: 'eq', value: '80' }],
      [{ field: 'dst_port', operator: 'eq', value: '443' }],
    ],
    distinctField: '',
  },
  {
    id: 'web-path-scanner',
    name: 'Web path scanner',
    description: 'Detect repeated probes for sensitive web paths from one source against one host.',
    eventCategory: 'security.event', eventType: 'web.request', threshold: 10, windowSeconds: 60,
    groupBy: ['src_ip', 'node_id'], severity: 'high', suppressionSeconds: 300,
    conditions: [],
    conditionGroups: [
      [{ field: 'path', operator: 'contains', value: '/.env' }],
      [{ field: 'path', operator: 'contains', value: '/wp-admin' }],
      [{ field: 'path', operator: 'contains', value: '/phpmyadmin' }],
    ],
    distinctField: '',
  },
  {
    id: 'credential-stuffing',
    name: 'Credential stuffing',
    description: 'Detect failed authentication attempts against many accounts from one source.',
    eventCategory: 'security.event', eventType: 'authentication.failure', threshold: 10, windowSeconds: 60,
    groupBy: ['src_ip', 'node_id'], severity: 'critical', suppressionSeconds: 300,
    conditions: [{ field: 'auth_result', operator: 'eq', value: 'failure' }],
    conditionGroups: [],
    distinctField: 'user_name',
  },
  {
    id: 'port-scanning',
    name: 'Port scanning',
    description: 'Detect one source probing many destination ports on the same host.',
    eventCategory: 'security.event', eventType: 'network.connection', threshold: 20, windowSeconds: 60,
    groupBy: ['src_ip', 'node_id'], severity: 'high', suppressionSeconds: 300,
    conditions: [{ field: 'protocol', operator: 'eq', value: 'tcp' }],
    conditionGroups: [],
    distinctField: 'dst_port',
  },
];

export function correlationRuleTemplate(id: string): CorrelationRuleTemplate | undefined {
  return CORRELATION_RULE_TEMPLATES.find((template) => template.id === id);
}
