import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import { PatchReviewPanel } from './PatchReviewPanel';
import type { PatchReviewResponse } from './reviewTypes';
const api = vi.hoisted(() => ({ getPatchReviews: vi.fn(), downloadPatchReviewEvidence:vi.fn(),savePatchReviewPolicy: vi.fn(), simulatePatchPriority: vi.fn(), savePatchFindingReview: vi.fn() }));
vi.mock('@/hooks/useApiClient', () => ({ useApiClient: () => api }));
const response = (): PatchReviewResponse => ({ tenant_id: 'lab', permissions:{manage_policy:true,review_findings:true}, generated_at: new Date().toISOString(), policy: null, findings: [], total_findings: 0, findings_truncated: false, events: [], history_truncated: false, history_window_days: 0, retention: 'Durable history retained; automatic deletion disabled.', automation: { effective_mode: 'recommend_only', installation_authorized: false, stop_conditions: ['unknown evidence'] } });
beforeEach(() => { vi.resetAllMocks(); api.getPatchReviews.mockResolvedValue(response()); });
it('pages findings and history without treating a page as the complete export',async()=>{
 const user=userEvent.setup();const first=response();first.total_findings=102;first.pagination={limit:25,history_cursor:0,next_cursor:'cursor-next',next_history_cursor:90};api.getPatchReviews.mockImplementation(async(_tenant,_export,page)=>({...first,pagination:{...first.pagination,cursor:page?.cursor??'',history_cursor:page?.historyCursor??0}}));render(<PatchReviewPanel tenantId="lab"/>);await user.click(await screen.findByRole('button',{name:'Next findings'}));await waitFor(()=>expect(api.getPatchReviews).toHaveBeenCalledWith('lab',false,expect.objectContaining({cursor:'cursor-next'})));await user.click(screen.getByText('Review history (0 on this page)'));await user.click(screen.getByRole('button',{name:'Older review history'}));await waitFor(()=>expect(api.getPatchReviews).toHaveBeenCalledWith('lab',false,expect.objectContaining({cursor:'cursor-next',historyCursor:90})));await user.click(screen.getByRole('button',{name:'Previous findings'}));await waitFor(()=>expect(api.getPatchReviews).toHaveBeenCalledWith('lab',false,expect.objectContaining({cursor:'',historyCursor:90})));
});
it('keeps viewer policy writes disabled',async()=>{
 const r=response();r.permissions={manage_policy:false,review_findings:false};api.getPatchReviews.mockResolvedValue(r);render(<PatchReviewPanel tenantId="lab"/>);expect(await screen.findByRole('button',{name:'Save recommendation policy'})).toBeDisabled();expect(api.savePatchReviewPolicy).not.toHaveBeenCalled();
});
it('starts a fresh review without renewing an expired exception or expired context',async()=>{
 const user=userEvent.setup();const r=response();r.findings=[{finding:{id:'finding',node_id:'node-123',package_name:'liba',installed_version:'1',cve_id:'CVE-EXPIRED',severity:'high',kev:false,first_seen_at:r.generated_at,last_seen_at:r.generated_at},tracking:{revision:4,tracking:{owner:'Lab',escalate_to:'Queue',reason:'Previous review',exposure:'exposed',importance:'critical',compatibility:'reviewed',context_expires_at:'2020-01-01T00:00:00Z',exception_until:'2020-01-01T00:00:00Z'}},decision:{priority:'review_required',reasons:['Context expired'],overdue:false,installation_authorized:false},exception_status:'expired',escalation_required:true,evidence_current:false}];api.getPatchReviews.mockResolvedValue(r);render(<PatchReviewPanel tenantId="lab"/>);await user.click(await screen.findByRole('button',{name:'Review CVE-EXPIRED on node-123'}));expect(screen.getByLabelText('Exception expiry (UTC, administrator)')).toHaveValue('');expect(screen.getByLabelText('Review exposure')).toHaveValue('unknown');expect(screen.getByLabelText('Review compatibility')).toHaveValue('unknown');expect(screen.getByText(/Expired exceptions and context stay in history/)).toBeVisible();expect(api.savePatchFindingReview).not.toHaveBeenCalled();
});
it('requires an explicit save of a labelled lab example and binds the current revision', async () => {
    const user = userEvent.setup();
    api.savePatchReviewPolicy.mockImplementation(async (_tenant, revision, policy) => ({ revision: revision + 1, policy }));
    render(<PatchReviewPanel tenantId="lab"/>);
    await screen.findByText(/Saved policy revision:/);
    await user.click(screen.getByRole('button', { name: 'Load lab example (unsaved)' }));
    expect(api.savePatchReviewPolicy).not.toHaveBeenCalled();
    expect(screen.getByRole('status')).toHaveTextContent('not a production policy');
    await user.click(screen.getByRole('button', { name: 'Save recommendation policy' }));
    await waitFor(() => expect(api.savePatchReviewPolicy).toHaveBeenCalledWith('lab', 0, expect.objectContaining({ mode: 'recommend_only', owner: 'Local Development operator' })));
    expect(screen.getByText(/separately enabled scoped policy/)).toBeVisible();
});
it('shows unavailable on failure and does not use another tenant response', async () => {
    let resolveOld!: (value: PatchReviewResponse) => void;
    api.getPatchReviews.mockImplementation((tenant: string) => tenant === 'old' ? new Promise(resolve => { resolveOld = resolve; }) : Promise.reject(new Error('unavailable')));
    const view = render(<PatchReviewPanel tenantId="old"/>);
    view.rerender(<PatchReviewPanel tenantId="new"/>);
    expect(await screen.findByRole('alert')).toHaveTextContent('unavailable');
    await act(async () => resolveOld({ ...response(), total_findings: 99 }));
    expect(screen.queryByText(/99 active/)).not.toBeInTheDocument();
});
it('does not display a stale scenario result after its hypothetical inputs change', async () => {
    const user = userEvent.setup();
    const r = response();
    r.policy = { revision: 2, policy: { name: 'Lab', owner: 'Lab', mode: 'recommend_only', retention_days: 30, rules: [] }, updated_by: 'lab', updated_at: r.generated_at };
    api.getPatchReviews.mockResolvedValue(r);
    api.simulatePatchPriority.mockResolvedValue({ simulation: true, decision: { priority: 'urgent', reasons: ['Hypothetical only'], installation_authorized: false, overdue: false } });
    render(<PatchReviewPanel tenantId="lab"/>);
    await screen.findByText(/Saved policy revision:/);
    await user.click(screen.getByText('Try a hypothetical priority scenario'));
    await user.click(screen.getByRole('button', { name: 'Evaluate hypothetical scenario' }));
    expect(await screen.findByText(/Hypothetical result: urgent/)).toBeVisible();
    await user.selectOptions(screen.getByLabelText('Scenario severity'), 'low');
    expect(screen.queryByText(/Hypothetical result: urgent/)).not.toBeInTheDocument();
});
it('retains a failed review without claiming that a finding resolved', async () => {
    const user = userEvent.setup();
    const r = response();
    r.findings = [{ finding: { id: 'finding', node_id: 'node-123', package_name: 'liba', installed_version: '1', cve_id: 'CVE-EXAMPLE', severity: 'high', kev: false, first_seen_at: r.generated_at, last_seen_at: r.generated_at }, decision: { priority: 'review_required', reasons: ['Evidence unavailable'], overdue: false, installation_authorized: false }, exception_status: 'none', escalation_required: true, evidence_current: false }];
    r.total_findings = 1;
    api.getPatchReviews.mockResolvedValue(r);
    api.savePatchFindingReview.mockRejectedValue(new Error('review changed; reload'));
    render(<PatchReviewPanel tenantId="lab"/>);
    await user.click(await screen.findByRole('button', { name: 'Review CVE-EXAMPLE on node-123' }));
    await user.type(screen.getByLabelText('Finding owner/team'), 'Lab');
    await user.type(screen.getByLabelText('Escalation destination'), 'Lab queue');
    await user.type(screen.getByLabelText('Review reason'), 'Needs evidence');
    fireEvent.input(screen.getByLabelText('Due date override (UTC)'),{target:{value:'2026-11-02T11:47'}});
    fireEvent.input(screen.getByLabelText('Exception expiry (UTC, administrator)'),{target:{value:'2026-11-02T15:00'}});
    expect(screen.getByRole('status')).toHaveTextContent('2026-11-02T11:47:00.000Z');
    await user.click(screen.getByRole('button', { name: 'Save finding review' }));
    expect(api.savePatchFindingReview).toHaveBeenCalledWith('lab','finding',0,expect.objectContaining({due_at:'2026-11-02T11:47:00.000Z',exception_until:'2026-11-02T15:00:00.000Z'}));
    expect(await screen.findByRole('alert')).toHaveTextContent('review changed');
    expect(screen.getByRole('button', { name: 'Save finding review' })).toBeVisible();
    expect(screen.queryByText(/Finding review saved/)).not.toBeInTheDocument();
});
