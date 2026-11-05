import { useEffect, useRef, useState } from 'react';
import { useApiClient } from '@/hooks/useApiClient';
import { Button } from '@/components/ui/button';
import { ConfirmModal } from '@/components/ConfirmModal';
import {PatchAutomationPanel} from './PatchAutomationPanel';
import type { NodeSummary, MaintenanceWindow } from '@/lib/api';
import type { PatchOperationsResponse, PatchReviewOperationsPolicy, PatchRetentionPreview } from './operationsTypes';
const field = 'block w-full rounded border border-border bg-surface-1 p-2 text-sm';
export function PatchOperationsPanel({ tenantId }: {
    tenantId: string;
}): JSX.Element { return <OperationsWorkspace key={tenantId} tenantId={tenantId}/>; }
function OperationsWorkspace({ tenantId }: {
    tenantId: string;
}): JSX.Element {
    const api = useApiClient();
    const alive = useRef(true);
    const [data, setData] = useState<PatchOperationsResponse | null>(null);
    const [policy, setPolicy] = useState<PatchReviewOperationsPolicy | null>(null);
    const [nodes, setNodes] = useState<NodeSummary[]>([]);
    const [windows, setWindows] = useState<MaintenanceWindow[]>([]);
    const [error, setError] = useState('');
    const [message, setMessage] = useState('');
    const [busy, setBusy] = useState(false);
    const [preview, setPreview] = useState<PatchRetentionPreview | null>(null);
    const [archive, setArchive] = useState('');
    const [destination, setDestination] = useState('');
    const [verified, setVerified] = useState(false);
    const [confirm, setConfirm] = useState(false);
	const [confirmActivation,setConfirmActivation]=useState(false);
    useEffect(() => { alive.current = true; let cancelled = false; api.getPatchReviewOperations(tenantId).then(async result=>{
        if(!result.can_manage){if(!cancelled){setData(result);setPolicy(result.operations.policy);}return;}
        const [calendar,machines]=await Promise.all([api.listMaintenanceWindows(tenantId), (async () => { const all: NodeSummary[] = []; let offset = 0; for (;;) {
            const page = await api.listNodes({ tenantId, limit: 500, offset });
            all.push(...page.data);
            const next = page.pagination?.nextOffset;
            if (next == null || next <= offset)
                return all;
            offset = next;
        } })()]); if (!cancelled) {
        setData(result);
        setPolicy(result.operations.policy);
        setWindows(calendar.windows);
        setNodes(machines.filter(n => n.state === 'active'));
    } }).catch(err => { if (!cancelled)
        setError(err instanceof Error ? err.message : 'Operations unavailable'); }); return () => { cancelled = true; alive.current = false; }; }, [api, tenantId]);
    const run = async (action: () => Promise<void>) => { setBusy(true); setError(''); setMessage(''); try {
        await action();
    }
    catch (err) {
        if (alive.current)
            setError(err instanceof Error ? err.message : 'Operation failed; refresh before retrying.');
    }
    finally {
        if (alive.current)
            setBusy(false);
    } };
    const reload = async () => { const result = await api.getPatchReviewOperations(tenantId); if (alive.current) {
        setData(result);
        setPolicy(result.operations.policy);
        setPreview(null);
        setVerified(false);
    } };
    const edit = (next: PatchReviewOperationsPolicy) => { setPolicy(next); setPreview(null); setVerified(false); };
    if (!data || !policy)
        return <section aria-label="Patch operational controls"><h2>Operational controls</h2>{error ? <p role="alert">{error}</p> : <p>Loading operational controls…</p>}<Button disabled={busy} onClick={() => void run(reload)}>Retry operational controls</Button></section>;
    const canManage = data.can_manage && !busy;
    if(!data.can_manage)return <section aria-label="Patch operational controls" className="space-y-3 rounded border border-border p-4"><h2>Saved operational controls — read only</h2><p>Revision {data.operations.revision}. Effective installation mode: {data.effective_installation_mode}. Only tenant administrators can configure these controls.</p><p>Operations owner: {policy.owner}. Reason: {policy.reason}.</p><p>Target IDs: {policy.automation.node_ids.join(', ')||'none'}. Packages: {policy.automation.packages.join(', ')||'none'}.</p><p>Retention: {policy.retention.days} days. Legal hold: {String(policy.retention.legal_hold)}. Notifications enabled: {String(policy.notifications.enabled)}.</p><Button disabled={busy} onClick={()=>void run(reload)}>Reload saved operational controls</Button>{error&&<p role="alert">{error}</p>}</section>;
    const automation = policy.automation;
	const save=()=>run(async()=>{await api.savePatchReviewOperations(tenantId,data.operations.revision,policy);if(alive.current){await reload();setConfirmActivation(false);setMessage(policy.automation.enabled?'Scoped installation policy saved. Current execution gates still apply.':'Operational controls saved. Installation activation remains disabled.');}});
    return <section aria-label="Patch operational controls" className="space-y-4 rounded border border-border p-4"><h2 className="text-lg font-semibold">Retention, notifications and automation controls</h2>
 <p>Operations revision {data.operations.revision || 'Not configured'}. Effective installation mode: {data.effective_installation_mode}. Drafts create no installation authority; an enabled saved policy still requires all current execution gates.</p>
 {error && <p role="alert" className="text-destructive">{error}</p>}{message && <p role="status" className="break-all">{message}</p>}
 <label className="block">Operations owner/team<input disabled={!canManage} className={field} value={policy.owner} onChange={e => edit({ ...policy, owner: e.target.value })}/></label><label className="block">Operations change reason<input disabled={!canManage} className={field} value={policy.reason} onChange={e => edit({ ...policy, reason: e.target.value })}/></label>
 <details><summary>Evidence retention and legal hold</summary><p>Retain preserves history. Archive-then-purge permits a reviewed batch of historical review events only after a complete archive is downloaded, verified and retained. Current revisions, findings, approvals and installation receipts are preserved.</p>
 <label className="block">Retention mode<select disabled={!canManage} className={field} value={policy.retention.mode} onChange={e => edit({ ...policy, retention: { ...policy.retention, mode: e.target.value as 'retain' | 'archive_then_purge' } })}><option value="retain">Retain all history</option><option value="archive_then_purge">Archive then reviewed purge</option></select></label>
 <label className="block">Retain review history for at least days<input disabled={!canManage} type="number" min={1} max={3650} className={field} value={policy.retention.days} onChange={e => edit({ ...policy, retention: { ...policy.retention, days: Number(e.target.value) } })}/></label>
 <label className="block"><input disabled={!canManage} type="checkbox" checked={policy.retention.legal_hold} onChange={e => edit({ ...policy, retention: { ...policy.retention, legal_hold: e.target.checked } })}/> Legal hold: prevent purge</label>
 <Button disabled={!canManage || !data.operations.revision} onClick={() => void run(async () => { const result = await api.previewPatchReviewRetention(tenantId); if (alive.current) {
        setPreview(result);
        setVerified(false);
    } })}>Preview retention batch</Button>
 {preview && <div className="space-y-2"><p role="status">Retention preview: {preview.count} eligible historical events before {preview.cutoff}. {preview.blocked ? `Blocked: ${preview.blocked}` : 'A complete verified archive and confirmation are required.'}</p><p className="break-all">Preview SHA-256: {preview.sha256}</p>
 <label className="block">Complete export archive ID<input disabled={!canManage} className={field} value={archive} onChange={e => { setArchive(e.target.value); setVerified(false); }}/></label><label className="block">Verified archive destination<input disabled={!canManage} className={field} value={destination} onChange={e => { setDestination(e.target.value); setVerified(false); }}/></label>
 <label className="block"><input disabled={!canManage} type="checkbox" checked={verified} onChange={e => setVerified(e.target.checked)}/> I retained and verified this complete export at the stated destination</label>
 <Button disabled={!canManage || !!preview.blocked || preview.count === 0 || !verified || !archive || !destination} onClick={() => setConfirm(true)}>Review permanent history purge</Button></div>}
 </details>
 <details><summary>Escalation notification handling</summary><p>Supported delivery is the tenant in-app notification inbox. Email and webhooks are not enabled by these controls. Every delivery rechecks recipient access; receipts prevent duplicates in the configured repeat interval. Failed checks retry on the next minute without claiming delivery.</p>
 <label className="block"><input disabled={!canManage} type="checkbox" checked={policy.notifications.enabled} onChange={e => edit({ ...policy, notifications: { ...policy.notifications, enabled: e.target.checked } })}/> Enable in-app escalation notifications</label>
 <label className="block">Repeat interval (hours)<input disabled={!canManage} type="number" min={1} max={168} className={field} value={policy.notifications.repeat_hours} onChange={e => edit({ ...policy, notifications: { ...policy.notifications, repeat_hours: Number(e.target.value) } })}/></label>
 <fieldset><legend>Explicit authorized recipients</legend>{data.recipients.map(person => <label className="block" key={person.id}><input type="checkbox" disabled={!canManage} checked={policy.notifications.recipients.includes(person.id)} onChange={e => edit({ ...policy, notifications: { ...policy.notifications, recipients: e.target.checked ? [...policy.notifications.recipients, person.id] : policy.notifications.recipients.filter(id => id !== person.id) } })}/>{person.name}{person.id === data.current_user_id ? ' (you)' : ''}</label>)}</fieldset>
 <Button disabled={!canManage} onClick={() => edit({ ...policy, notifications: { ...policy.notifications, recipients: [data.current_user_id] } })}>Notify only my account</Button>
 <p>Last check: {data.delivery.checked_at ?? 'Not run'}. Last delivered count: {data.delivery.delivered}. {data.delivery.last_error}</p><Button disabled={!canManage || !data.operations.revision} onClick={() => void run(async () => { const result = await api.checkPatchReviewEscalations(tenantId); if (alive.current) {
        await reload();
        setMessage(`Escalation check committed ${result.delivered} in-app notifications. Repeating the same interval does not duplicate them.`);
    } })}>Run saved escalation check</Button>
 </details>
 <details><summary>Explicit automation policy draft</summary><p>Scheduled/automatic policies begin disabled. Supported execution requires a fresh readiness check, explicit administrator activation and individually approved exact canary requests. Broader production qualification remains separate.</p>
 <label className="block">Requested automation mode<select disabled={!canManage} className={field} value={automation.mode} onChange={e => edit({ ...policy, automation: { ...automation, mode: e.target.value as typeof automation.mode, enabled: false, require_approval: e.target.value === 'approved_scheduled' ? true : automation.require_approval, max_wave_size: automation.max_wave_size || 1 } })}><option value="recommend_only">Recommend only</option><option value="approved_scheduled">Approved scheduled installation (draft)</option><option value="automatic">Narrow automatic installation (draft)</option></select></label>
 {automation.mode !== 'recommend_only' && <div className="space-y-2"><p role="status">{automation.enabled ? 'Enabled policy requested — explicit Save and current gates are required.' : 'Draft only — execution disabled. Existing plans, approvals and jobs are not expanded or changed.'}</p><fieldset><legend>Explicit target and canary scope</legend>{nodes.map(node => <div key={node.id}><label><input type="checkbox" disabled={!canManage} checked={automation.node_ids.includes(node.id)} onChange={e => edit({ ...policy, automation: { ...automation, node_ids: e.target.checked ? [...automation.node_ids, node.id] : automation.node_ids.filter(id => id !== node.id), canary_node_ids: e.target.checked ? automation.canary_node_ids : automation.canary_node_ids.filter(id => id !== node.id) } })}/>{node.hostname} ({node.id.slice(0, 8)})</label><label className="ml-3"><input type="checkbox" disabled={!canManage || !automation.node_ids.includes(node.id)} checked={automation.canary_node_ids.includes(node.id)} onChange={e => edit({ ...policy, automation: { ...automation, canary_node_ids: e.target.checked ? [...automation.canary_node_ids, node.id] : automation.canary_node_ids.filter(id => id !== node.id) } })}/>Canary {node.hostname}</label></div>)}</fieldset>
 <label className="block">Package scope<input disabled={!canManage} className={field} key={`packages-${data.operations.revision}-${automation.mode}`} defaultValue={automation.packages.join(', ')} onChange={e => edit({ ...policy, automation: { ...automation, packages: e.target.value.split(/[\s,]+/).filter(Boolean) } })}/></label><label className="block">Excluded packages<input disabled={!canManage} className={field} key={`exclusions-${data.operations.revision}-${automation.mode}`} defaultValue={automation.exclusions.join(', ')} onChange={e => edit({ ...policy, automation: { ...automation, exclusions: e.target.value.split(/[\s,]+/).filter(Boolean) } })}/></label>
 <label className="block">Draft maintenance window<select disabled={!canManage} className={field} value={automation.window_id} onChange={e => edit({ ...policy, automation: { ...automation, window_id: e.target.value } })}><option value="">Select an existing tenant window</option>{windows.map(window => <option key={window.ID} value={window.ID}>{window.Name} — {window.Status}</option>)}</select></label><p className="text-xs">Window availability and membership must be rechecked before any future activation; a saved draft does not make a closed window usable.</p>
 <label className="block">Draft transport<select disabled={!canManage} className={field} value={automation.transport} onChange={e => edit({ ...policy, automation: { ...automation, transport: e.target.value } })}><option value="">Choose transport</option><option>direct</option><option>proxy</option><option>airgapped</option></select></label><label className="block">Maximum wave size<input disabled={!canManage} type="number" min={1} max={100} className={field} value={automation.max_wave_size} onChange={e => edit({ ...policy, automation: { ...automation, max_wave_size: Number(e.target.value) } })}/></label><label className="block">Health and recovery profile<input disabled={!canManage} className={field} value={automation.health_profile} onChange={e => edit({ ...policy, automation: { ...automation, health_profile: e.target.value } })}/></label>
 <label className="block">Draft expiry (UTC)<input disabled={!canManage} type="datetime-local" className={field} value={automation.valid_until?.slice(0, 16) ?? ''} onInput={e => { const raw = e.currentTarget.value; const parsed = raw ? new Date(raw + 'Z') : null; edit({ ...policy, automation: { ...automation, valid_until: parsed && !Number.isNaN(parsed.getTime()) ? parsed.toISOString() : undefined } }); }}/></label><p className="text-xs">The declared health/recovery profile is a review requirement, not a qualified or executed probe.</p><p>Parsed expiry: {automation.valid_until ?? 'Not set'}</p>
 <label className="block"><input type="checkbox" disabled={!canManage || automation.mode === 'approved_scheduled'} checked={automation.require_approval} onChange={e => edit({ ...policy, automation: { ...automation, require_approval: e.target.checked } })}/> Require individual approval</label><p>Always required: immutable exact plan, fresh assessment, verified health and reboot outcomes, canary verification, bounded waves, maintenance window and stop on failure. Unknown or changed evidence blocks execution and promotion.</p></div>}
 </details>
 <PatchAutomationPanel tenantId={tenantId} policy={policy} revision={data.operations.revision} nodes={nodes} canManage={canManage} available={data.installation_activation_available} onChange={edit}/>
 {data.operations.policy.automation.enabled&&<Button disabled={!canManage} onClick={()=>void run(async()=>{const stopped={...data.operations.policy,automation:{...data.operations.policy.automation,mode:'recommend_only' as const,enabled:false}};await api.savePatchReviewOperations(tenantId,data.operations.revision,stopped);await reload();setMessage('Installation policy stopped. Undispatched schedules and start-time authorizations require the saved current revision. Running package managers are not force-killed.');})}>Stop installation automation</Button>}
 <div className="flex gap-2"><Button disabled={!canManage} onClick={()=>{if(policy.automation.enabled)setConfirmActivation(true);else void save();}}>Save operational controls</Button><Button disabled={busy} onClick={() => void run(reload)}>Reload saved operational controls</Button></div>
 {confirmActivation&&<ConfirmModal open title="Enable this scoped installation policy?" body={`Mode ${automation.mode}; targets ${automation.node_ids.length}; packages ${automation.packages.join(', ')}; window ${automation.window_id}. This delegates installation authority within this scope. Individually approved canaries, current health and exact-plan checks remain mandatory. Unknown or failed evidence stops execution.`} confirmLabel="Save enabled scoped policy" confirmDisabled={busy} cancelDisabled={busy} onCancel={()=>setConfirmActivation(false)} onConfirm={()=>void save()}>{error&&<p role="alert">{error}</p>}</ConfirmModal>}
 {confirm && preview && <ConfirmModal open title="Permanently purge reviewed history?" body={`This deletes ${preview.count} eligible historical review events from this tenant. The complete verified archive, current revisions, findings and approval/job receipts remain. This cannot be undone through the app.`} confirmLabel="Permanently purge this batch" variant="danger" confirmDisabled={busy} cancelDisabled={busy} onCancel={() => setConfirm(false)} onConfirm={() => void run(async () => { const result = await api.purgePatchReviewRetention(tenantId, preview, archive, destination); if (alive.current) {
        setConfirm(false);
        setPreview(null);
        setMessage(`Purged ${result.deleted} historical review events; a retention receipt was retained.`);
    } })}>{error && <p role="alert" className="text-destructive">{error}</p>}</ConfirmModal>}
 </section>;
}
