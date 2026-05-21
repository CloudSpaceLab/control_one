import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Cases } from './Cases';
import * as useApiClientModule from '@/hooks/useApiClient';
import * as useTenantModule from '@/providers/TenantProvider';
import type { SOCCase, SOCCaseExport } from '@/lib/api';

const caseRow: SOCCase = {
  case_id: '11111111-1111-1111-1111-111111111111',
  tenant_id: 'tenant-1',
  title: 'Suspicious IP investigation',
  status: 'open',
  severity: 'critical',
  source: 'ai_investigation',
  trigger_type: 'ip_behavior',
  trigger_event_type: 'credential_attack',
  dedup_key: 'ip:203.0.113.10',
  summary: 'Credential attack timeline with cited source rows.',
  evidence_refs: [
    { id: 'normalized_events:row-1', kind: 'event' },
    { id: 'posture_receipts:receipt-7', kind: 'posture_receipt' },
  ],
  timeline: [
    {
      timestamp: '2026-05-21T10:00:00Z',
      event: 'case.created',
      source: 'ai_investigations',
      citation_id: 'ai_investigations:11111111-1111-1111-1111-111111111111',
      description: 'Case created from cited timeline.',
    },
  ],
  notes: [],
  citations: [],
  coverage_badges: [
    { id: 'source_row_citations', label: 'Source-row cited', tone: 'healthy' },
    { id: 'actions_proposal_only', label: 'Actions proposal-only', tone: 'info' },
  ],
  export_url: '/api/v1/soc/cases/11111111-1111-1111-1111-111111111111/export?tenant_id=tenant-1',
  created_at: '2026-05-21T10:00:00Z',
  updated_at: '2026-05-21T10:01:00Z',
};

const exportRow: SOCCaseExport = {
  export_version: 'soc-case-export-v1',
  generated_at: '2026-05-21T10:02:00Z',
  tenant_id: 'tenant-1',
  case: caseRow,
  evidence: caseRow.evidence_refs ?? [],
  notes: [],
  guardrails: ['tenant_scoped', 'source_row_citations', 'evidence_refs_only', 'no_enforcement_execution'],
};

describe('Cases', () => {
  beforeEach(() => {
    vi.spyOn(useTenantModule, 'useTenant').mockReturnValue({
      currentTenantId: 'tenant-1',
      currentTenant: { id: 'tenant-1', name: 'Bank Tenant', created_at: '2026-05-21T00:00:00Z' },
      tenants: [],
      loading: false,
      error: null,
      setCurrentTenantId: vi.fn(),
      refresh: vi.fn(),
    });
    vi.spyOn(useApiClientModule, 'useApiClient').mockReturnValue({
      listSOCCases: vi.fn().mockResolvedValue({
        data: [caseRow],
        pagination: { total: 1, count: 1, limit: 50, offset: 0, nextOffset: null, prevOffset: null },
      }),
      getSOCCase: vi.fn().mockResolvedValue(caseRow),
      addSOCCaseNote: vi.fn(),
      exportSOCCase: vi.fn().mockResolvedValue(exportRow),
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } as any);
  });

  it('renders cases as evidence-backed export packets', async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <Cases />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(screen.getAllByText('Suspicious IP investigation').length).toBeGreaterThan(0);
    });

    expect(await screen.findByText('Evidence drawer')).toBeInTheDocument();
    expect(screen.getByText('normalized_events:row-1')).toBeInTheDocument();
    expect(screen.getByText('Timeline')).toBeInTheDocument();
    expect(screen.getByText('Source-row cited')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /preview export/i }));

    expect(await screen.findByText('soc-case-export-v1')).toBeInTheDocument();
    expect(screen.getByText('evidence_refs_only')).toBeInTheDocument();
    expect(screen.getByText('no_enforcement_execution')).toBeInTheDocument();
  });
});
