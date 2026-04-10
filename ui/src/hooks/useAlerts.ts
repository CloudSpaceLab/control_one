import { useMemo, useState } from 'react';
import { Alert } from '../lib/api';

const MOCK_ALERTS: Alert[] = [
  {
    id: 'alert-001',
    severity: 'critical',
    ruleName: 'Unauthorized Root Access',
    source: 'node-prod-web-01',
    message: 'Root login detected from unknown IP 203.0.113.42',
    status: 'open',
    tenantId: 'tenant-001',
    nodeId: 'node-001',
    createdAt: new Date(Date.now() - 15 * 60000).toISOString(),
  },
  {
    id: 'alert-002',
    severity: 'high',
    ruleName: 'Excessive Failed Logins',
    source: 'node-prod-api-02',
    message: '47 failed SSH login attempts in the last 10 minutes',
    status: 'acknowledged',
    tenantId: 'tenant-001',
    nodeId: 'node-002',
    createdAt: new Date(Date.now() - 32 * 60000).toISOString(),
    acknowledgedAt: new Date(Date.now() - 20 * 60000).toISOString(),
  },
  {
    id: 'alert-003',
    severity: 'critical',
    ruleName: 'Malware Signature Detected',
    source: 'node-prod-db-01',
    message: 'Known cryptominer binary detected in /tmp/xmrig',
    status: 'open',
    tenantId: 'tenant-002',
    nodeId: 'node-003',
    createdAt: new Date(Date.now() - 45 * 60000).toISOString(),
  },
  {
    id: 'alert-004',
    severity: 'medium',
    ruleName: 'Firewall Rule Modified',
    source: 'node-staging-01',
    message: 'Outbound rule added allowing traffic on port 4444',
    status: 'open',
    tenantId: 'tenant-001',
    nodeId: 'node-004',
    createdAt: new Date(Date.now() - 2 * 3600000).toISOString(),
  },
  {
    id: 'alert-005',
    severity: 'low',
    ruleName: 'SSL Certificate Expiring',
    source: 'node-prod-web-02',
    message: 'TLS certificate for api.example.com expires in 14 days',
    status: 'resolved',
    tenantId: 'tenant-001',
    nodeId: 'node-005',
    createdAt: new Date(Date.now() - 24 * 3600000).toISOString(),
    resolvedAt: new Date(Date.now() - 12 * 3600000).toISOString(),
  },
  {
    id: 'alert-006',
    severity: 'high',
    ruleName: 'Privilege Escalation Attempt',
    source: 'node-prod-api-01',
    message: 'User "deploy" attempted sudo without authorization',
    status: 'acknowledged',
    tenantId: 'tenant-002',
    nodeId: 'node-006',
    createdAt: new Date(Date.now() - 3 * 3600000).toISOString(),
    acknowledgedAt: new Date(Date.now() - 2.5 * 3600000).toISOString(),
  },
  {
    id: 'alert-007',
    severity: 'medium',
    ruleName: 'Anomalous Network Traffic',
    source: 'node-prod-web-01',
    message: 'Outbound data transfer spike: 2.3GB in 5 minutes to external IP',
    status: 'open',
    tenantId: 'tenant-001',
    nodeId: 'node-001',
    createdAt: new Date(Date.now() - 5 * 3600000).toISOString(),
  },
  {
    id: 'alert-008',
    severity: 'low',
    ruleName: 'Configuration Drift Detected',
    source: 'node-staging-02',
    message: 'sshd_config modified: PermitRootLogin changed to yes',
    status: 'resolved',
    tenantId: 'tenant-001',
    nodeId: 'node-007',
    createdAt: new Date(Date.now() - 48 * 3600000).toISOString(),
    resolvedAt: new Date(Date.now() - 46 * 3600000).toISOString(),
  },
  {
    id: 'alert-009',
    severity: 'critical',
    ruleName: 'Data Exfiltration Suspected',
    source: 'node-prod-db-01',
    message: 'Large database dump initiated by non-admin user',
    status: 'open',
    tenantId: 'tenant-002',
    nodeId: 'node-003',
    createdAt: new Date(Date.now() - 10 * 60000).toISOString(),
  },
  {
    id: 'alert-010',
    severity: 'medium',
    ruleName: 'Outdated Package Vulnerability',
    source: 'node-prod-api-02',
    message: 'CVE-2024-3094 detected in xz-utils 5.6.0',
    status: 'acknowledged',
    tenantId: 'tenant-001',
    nodeId: 'node-002',
    createdAt: new Date(Date.now() - 6 * 3600000).toISOString(),
    acknowledgedAt: new Date(Date.now() - 4 * 3600000).toISOString(),
  },
];

interface UseAlertsParams {
  severity?: string;
  status?: string;
}

interface UseAlertsResult {
  data: Alert[];
  loading: boolean;
  error: string | null;
  reload: () => void;
}

export function useAlerts(params: UseAlertsParams = {}): UseAlertsResult {
  const [loading] = useState(false);
  const [error] = useState<string | null>(null);

  const filtered = useMemo(() => {
    let result = [...MOCK_ALERTS];
    if (params.severity) {
      result = result.filter((a) => a.severity === params.severity);
    }
    if (params.status) {
      result = result.filter((a) => a.status === params.status);
    }
    return result;
  }, [params.severity, params.status]);

  return {
    data: filtered,
    loading,
    error,
    reload: () => {},
  };
}
