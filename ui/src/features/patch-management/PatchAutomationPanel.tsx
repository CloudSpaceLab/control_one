import {useEffect,useState} from 'react';
import {useApiClient} from '@/hooks/useApiClient';
import {Button} from '@/components/ui/button';
import {ConfirmModal} from '@/components/ConfirmModal';
import type {NodeSummary,PatchApproval} from '@/lib/api';
import type {PatchReviewOperationsPolicy,PatchAutomationRun} from './operationsTypes';
const field='block w-full rounded border border-border bg-surface-1 p-2 text-sm';
export function PatchAutomationPanel({tenantId,policy,revision,nodes,canManage,available,onChange}:{tenantId:string;policy:PatchReviewOperationsPolicy;revision:number;nodes:NodeSummary[];canManage:boolean;available:boolean;onChange:(value:PatchReviewOperationsPolicy)=>void}){
 const api=useApiClient();const [runs,setRuns]=useState<PatchAutomationRun[]>([]);const [approvals,setApprovals]=useState<PatchApproval[]>([]);const [error,setError]=useState('');const [busy,setBusy]=useState(false);const [checked,setChecked]=useState<{at:number;rows:{node:string;ready:boolean;reason:string}[]}|null>(null);const [choice,setChoice]=useState('');const [at,setAt]=useState('');const [confirm,setConfirm]=useState(false);
 const a=policy.automation;const signature=JSON.stringify([a.node_ids,a.mode,a.health_profile,a.window_id,a.observation_minutes]);
 useEffect(()=>{setChecked(null);},[signature]);
 useEffect(()=>{let cancelled=false;Promise.all([api.getPatchAutomationRuns(tenantId),api.listPatchApprovals({tenantId,status:'pending',limit:100})]).then(([r,p])=>{if(!cancelled){setRuns(r.runs);setApprovals(p.data);}}).catch(()=>{if(!cancelled)setError('Scheduled execution evidence unavailable; refresh before acting.');});const timer=setInterval(()=>setChecked(old=>old&&Date.now()-old.at<60000?old:null),30000);return()=>{cancelled=true;clearInterval(timer);};},[api,tenantId]);
 const run=async(action:()=>Promise<void>)=>{setBusy(true);setError('');try{await action();}catch(e){setError(e instanceof Error?e.message:'Automation operation failed');}finally{setBusy(false);}};
 const refresh=async()=>{const r=await api.getPatchAutomationRuns(tenantId);setRuns(r.runs);};
 const ready=!!checked&&checked.rows.length===a.node_ids.length&&a.node_ids.length>0&&checked.rows.every(r=>r.ready&&a.node_ids.includes(r.node))&&Date.now()-checked.at<60000;
 return <section aria-label="Scheduled installation qualification" className="space-y-3 rounded border border-border p-3"><h3>Scheduled and automatic execution checks</h3>
 <p>Supported execution is an immutable Ubuntu APT direct/proxy plan. Complete fresh package, service, consumer, reboot and assessment evidence is required. Docker health marked unknown does not pass. Readiness checks install nothing.</p>
 {error&&<p role="alert">{error}</p>}
 <Button disabled={!canManage||busy||!a.node_ids.length} onClick={()=>void run(async()=>{const rows=await Promise.all(a.node_ids.map(async node=>({node,...await api.checkPatchAutomationReadiness(tenantId,node)})));setChecked({at:Date.now(),rows});})}>Test selected targets for automation readiness</Button>
 {checked&&<ul>{checked.rows.map(row=><li key={row.node}>{nodes.find(n=>n.id===row.node)?.hostname??row.node}: {row.ready?'Host observations current':'Blocked'} — {row.reason}</li>)}</ul>}
 {a.mode!=='recommend_only'&&<>
 <p>Required execution profile: <code>ubuntu-apt-service-reboot-v1</code>. Naming this profile does not produce or qualify observations.</p>
 <Button disabled={!canManage||busy} onClick={()=>onChange({...policy,automation:{...a,health_profile:'ubuntu-apt-service-reboot-v1',observation_minutes:a.observation_minutes||60}})}>Use native service/reboot requirements</Button>
 <label className="block">Canary observation period (minutes)<input type="number" min={1} max={1440} disabled={!canManage||busy} className={field} value={a.observation_minutes??60} onChange={e=>onChange({...policy,automation:{...a,observation_minutes:Number(e.target.value)}})}/></label>
 <label className="block"><input type="checkbox" disabled={!canManage||busy||(!a.enabled&&(!ready||!available||a.health_profile!=='ubuntu-apt-service-reboot-v1'))} checked={a.enabled} onChange={e=>onChange({...policy,automation:{...a,enabled:e.target.checked}})}/>Enable scoped installation policy after readiness checks</label>
 <p>Save is still required. A policy change invalidates earlier schedules. Canary installation always requires individual approval; later automatic targets require the same resolved versions, fresh verified canary outcomes, observation time, bounded waves and a clear circuit breaker.</p>
 </>}
 <h4>Individually approve and schedule an exact request</h4><p>Latest 100 pending approvals are available here. The server rejects any selection outside the saved enabled policy. An exact plan expires after at most one hour; long-future windows require fresh preparation near the window. Saving a schedule authorizes that exact request, not later updates.</p>
 <label className="block">Pending exact approval<select className={field} disabled={!canManage||busy} value={choice} onChange={e=>setChoice(e.target.value)}><option value="">Select a pending exact request</option>{approvals.filter(p=>p.online_plan).map(p=><option key={p.id} value={p.id}>{p.package_allowlist?.join(', ')||'Scope unavailable'} · {p.node_id.slice(0,8)} · {p.id.slice(0,8)} · expires {p.expires_at}</option>)}</select></label>
 <label className="block">Execute no earlier than (UTC)<input className={field} type="datetime-local" disabled={!canManage||busy} value={at} onChange={e=>setAt(e.target.value)}/></label>
 <Button disabled={!canManage||busy||!choice||!at||!a.enabled} onClick={()=>setConfirm(true)}>Review exact schedule authorization</Button>
 <Button disabled={busy} onClick={()=>void run(refresh)}>Refresh scheduled requests</Button>
 <p>Latest 100 retained requests. Waiting or blocked is not an installed package. Failed, expired or cancelled requests are not silently replaced or replayed.</p>
 {runs.map(r=><article className="rounded border border-border p-2" key={r.id}><p>{r.approval_id.slice(0,8)} · {r.status} · authority {r.authority} · policy revision {r.policy_revision}</p><p>Not before {r.not_before} · expires {r.expires_at}</p><p>{r.reason}</p>{r.execution_job_id&&<p>Execution job {r.execution_job_id}</p>}{['waiting','blocked'].includes(r.status)&&<Button disabled={!canManage||busy} onClick={()=>void run(async()=>{await api.cancelPatchAutomation(tenantId,r.id);await refresh();})}>Cancel undispatched schedule {r.id.slice(0,8)}</Button>}</article>)}
 {confirm&&<ConfirmModal open title="Authorize this exact scheduled installation?" body={`Approval ${choice}; saved policy revision ${revision}; earliest ${at} UTC. This grants individual approval for this exact plan when all current gates pass. Changed or unknown evidence blocks execution.`} confirmLabel="Approve and schedule exact request" confirmDisabled={busy} cancelDisabled={busy} onCancel={()=>setConfirm(false)} onConfirm={()=>void run(async()=>{await api.schedulePatchAutomation(tenantId,choice,revision,new Date(at+'Z').toISOString());setConfirm(false);setChoice('');await refresh();})}>{error&&<p role="alert">{error}</p>}</ConfirmModal>}
 </section>;
}
