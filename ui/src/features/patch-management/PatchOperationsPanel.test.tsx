import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import { PatchOperationsPanel } from './PatchOperationsPanel';
import type { PatchOperationsResponse } from './operationsTypes';
const api = vi.hoisted(() => ({ getPatchAutomationRuns: vi.fn(), listPatchApprovals: vi.fn(), getPatchReviewOperations: vi.fn(), listMaintenanceWindows: vi.fn(), listNodes: vi.fn(), savePatchReviewOperations: vi.fn(), previewPatchReviewRetention: vi.fn(), purgePatchReviewRetention: vi.fn(), checkPatchReviewEscalations: vi.fn() }));
vi.mock('@/hooks/useApiClient', () => ({ useApiClient: () => api }));
const fixture = (): PatchOperationsResponse => ({ operations: { revision: 2, updated_by: 'admin', updated_at: new Date().toISOString(), policy: { owner: 'Lab operations', reason: 'Local qualification', retention: { mode: 'retain', days: 90, legal_hold: true }, notifications: { enabled: false, channel: 'in_app', recipients: [], repeat_hours: 24 }, automation: { mode: 'recommend_only', enabled: false, node_ids: [], canary_node_ids: [], packages: [], exclusions: [], window_id: '', transport: '', max_wave_size: 1, health_profile: '', require_approval: true, require_exact_plan: true, require_fresh_assessment: true, require_health: true, require_reboot_verification: true, stop_on_failure: true } } }, recipients: [{ id: 'current-user', name: 'Lab operator' }], delivery: { delivered: 0, last_error: '' }, can_manage: true, current_user_id: 'current-user', effective_installation_mode: 'recommend_only', installation_activation_available: false, notification_channel: 'in_app', external_delivery_available: false });
beforeEach(() => { vi.resetAllMocks(); api.getPatchAutomationRuns.mockResolvedValue({runs:[],limit:100}); api.listPatchApprovals.mockResolvedValue({data:[]}); api.getPatchReviewOperations.mockResolvedValue(fixture()); api.listMaintenanceWindows.mockResolvedValue({ windows: [] }); api.listNodes.mockResolvedValue({ data: [] }); api.savePatchReviewOperations.mockResolvedValue(fixture().operations); });
it('shows saved controls to a read-only reviewer without requesting editing dependencies',async()=>{api.getPatchReviewOperations.mockResolvedValue({...fixture(),can_manage:false});api.listNodes.mockRejectedValue(new Error('No node editing access'));api.listMaintenanceWindows.mockRejectedValue(new Error('No calendar editing access'));render(<PatchOperationsPanel tenantId="lab"/>);expect(await screen.findByText('Saved operational controls — read only')).toBeVisible();expect(api.listNodes).not.toHaveBeenCalled();expect(api.listMaintenanceWindows).not.toHaveBeenCalled();expect(screen.queryByRole('button',{name:'Save operational controls'})).not.toBeInTheDocument();});
it('saves explicit self-only in-app notification scope without enabling installations', async () => {
    const user = userEvent.setup();
    render(<PatchOperationsPanel tenantId="lab"/>);
    await user.click(await screen.findByText('Escalation notification handling'));
    await user.click(screen.getByRole('button', { name: 'Notify only my account' }));
    await user.click(screen.getByRole('checkbox', { name: 'Enable in-app escalation notifications' }));
    await user.click(screen.getByRole('button', { name: 'Save operational controls' }));
    await waitFor(() => expect(api.savePatchReviewOperations).toHaveBeenCalledWith('lab', 2, expect.objectContaining({ notifications: expect.objectContaining({ enabled: true, recipients: ['current-user'] }), automation: expect.objectContaining({ mode: 'recommend_only', enabled: false }) })));
});
it('makes scheduled and automatic controls drafts with no activation action', async () => {
    const user = userEvent.setup();
    render(<PatchOperationsPanel tenantId="lab"/>);
    await user.click(await screen.findByText('Explicit automation policy draft'));
    await user.selectOptions(screen.getByLabelText('Requested automation mode'), 'approved_scheduled');
    expect(await screen.findByRole('status')).toHaveTextContent('execution disabled');
    expect(screen.getByRole('checkbox', { name: 'Require individual approval' })).toBeDisabled();
    await user.selectOptions(screen.getByLabelText('Requested automation mode'), 'automatic');
    expect(screen.queryByRole('button', { name: /Activate installation/i })).not.toBeInTheDocument();
});
it('keeps a legal-hold preview non-destructive', async () => {
    const user = userEvent.setup();
    api.previewPatchReviewRetention.mockResolvedValue({ revision: 2, count: 3, cutoff: '2026-01-01T00:00:00Z', sha256: 'a'.repeat(64), blocked: 'Legal hold is enabled' });
    render(<PatchOperationsPanel tenantId="lab"/>);
    await user.click(await screen.findByText('Evidence retention and legal hold'));
    await user.click(screen.getByRole('button', { name: 'Preview retention batch' }));
    expect(await screen.findByRole('status')).toHaveTextContent('Legal hold');
    expect(screen.getByRole('button', { name: 'Review permanent history purge' })).toBeDisabled();
    expect(api.purgePatchReviewRetention).not.toHaveBeenCalled();
});
it('requires verified archive details and confirmation, and retains a rejected purge', async () => {
    const user = userEvent.setup();
    const preview = { revision: 2, count: 3, cutoff: '2026-01-01T00:00:00Z', sha256: 'a'.repeat(64) };
    api.previewPatchReviewRetention.mockResolvedValue(preview);
    api.purgePatchReviewRetention.mockRejectedValue(new Error('archive coverage changed'));
    render(<PatchOperationsPanel tenantId="lab"/>);
    await user.click(await screen.findByText('Evidence retention and legal hold'));
    await user.click(screen.getByRole('button', { name: 'Preview retention batch' }));
    await screen.findByRole('status');
    expect(screen.getByRole('button', { name: 'Review permanent history purge' })).toBeDisabled();
    await user.type(screen.getByLabelText('Complete export archive ID'), 'archive-id');
    await user.type(screen.getByLabelText('Verified archive destination'), 'isolated verified archive');
    await user.click(screen.getByRole('checkbox', { name: 'I retained and verified this complete export at the stated destination' }));
    await user.click(screen.getByRole('button', { name: 'Review permanent history purge' }));
    expect(api.purgePatchReviewRetention).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Permanently purge this batch' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('archive coverage changed');
    expect(api.purgePatchReviewRetention).toHaveBeenCalledWith('lab', preview, 'archive-id', 'isolated verified archive');
});
it('does not offer privileged writes to a tenant viewer', async () => {
    const f = fixture();
    f.can_manage = false;
    api.getPatchReviewOperations.mockResolvedValue(f);
    render(<PatchOperationsPanel tenantId="lab"/>);
    expect(await screen.findByText('Saved operational controls — read only')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Save operational controls' })).not.toBeInTheDocument();
});

it('stops an enabled installation policy even when its stored draft expiry has passed',async()=>{
 const user=userEvent.setup();const f=fixture();f.operations.policy.automation={...f.operations.policy.automation,mode:'automatic',enabled:true,valid_until:'2020-01-01T00:00:00Z'};f.effective_installation_mode='automatic';api.getPatchReviewOperations.mockResolvedValue(f);render(<PatchOperationsPanel tenantId="lab"/>);await user.click(await screen.findByRole('button',{name:'Stop installation automation'}));await waitFor(()=>expect(api.savePatchReviewOperations).toHaveBeenCalledWith('lab',2,expect.objectContaining({automation:expect.objectContaining({mode:'recommend_only',enabled:false})})));
});
