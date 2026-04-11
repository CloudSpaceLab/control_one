import { useMemo, useState } from 'react';
import { Incident } from '../lib/api';

const MOCK_INCIDENTS: Incident[] = [
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
    id: 'INC-002',
    title: 'Cryptominer Detected on Staging Nodes',
    description: 'XMRig cryptominer binary found on two staging servers. Likely exploited via exposed Docker socket.',
    severity: 'high',
    status: 'open',
    assignedTo: 'Mike Torres',
    relatedAlerts: 3,
    createdAt: new Date(Date.now() - 5 * 3600000).toISOString(),
    updatedAt: new Date(Date.now() - 4 * 3600000).toISOString(),
  },
  {
    id: 'INC-003',
    title: 'Brute Force Attack on SSH Services',
    description: 'Coordinated SSH brute force campaign targeting production API servers from multiple IPs.',
    severity: 'high',
    status: 'investigating',
    assignedTo: 'Sarah Chen',
    relatedAlerts: 7,
    createdAt: new Date(Date.now() - 8 * 3600000).toISOString(),
    updatedAt: new Date(Date.now() - 1 * 3600000).toISOString(),
  },
  {
    id: 'INC-004',
    title: 'Suspicious Data Exfiltration Pattern',
    description: 'Anomalous outbound data transfers detected from production web servers during off-hours.',
    severity: 'critical',
    status: 'open',
    relatedAlerts: 2,
    createdAt: new Date(Date.now() - 1 * 3600000).toISOString(),
    updatedAt: new Date(Date.now() - 45 * 60000).toISOString(),
  },
  {
    id: 'INC-005',
    title: 'TLS Certificate Chain Misconfiguration',
    description: 'Several nodes reporting certificate validation errors after recent deployment.',
    severity: 'medium',
    status: 'resolved',
    assignedTo: 'Alex Rivera',
    relatedAlerts: 5,
    createdAt: new Date(Date.now() - 72 * 3600000).toISOString(),
    updatedAt: new Date(Date.now() - 48 * 3600000).toISOString(),
    resolvedAt: new Date(Date.now() - 48 * 3600000).toISOString(),
  },
  {
    id: 'INC-006',
    title: 'Firewall Policy Bypass Detected',
    description: 'Internal scan revealed nodes with modified iptables rules allowing unrestricted outbound traffic.',
    severity: 'medium',
    status: 'closed',
    assignedTo: 'Mike Torres',
    relatedAlerts: 3,
    createdAt: new Date(Date.now() - 168 * 3600000).toISOString(),
    updatedAt: new Date(Date.now() - 120 * 3600000).toISOString(),
    resolvedAt: new Date(Date.now() - 120 * 3600000).toISOString(),
  },
];

interface UseIncidentsParams {
  status?: string;
  severity?: string;
}

interface UseIncidentsResult {
  data: Incident[];
  loading: boolean;
  error: string | null;
  reload: () => void;
}

export function useIncidents(params: UseIncidentsParams = {}): UseIncidentsResult {
  const [loading] = useState(false);
  const [error] = useState<string | null>(null);

  const filtered = useMemo(() => {
    let result = [...MOCK_INCIDENTS];
    if (params.status) {
      result = result.filter((i) => i.status === params.status);
    }
    if (params.severity) {
      result = result.filter((i) => i.severity === params.severity);
    }
    return result;
  }, [params.status, params.severity]);

  return {
    data: filtered,
    loading,
    error,
    reload: () => {},
  };
}
