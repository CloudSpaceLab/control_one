import {useEffect, useState} from 'react';
import {useApiClient} from '@/hooks/useApiClient';
import {Button} from '@/components/ui/button';
import {ConfirmModal} from '@/components/ConfirmModal';
import type {OrganizationPolicy, OrganizationPolicyResponse} from './organizationTypes';

const empty: OrganizationPolicy = {name:'',owner_id:'',approver_id:'',responsibilities:'',scope:'',customer_authorization:'',exclusions:'',reason:'',effective_at:'',expires_at:''};
const field='block w-full rounded border border-border bg-surface-1 p-2 text-sm';
export function PatchOrganizationPanel({tenantId}:{tenantId:string}):JSX.Element {
  return <OrganizationWorkspace key={tenantId} tenantId={tenantId}/>;
}
function OrganizationWorkspace({tenantId}:{tenantId:string}):JSX.Element {
  const api=useApiClient();
  const [data,setData]=useState<OrganizationPolicyResponse|null>(null);
  const [policy,setPolicy]=useState<OrganizationPolicy>(empty);
  const [busy,setBusy]=useState(false);
  const [error,setError]=useState('');
  const [message,setMessage]=useState('');
  const [confirmation,setConfirmation]=useState('');
  useEffect(()=>{let cancelled=false;api.getPatchOrganizationPolicy(tenantId).then(value=>{if(!cancelled){setData(value);setPolicy(value.record?.policy??empty);}}).catch(e=>{if(!cancelled)setError(e instanceof Error?e.message:'Policy unavailable');});return()=>{cancelled=true;};},[api,tenantId]);
  const reload=async()=>{setBusy(true);setError('');try{const value=await api.getPatchOrganizationPolicy(tenantId);setData(value);setPolicy(value.record?.policy??empty);}catch(e){setData(null);setError(e instanceof Error?e.message:'Policy unavailable');}finally{setBusy(false);}};
  const save=async(action:string)=>{if(!data)return;setBusy(true);setError('');setMessage('');try{await api.savePatchOrganizationPolicy(tenantId,data.record?.revision??0,action,policy);setConfirmation('');await reload();setMessage('Organizational policy record saved. No installation authorized or started.');}catch(e){setError(e instanceof Error?e.message:'Save failed; reload before retrying.');}finally{setBusy(false);}};
  const record=data?.record;
  const canApprove=!!(data?.can_approve??data?.can_manage)&&record?.status==='review'&&record.policy.approver_id===data.current_user_id&&record.authored_by!==data.current_user_id;
  return <section aria-label="Organizational policy approval" className="space-y-3 rounded border border-border p-4">
    <h2 className="text-lg font-semibold">Organizational policy approval</h2>
    <p>An administrator prepares the policy; the designated independent administrator or CISO approves it. Organizational approval is an additional execution gate, not installation approval or production qualification.</p>
    {error&&<p role="alert">{error}</p>}{message&&<p role="status">{message}</p>}
    <Button disabled={busy} onClick={()=>void reload()}>Reload organizational policy</Button>
    {!data?<p>Organizational policy evidence unavailable.</p>:<>
      <p role="status">{data.review_status}</p>
      <p>Revision {record?.revision??0} · recorded state: {record?.status??'not configured'} · urgency revision {record?.urgency_revision??'not bound'} · operations revision {record?.operations_revision??'not bound'}</p>
      <p className="text-sm">{data.execution_integration}</p>
      {record&&<details open><summary>Exact saved controls for independent review</summary>
        <p>Policy: {record.policy.name}. Accountable person: {data.administrators.find(p=>p.id===record.policy.owner_id)?.name??record.policy.owner_id}. Designated approver: {(data.approvers??data.administrators).find(p=>p.id===record.policy.approver_id)?.name??record.policy.approver_id}.</p>
        <p>Effective {record.policy.effective_at}; expires {record.policy.expires_at}. UTC.</p>
        <p>Declared scope: {record.policy.scope}</p><p>Responsibilities: {record.policy.responsibilities}</p><p>Authorization reference: {record.policy.customer_authorization}</p><p>Exclusions/risks: {record.policy.exclusions}</p>
        {!record.bound_controls?.operations||!record.bound_controls.urgency?<p role="alert">This older record has no complete saved-controls snapshot. An administrator must save and submit a fresh draft.</p>:<>
          <p>Saved installation mode: {record.bound_controls.operations.automation.mode}; enabled: {String(record.bound_controls.operations.automation.enabled)}. Transport: {record.bound_controls.operations.automation.transport??'not selected'}.</p>
          <p>Exact target IDs: {record.bound_controls.operations.automation.node_ids.join(', ')||'none'}. Canary IDs: {record.bound_controls.operations.automation.canary_node_ids.join(', ')||'none'}.</p>
          <p>Packages: {record.bound_controls.operations.automation.packages.join(', ')||'none'}. Exclusions: {record.bound_controls.operations.automation.exclusions.join(', ')||'none'}.</p>
          <p>Window: {record.bound_controls.operations.automation.window_id||'none'}. Wave size: {record.bound_controls.operations.automation.max_wave_size??'not set'}. Observation minutes: {record.bound_controls.operations.automation.observation_minutes??'not set'}.</p>
          <p>Urgency rules (first match): {record.bound_controls.urgency.rules.map(rule=>`${rule.name}: ${rule.priority}, ${rule.due_hours} hours`).join('; ')}.</p>
          <p>Review retention: {record.bound_controls.operations.retention.days} days. Legal hold: {String(record.bound_controls.operations.retention.legal_hold)}. In-app escalation enabled: {String(record.bound_controls.operations.notifications.enabled)}.</p>
        </>}
      </details>}
      <details><summary>Configure organizational policy (administrator)</summary>
        <fieldset disabled={busy||!data.can_manage} className="space-y-3">
          <label>Organizational policy name<input className={field} maxLength={160} value={policy.name} onChange={e=>setPolicy({...policy,name:e.target.value})}/></label>
          {(['owner_id','approver_id'] as const).map(key=><label className="block" key={key}>{key==='owner_id'?'Accountable person':'Independent designated approver'}<select className={field} value={policy[key]} onChange={e=>setPolicy({...policy,[key]:e.target.value})}><option value="">Select an authorized tenant identity</option>{(key==='owner_id'?data.administrators:(data.approvers??data.administrators)).map(person=><option key={person.id} value={person.id}>{person.name}</option>)}</select></label>)}
          <p>Accountable owners require tenant administrator access; designated approvers may be administrators or CISOs. CISOs can approve their designated record but cannot edit or submit policy. The author cannot self-approve.</p>
          {(['responsibilities','scope','customer_authorization','exclusions','reason'] as const).map(key=><label className="block" key={key}>{({responsibilities:'Responsible functions and duties',scope:'Declared production scope',customer_authorization:'Customer authorization reference',exclusions:'Exclusions and accepted risks',reason:'Organizational change reason'})[key]}<textarea className={field} maxLength={key==='scope'||key==='exclusions'?4000:2000} value={policy[key]} onChange={e=>setPolicy({...policy,[key]:e.target.value})}/></label>)}
          {(['effective_at','expires_at'] as const).map(key=><label className="block" key={key}>{key==='effective_at'?'Policy effective time (UTC)':'Policy expiry (UTC)'}<input className={field} type="datetime-local" value={policy[key].slice(0,16)} onInput={e=>{const value=e.currentTarget.value;const parsed=value?new Date(value+'Z'):null;setPolicy({...policy,[key]:parsed&&!Number.isNaN(parsed.getTime())?parsed.toISOString():''});}}/></label>)}
          <p>A saved draft binds the current urgency and operational revisions. Changing those controls requires renewed review. Expiry is at most 90 days; do not enter credentials or private keys in these fields.</p>
          <Button onClick={()=>void save('draft')}>Save organizational draft</Button>
        </fieldset>
      </details>
      <div className="flex flex-wrap gap-2">
        <Button disabled={busy||!data.can_manage||record?.status!=='draft'} onClick={()=>setConfirmation('submit')}>Submit saved policy for review</Button>
        <Button disabled={busy||!canApprove} onClick={()=>setConfirmation('approve')}>Approve saved organizational policy</Button>
        <Button disabled={busy||!data.can_manage||!record||record.status==='withdrawn'} onClick={()=>setConfirmation('withdraw')}>Withdraw organizational record</Button>
      </div>
      <p>Lifecycle actions use the saved record, not unsaved edits. For adopted tenants, draft, review, withdrawal, expiry or changed controls block new dispatch and exact online APT starts. A running package manager is not force-killed. Unqualified offline/other start paths remain unavailable.</p>
      <details><summary>Organizational sign-off history</summary><p>Latest {data.history_limit} retained revisions. Full history remains stored separately from review-event purge.</p>{data.history.map(item=><p key={item.revision}>Revision {item.revision} · {item.status} · actor {item.changed_by} · {item.updated_at} · bound controls {item.urgency_revision}/{item.operations_revision}</p>)}</details>
      <ConfirmModal open={!!confirmation} title="Confirm organizational policy action" body={`Action: ${confirmation}. Saved revision ${record?.revision??0}, ${record?.policy.name??''}. This changes organizational authority; separate exact patch approval and execution gates still apply.`} confirmLabel="Confirm saved policy action" confirmDisabled={busy} cancelDisabled={busy} onCancel={()=>setConfirmation('')} onConfirm={()=>void save(confirmation)}>{error&&<p role="alert">{error}</p>}</ConfirmModal>
    </>}
  </section>;
}
