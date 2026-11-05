import {useEffect,useRef,useState} from 'react';
import {Link} from 'react-router-dom';
import {useApiClient} from '@/hooks/useApiClient';
import {Button} from '@/components/ui/button';
import {ConfirmModal} from '@/components/ConfirmModal';
import {useAuth} from '@/providers/AuthProvider';
import type {TenantRoleChange,TenantRoleResponse,TenantRoleAssignment} from './tenantRoleTypes';
const field='block w-full rounded border border-border bg-surface-1 p-2 text-sm';
export function TenantRolePanel({tenantId}:{tenantId:string}):JSX.Element{return <TenantRoleWorkspace key={tenantId} tenantId={tenantId}/>;}
function TenantRoleWorkspace({tenantId}:{tenantId:string}):JSX.Element{
 const api=useApiClient();const {refreshProfile}=useAuth();const alive=useRef(true);
 const[data,setData]=useState<TenantRoleResponse|null>(null);const[error,setError]=useState('');const[message,setMessage]=useState('');const[busy,setBusy]=useState(false);
 const[target,setTarget]=useState('');const[role,setRole]=useState('viewer');const[reason,setReason]=useState('');const[expiry,setExpiry]=useState(()=>new Date(Date.now()+3600000).toISOString());
 const[confirmation,setConfirmation]=useState<TenantRoleChange|null>(null);const[now,setNow]=useState(Date.now());
 useEffect(()=>{alive.current=true;let cancelled=false;api.getTenantRoleControls(tenantId).then(value=>{if(!cancelled)setData(value);}).catch(e=>{if(!cancelled)setError(e instanceof Error?e.message:'Access controls unavailable');});const timer=setInterval(()=>setNow(Date.now()),30000);return()=>{cancelled=true;alive.current=false;clearInterval(timer);};},[api,tenantId]);
 const reload=async()=>{setBusy(true);setError('');try{await refreshProfile({background:true});const value=await api.getTenantRoleControls(tenantId);if(alive.current){setData(value);setNow(Date.now());}}catch(e){if(alive.current){setData(null);setError(e instanceof Error?e.message:'Access controls unavailable');}}finally{if(alive.current)setBusy(false);}};
 const review=(action:'grant'|'revoke',assignment?:TenantRoleAssignment)=>{if(!data)return;setError('');setMessage('');if(!reason.trim()){setError('Enter a reason before reviewing an access change.');return;}setConfirmation({expected_revision:data.controls.revision,user_id:assignment?.user_id??target,role:assignment?.role??role,action,expires_at:action==='grant'?expiry:new Date().toISOString(),reason});};
 const commit=async()=>{if(!confirmation)return;setBusy(true);setError('');try{await api.saveTenantRoleControl(tenantId,confirmation);if(alive.current){setConfirmation(null);setMessage('Tenant assignment saved. Global roles and credentials were not changed.');await reload();}}catch(e){if(alive.current)setError(e instanceof Error?e.message:'Access change failed; reload and review again.');}finally{if(alive.current)setBusy(false);}};
 const labels:Record<string,string>={configure_policy:'Configure organizational policy',approve_designated_organization_policy:'Approve a designated organizational policy',request_or_approve_patch:'Request or approve a patch change',stop_automation:'Stop installation automation'};
 return <section aria-label="Tenant access and action permissions" className="space-y-3 rounded border border-border p-4">
  <h2 className="text-lg font-semibold">Tenant access & action permissions</h2>
  <p>Use existing accounts. Expiring tenant assignments are separate from global roles. Revoking a tenant assignment does not remove access inherited from a global role.</p>
  {error&&<p role="alert">{error}</p>}{message&&<p role="status">{message}</p>}
  <Button disabled={busy} onClick={()=>void reload()}>Refresh tenant access evidence</Button>
  {!data?<p>Current access evidence unavailable.</p>:<>
   <p>Access revision {data.controls.revision}. {data.scope}</p>
   <h3>My tenant action permissions at last refresh</h3><ul>{Object.entries(labels).map(([key,label])=><li key={key}>{label}: {data.permissions[key]?'Allowed by current role':'Not allowed by current role'}</li>)}</ul>
   <p>Refresh after a role change or expiry. The server rechecks authority for each action; this displayed snapshot does not grant permission.</p>
   <p>These role checks do not replace designation, independent approval, exact plans, maintenance windows or host health. The enrolled agent executes authorized jobs.</p>
   {data.can_manage?<details><summary>Manage existing-account tenant assignments</summary>
    <Link to="/users">Existing accounts and global role management</Link>
    <p>Only current global administrators manage these assignments. Tenant administrator delegation and account creation are unavailable here. Showing the first {data.people_limit} of {data.people_total} existing account choices.</p>
    <label>Existing account<select className={field} disabled={busy} value={target} onChange={e=>setTarget(e.target.value)}><option value="">Select an existing account</option>{data.people.filter(p=>p.id!==data.current_user_id).map(person=><option key={person.id} value={person.id}>{person.name||person.id}</option>)}</select></label>
    <label>Tenant role<select className={field} disabled={busy} value={role} onChange={e=>setRole(e.target.value)}>{data.allowed_roles.map(value=><option key={value}>{value}</option>)}</select></label>
    <label>Tenant role expiry (UTC)<input className={field} disabled={busy} type="datetime-local" value={expiry.slice(0,16)} onInput={e=>{const parsed=e.currentTarget.value?new Date(e.currentTarget.value+'Z'):null;setExpiry(parsed&&!Number.isNaN(parsed.getTime())?parsed.toISOString():'');}}/></label>
    <p>Expiry: {expiry||'Not set'}. Grants must expire within 30 days. No self-grants or global administrator grants.</p>
    <label>Access change reason<textarea className={field} disabled={busy} maxLength={2000} value={reason} onChange={e=>setReason(e.target.value)}/></label>
    <Button disabled={busy||!target||!reason.trim()||!expiry||Date.parse(expiry)<=now} onClick={()=>review('grant')}>Review tenant role assignment</Button>
    <h3>Existing global and tenant assignments</h3>
    {data.controls.assignments.map(assignment=><div key={assignment.id} className="rounded border border-border p-2"><p>{assignment.name} · {assignment.role} · {assignment.global?'Global — inherited across tenants':'This tenant'} · {assignment.expires_at?(Date.parse(assignment.expires_at)>now?'Expires ':'Expired/revoked ')+assignment.expires_at:'No expiry recorded'}</p>{!assignment.global&&assignment.user_id!==data.current_user_id&&(!assignment.expires_at||Date.parse(assignment.expires_at)>now)&&<Button disabled={busy||!reason.trim()} onClick={()=>review('revoke',assignment)}>Review revoke {assignment.name} {assignment.role}</Button>}</div>)}
   </details>:<p>Read-only access evidence for your account. You cannot grant or revoke assignments.</p>}
   <details><summary>Tenant access change history</summary><p>Latest {data.history_limit} retained events in your authorized view. All events remain stored.</p>{data.controls.history.map(event=><p key={event.revision}>Revision {event.revision} · {event.action} {event.role} · user {event.user_id} · actor {event.actor_id} · {event.created_at} · {event.reason}</p>)}</details>
   <ConfirmModal open={!!confirmation} title="Confirm tenant access change" body={confirmation?`${confirmation.action.toUpperCase()} ${confirmation.role} for ${data.people.find(p=>p.id===confirmation.user_id)?.name??confirmation.user_id} in this tenant. ${confirmation.action==='grant'?'Expires '+confirmation.expires_at:'This revokes only the tenant assignment; global roles remain.'} Reason: ${confirmation.reason}. Access revision ${confirmation.expected_revision}.`:''} confirmLabel="Apply reviewed tenant access change" confirmDisabled={busy} cancelDisabled={busy} onCancel={()=>setConfirmation(null)} onConfirm={()=>void commit()}>{error&&<p role="alert">{error}</p>}</ConfirmModal>
  </>}
 </section>;
}
