import { Alert, Incident } from '../lib/api';

interface TopRule {
  name: string;
  triggerCount: number;
  lastTriggered: string;
}

interface SecurityDashboardData {
  activeAlerts: number;
  openIncidents: number;
  complianceScore: number;
  monitoredNodes: number;
  recentAlerts: Alert[];
  recentIncidents: Incident[];
  topRules: TopRule[];
  loading: boolean;
  error: string | null;
}

export function useSecurityDashboard(): SecurityDashboardData {
  const recentAlerts: Alert[] = [
    {
      id: 'alert-001',
      severity: 'critical',
      ruleName: 'Unauthorized Root Access',
      source: 'node-prod-web-01',
      message: 'Root login detected from unknown IP 203.0.113.42',
      status: 'open',
      tenantId: 'tenant-001',
      createdAt: new Date(Date.now() - 15 * 60000).toISOString(),
    },
    {
      id: 'alert-009',
      severity: 'critical',
      ruleName: 'Data Exfiltration Suspected',
      source: 'node-prod-db-01',
      message: 'Large database dump initiated by non-admin user',
      status: 'open',
      tenantId: 'tenant-002',
      createdAt: new Date(Date.now() - 10 * 60000).toISOString(),
    },
    {
      id: 'alert-002',
      severity: 'high',
      ruleName: 'Excessive Failed Logins',
      source: 'node-prod-api-02',
      message: '47 failed SSH login attempts in the last 10 minutes',
      status: 'acknowledged',
      tenantId: 'tenant-001',
      createdAt: new Date(Date.now() - 32 * 60000).toISOString(),
    },
    {
      id: 'alert-004',
      severity: 'medium',
      ruleName: 'Firewall Rule Modified',
      source: 'node-staging-01',
      message: 'Outbound rule added allowing traffic on port 4444',
      status: 'open',
      tenantId: 'tenant-001',
      createdAt: new Date(Date.now() - 2 * 3600000).toISOString(),
    },
    {
      id: 'alert-006',
      severity: 'high',
      ruleName: 'Privilege Escalation Attempt',
      source: 'node-prod-api-01',
      message: 'User "deploy" attempted sudo without authorization',
      status: 'acknowledged',
      tenantId: 'tenant-002',
      createdAt: new Date(Date.now() - 3 * 3600000).toISOString(),
    },
  ];

  const recentIncidents: Incident[] = [
    {
      id: 'INC-001',
      title: 'Production Database Unauthorized Access',
      description: 'Multiple alerts indicate unauthorized access attempts on the production database cluster.',
      severity: 'critical',
      status: 'investigating',
      assignedTo: 'Sarah Chen',
      relatedAlerts: 4,
      createdAt: new Date(Date.now() - 2 * 3600000).toISOString(),
      updatedAt: new Date(Date.now() - 30 * 60000).toISOString(),
    },
    {
      id: 'INC-004',
      title: 'Suspicious Data Exfiltration Pattern',
      description: 'Anomalous outbound data transfers detected from production web servers.',
      severity: 'critical',
      status: 'open',
      relatedAlerts: 2,
      createdAt: new Date(Date.now() - 1 * 3600000).toISOString(),
      updatedAt: new Date(Date.now() - 45 * 60000).toISOString(),
    },
    {
      id: 'INC-002',
      title: 'Cryptominer Detected on Staging Nodes',
      description: 'XMRig cryptominer binary found on two staging servers.',
      severity: 'high',
      status: 'open',
      assignedTo: 'Mike Torres',
      relatedAlerts: 3,
      createdAt: new Date(Date.now() - 5 * 3600000).toISOString(),
      updatedAt: new Date(Date.now() - 4 * 3600000).toISOString(),
    },
  ];

  const topRules: TopRule[] = [
    {
      name: 'Excessive Failed Logins',
      triggerCount: 23,
      lastTriggered: new Date(Date.now() - 32 * 60000).toISOString(),
    },
    {
      name: 'Unauthorized Root Access',
      triggerCount: 18,
      lastTriggered: new Date(Date.now() - 15 * 60000).toISOString(),
    },
    {
      name: 'Firewall Rule Modified',
      triggerCount: 12,
      lastTriggered: new Date(Date.now() - 2 * 3600000).toISOString(),
    },
    {
      name: 'Anomalous Network Traffic',
      triggerCount: 9,
      lastTriggered: new Date(Date.now() - 5 * 3600000).toISOString(),
    },
    {
      name: 'Configuration Drift Detected',
      triggerCount: 7,
      lastTriggered: new Date(Date.now() - 48 * 3600000).toISOString(),
    },
  ];

  return {
    activeAlerts: 17,
    openIncidents: 4,
    complianceScore: 87,
    monitoredNodes: 42,
    recentAlerts,
    recentIncidents,
    topRules,
    loading: false,
    error: null,
  };
}
