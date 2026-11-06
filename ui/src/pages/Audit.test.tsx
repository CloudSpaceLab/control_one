import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { AuditLog } from '../lib/api';
import { Audit } from './Audit';

const mocks = vi.hoisted(() => {
  const useAuditLogs = vi.fn();
  const reload = vi.fn();
  return {
    useAuditLogs,
    reload,
    auditState: {
      data: [] as AuditLog[],
      loading: false,
      error: null as string | null,
      pagination: { total: 0, count: 0, limit: 100, offset: 0, nextOffset: null, prevOffset: null },
      reload,
    },
  };
});

vi.mock('../hooks/useAuditLogs', () => ({
  useAuditLogs: (params: unknown) => mocks.useAuditLogs(params),
}));

vi.mock('../hooks/useTenants', () => ({
  useTenants: () => ({
    data: [{ id: 'tenant-1', name: 'Bank Tenant', created_at: '2026-01-01T00:00:00Z' }],
    loading: false,
    error: null,
    pagination: { total: 1, count: 1, limit: 10, offset: 0, nextOffset: null, prevOffset: null },
    reload: vi.fn(),
    refresh: vi.fn(),
  }),
}));

vi.mock('../providers/TenantProvider', () => ({
  useTenant: () => ({ currentTenantId: 'tenant-1' }),
}));

vi.mock('./AuditReports', () => ({
  AuditReports: () => <div>Reports child</div>,
}));

vi.mock('react-chartjs-2', () => ({
  Bar: () => <div data-testid="bar-chart" />,
  Doughnut: () => <div data-testid="doughnut-chart" />,
  Line: () => <div data-testid="line-chart" />,
}));

const userLogin: AuditLog = {
  id: 'audit-1',
  tenant_id: 'tenant-1',
  actor_id: 'user-1',
  actor_type: 'user',
  action: 'user.login',
  resource_type: 'session',
  resource_id: 'session-1',
  metadata: { ip: '203.0.113.10' },
  created_at: '2026-06-08T00:00:00Z',
};

const nodeDelete: AuditLog = {
  id: 'audit-2',
  tenant_id: 'tenant-1',
  actor_id: 'user-2',
  actor_type: 'user',
  action: 'node.delete',
  resource_type: 'node',
  resource_id: 'node-1',
  metadata: { hostname: 'old-node' },
  created_at: '2026-06-08T00:05:00Z',
};

function renderAudit() {
  return render(
    <MemoryRouter>
      <Audit />
    </MemoryRouter>,
  );
}

describe('Audit page production hardening', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.auditState = {
      data: [],
      loading: false,
      error: null,
      pagination: { total: 0, count: 0, limit: 100, offset: 0, nextOffset: null, prevOffset: null },
      reload: mocks.reload,
    };
    mocks.useAuditLogs.mockImplementation(() => mocks.auditState);
    Object.defineProperty(URL, 'createObjectURL', {
      configurable: true,
      value: vi.fn(() => 'blob:audit-csv'),
    });
    Object.defineProperty(URL, 'revokeObjectURL', {
      configurable: true,
      value: vi.fn(),
    });
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders audit load failures as unavailable instead of false empty results', async () => {
    mocks.auditState = {
      data: [],
      loading: false,
      error: 'audit database offline',
      pagination: { total: 0, count: 0, limit: 100, offset: 0, nextOffset: null, prevOffset: null },
      reload: mocks.reload,
    };

    renderAudit();

    expect(await screen.findAllByText('Audit trail unavailable')).toHaveLength(2);
    expect(screen.getByText('audit database offline')).toBeInTheDocument();
    expect(screen.queryByText('No audit entries')).not.toBeInTheDocument();
    expect(screen.getByText('Pagination unavailable until audit entries reload.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /export current audit page as csv/i })).toBeDisabled();
    expect(screen.getAllByText('N/A')).toHaveLength(3);
    expect(mocks.useAuditLogs).toHaveBeenCalledWith(expect.objectContaining({ tenant_id: 'tenant-1' }));
  });

  it('exports only the visible filtered audit rows from the current page', async () => {
    const user = userEvent.setup();
    mocks.auditState = {
      data: [userLogin, nodeDelete],
      loading: false,
      error: null,
      pagination: { total: 2, count: 2, limit: 100, offset: 0, nextOffset: null, prevOffset: null },
      reload: mocks.reload,
    };

    renderAudit();

    expect(await screen.findAllByText('user.login')).toHaveLength(2);
    await user.type(screen.getByLabelText(/search/i), 'login');
    await user.click(screen.getByRole('button', { name: /export current audit page as csv/i }));

    await waitFor(() => expect(URL.createObjectURL).toHaveBeenCalledTimes(1));
    const blob = vi.mocked(URL.createObjectURL).mock.calls[0][0] as Blob;
    const csv = await readBlobText(blob);
    expect(csv).toContain('user.login');
    expect(csv).toContain('session-1');
    expect(csv).toContain('Actor ID');
    expect(csv).toContain('user-1');
    expect(csv).toContain('audit-1');
    expect(csv).not.toContain('node.delete');
    expect(csv).not.toContain('old-node');
  });
});

function readBlobText(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result ?? ''));
    reader.onerror = () => reject(reader.error);
    reader.readAsText(blob);
  });
}

it('requests global identity evidence explicitly without a tenant',async()=>{
 mocks.useAuditLogs.mockReturnValue({...mocks.auditState,data:[],error:null,loading:false});
 const user=userEvent.setup();renderAudit();
 await user.selectOptions(screen.getByLabelText('Audit scope'),'global_identity');
 await waitFor(()=>expect(mocks.useAuditLogs).toHaveBeenLastCalledWith(expect.objectContaining({scope:'global_identity',tenant_id:undefined})));
 expect(screen.getByText(/Current global administrator access required/)).toBeVisible();
});

it('does not turn a user resource UUID into a session link',()=>{
 mocks.useAuditLogs.mockReturnValue({...mocks.auditState,error:null,loading:false,data:[{...userLogin,resource_type:'user',resource_id:'2a1779fb-abc0-4fc5-8c8e-cea66c1234a1'}]});
 renderAudit();expect(screen.queryByRole('link',{name:/SESS/})).not.toBeInTheDocument();
});

it('explains global identity denial without a misleading retry or export',async()=>{
 const user=userEvent.setup();mocks.useAuditLogs.mockReturnValue({...mocks.auditState,data:[],error:'Current global administrator required',errorStatus:403,loading:false});renderAudit();await user.selectOptions(screen.getByLabelText('Audit scope'),'global_identity');
 expect(screen.getAllByText('Global audit access restricted').length).toBeGreaterThan(0);expect(screen.queryByRole('button',{name:'Retry'})).not.toBeInTheDocument();expect(screen.getByRole('button',{name:'Export current audit page as CSV'})).toBeDisabled();
});
