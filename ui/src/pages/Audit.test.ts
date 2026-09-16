import { describe, expect, it } from 'vitest';
import type { AuditLog } from '../lib/api';
import { matchesAuditSearch } from './Audit';

describe('matchesAuditSearch', () => {
  it('matches correlation identifiers stored in audit metadata', () => {
    const log = {
      id: 'audit-1',
      created_at: '2026-09-16T00:00:00Z',
      action: 'alert.notification_delivered',
      resource_type: 'alert',
      resource_id: 'alert-1',
      actor_type: 'system',
      metadata: { correlation_id: 'rule-1/tenant-1' },
    } as AuditLog;

    expect(matchesAuditSearch(log, 'rule-1/tenant-1')).toBe(true);
    expect(matchesAuditSearch(log, 'missing-correlation')).toBe(false);
  });
});
