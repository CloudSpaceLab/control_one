import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { Cases, caseSummaryText } from './Cases';
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
  evidence: {
    src_ip: '203.0.113.10',
    process_name: 'nginx',
    confidence: 100,
    details: {
      source_file: '/var/log/nginx/access.log',
    },
  },
  evidence_refs: [
    { id: 'ai_investigations:11111111-1111-1111-1111-111111111111', kind: 'soc_case' },
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
  citations: [
    {
      id: 'ai_investigations:11111111-1111-1111-1111-111111111111',
      kind: 'soc_case',
      table: 'ai_investigations',
      source_record_id: 'ai_investigations:11111111-1111-1111-1111-111111111111',
    },
  ],
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

const secondCase: SOCCase = {
  ...caseRow,
  case_id: '22222222-2222-2222-2222-222222222222',
  title: 'Database audit gap',
  status: 'investigating',
  severity: 'high',
  source: 'db_audit',
  trigger_type: 'db',
  trigger_event_type: 'db.audit.gap',
  dedup_key: 'db-audit-gap',
  summary: 'Database audit evidence is missing for the settlement schema.',
  evidence: {
    application_name: 'core-banking',
    source_file: '/var/log/postgresql/audit.log',
  },
  evidence_refs: [],
  timeline: [],
  notes: [],
  citations: [],
  coverage_badges: [
    { id: 'actions_proposal_only', label: 'Actions proposal-only', tone: 'info' },
  ],
  export_url: '',
};

// eslint-disable-next-line @typescript-eslint/no-explicit-any
let mockApi: any;

describe('Cases', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    mockApi = {
      listSOCCases: vi.fn().mockResolvedValue({
        data: [caseRow],
        pagination: { total: 1, count: 1, limit: 50, offset: 0, nextOffset: null, prevOffset: null },
      }),
      getSOCCase: vi.fn().mockResolvedValue(caseRow),
      addSOCCaseNote: vi.fn().mockResolvedValue({
        id: 'note-1',
        tenant_id: 'tenant-1',
        case_id: caseRow.case_id,
        note: 'Confirmed scope.',
        citations: [],
        audit_id: 'audit-1',
        created_at: '2026-05-21T10:05:00Z',
        guardrails: ['tenant_scoped'],
      }),
      exportSOCCase: vi.fn().mockResolvedValue(exportRow),
    };
    vi.spyOn(useTenantModule, 'useTenant').mockReturnValue({
      currentTenantId: 'tenant-1',
      currentTenant: { id: 'tenant-1', name: 'Bank Tenant', created_at: '2026-05-21T00:00:00Z' },
      tenants: [],
      loading: false,
      error: null,
      setCurrentTenantId: vi.fn(),
      refresh: vi.fn(),
    });
    vi.spyOn(useApiClientModule, 'useApiClient').mockReturnValue(mockApi);
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
    expect(screen.getByText('Case facts')).toBeInTheDocument();
    expect(screen.getByText('203.0.113.10')).toBeInTheDocument();
    expect(screen.getByText('/var/log/nginx/access.log')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /review ip block/i })).toBeInTheDocument();
    expect(screen.getByText('normalized_events:row-1')).toBeInTheDocument();
    expect(screen.getAllByText('ai_investigations:11111111-1111-1111-1111-111111111111').length).toBeGreaterThan(0);
    expect(screen.getByText('Timeline')).toBeInTheDocument();
    expect(screen.getByText('Source-row cited')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /preview export/i }));

    expect(await screen.findByText('soc-case-export-v1')).toBeInTheDocument();
    expect(screen.getByText('evidence_refs_only')).toBeInTheDocument();
    expect(screen.getByText('no_enforcement_execution')).toBeInTheDocument();
  });

  it('uses trigger type as secondary text when case title and summary match', () => {
    expect(caseSummaryText({
      title: 'first connection to 20.169.85.72',
      summary: 'first connection to 20.169.85.72',
      trigger_event_type: 'anomaly.new_destination',
      dedup_key: 'anomaly.new_dst:tenant-1:20.169.85.72',
    })).toBe('anomaly.new_destination');
  });

  it('does not show a false empty state when the case list fails', async () => {
    mockApi.listSOCCases.mockRejectedValueOnce(new Error('case store unavailable'));

    render(
      <MemoryRouter>
        <Cases />
      </MemoryRouter>,
    );

    expect(await screen.findByRole('alert')).toHaveTextContent('case store unavailable');
    expect(screen.getByText(/case queue could not be loaded/i)).toBeInTheDocument();
    expect(screen.queryByText('No SOC cases yet')).not.toBeInTheDocument();
  });

  it('clears stale case detail when the selected case detail fails', async () => {
    const user = userEvent.setup();
    mockApi.listSOCCases.mockResolvedValueOnce({
      data: [caseRow, secondCase],
      pagination: { total: 2, count: 2, limit: 50, offset: 0, nextOffset: null, prevOffset: null },
    });
    mockApi.getSOCCase.mockImplementation((caseId: string) => {
      if (caseId === secondCase.case_id) {
        return Promise.reject(new Error('case detail unavailable'));
      }
      return Promise.resolve(caseRow);
    });

    render(
      <MemoryRouter>
        <Cases />
      </MemoryRouter>,
    );

    expect(await screen.findByText('Evidence drawer')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /database audit gap/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent('case detail unavailable');
    await waitFor(() => {
      expect(screen.queryByText('Evidence drawer')).not.toBeInTheDocument();
    });
    expect(screen.getByText('Select a case')).toBeInTheDocument();
  });

  it('surfaces export preview failures without an unhandled dead end', async () => {
    const user = userEvent.setup();
    mockApi.exportSOCCase.mockRejectedValueOnce(new Error('case export unavailable'));

    render(
      <MemoryRouter>
        <Cases />
      </MemoryRouter>,
    );

    await screen.findByText('Export packet');
    await user.click(screen.getByRole('button', { name: /preview export/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Export preview failed: case export unavailable',
    );
    expect(screen.queryByText('soc-case-export-v1')).not.toBeInTheDocument();
  });

  it('keeps note submission failures visible and preserves the draft', async () => {
    const user = userEvent.setup();
    mockApi.addSOCCaseNote.mockRejectedValueOnce(new Error('audit write unavailable'));

    render(
      <MemoryRouter>
        <Cases />
      </MemoryRouter>,
    );

    const noteBox = await screen.findByPlaceholderText(/add analyst decision/i);
    await user.type(noteBox, 'Escalate to the SOC manager before closure.');
    await user.click(screen.getByRole('button', { name: /add note/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Note failed: audit write unavailable');
    expect(noteBox).toHaveValue('Escalate to the SOC manager before closure.');
  });
});
