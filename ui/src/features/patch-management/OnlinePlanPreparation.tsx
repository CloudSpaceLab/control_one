import {useEffect,useRef,useState} from 'react';
import type {ExactOnlinePlan,NodeSummary,OfflinePreparationStatus} from '@/lib/api';
import {currentOnlinePlan,hasOnlinePlanner} from '@/lib/onlinePatchPlan';
import {useApiClient} from '@/hooks/useApiClient';
import {Button} from '@/components/ui/button';

export function OnlinePlanPreparation({nodes,mode,onPrepared,onSelected}:{nodes:NodeSummary[];mode:'direct'|'proxy';onPrepared:()=>void;onSelected:(plan:ExactOnlinePlan|null)=>void}):JSX.Element{
  const client=useApiClient();const[nodeId,setNodeId]=useState('');const[packages,setPackages]=useState('');const[job,setJob]=useState<OfflinePreparationStatus>({status:'idle'});const[busy,setBusy]=useState(false);const[error,setError]=useState('');const[pollError,setPollError]=useState('');const[loaded,setLoaded]=useState(false);const handled=useRef('');const generation=useRef(0);
  const[now,setNow]=useState(Date.now);useEffect(()=>{const timer=setInterval(()=>setNow(Date.now()),1000);return()=>clearInterval(timer);},[]);
  useEffect(()=>{
    const counter=generation;const current=++counter.current;let cancelled=false;setJob({status:'idle'});setError('');setPollError('');setLoaded(false);handled.current='';
    if(!nodeId){setBusy(false);return;}setBusy(true);
    client.getOnlinePreparation(nodeId).then(result=>{if(!cancelled){setJob(result);setLoaded(true);}}).catch(err=>{if(!cancelled)setError(err instanceof Error?err.message:'Cannot load preparation status.');}).finally(()=>{if(!cancelled)setBusy(false);});
    return()=>{cancelled=true;if(counter.current===current)counter.current++;};
  },[client,nodeId,mode]);
  useEffect(()=>{
    if(job.status==='succeeded'&&job.job_id&&handled.current!==job.job_id){handled.current=job.job_id;onPrepared();}
    if(!nodeId||!job.job_id||!['running','queued'].includes(job.status))return;
    let cancelled=false;const timer=setInterval(()=>{client.getOnlinePreparation(nodeId,job.job_id).then(result=>{if(!cancelled){setJob(result);setPollError('');}}).catch(err=>{if(!cancelled)setPollError(err instanceof Error?err.message:'Result temporarily unavailable.');});},3000);
    return()=>{cancelled=true;clearInterval(timer);};
  },[client,nodeId,job.job_id,job.status,onPrepared]);
  const prepare=async()=>{
    const scope=[...new Set(packages.split(/[\s,]+/).filter(Boolean))].sort();
    if(!scope.length||scope.length>100||scope.some(name=>!/^[a-z0-9][a-z0-9+.-]*(?::[a-z0-9_-]+)?$/.test(name))){setError('Select 1–100 installed package names, for example liblzma5.');return;}
    const current=generation.current;setBusy(true);setError('');setPollError('');onSelected(null);
    try{const result=await client.prepareOnlinePlan(nodeId,mode,scope);if(current===generation.current)setJob(result);}catch(err){if(current===generation.current)setError(err instanceof Error?err.message:'Preparation failed. Refresh status before retrying.');}finally{if(current===generation.current)setBusy(false);}
  };
  const target=nodes.find(node=>node.id===nodeId);const plan=target&&currentOnlinePlan(target,mode,now);const field='mt-1 w-full rounded border border-border bg-surface-1 px-2 py-1 text-sm';
  return <section aria-label="Prepare exact online plan" className="space-y-3 rounded border border-border p-3">
    <p className="text-sm font-medium">Prepare exact {mode} update plan</p><p className="text-xs text-text-secondary">Supported Ubuntu APT agents resolve the selected installed packages and acquire authenticated content first. Preparation does not install or authorize changes. Proxy mode uses the proxy saved in that machine&apos;s patch settings.</p>
    <label className="block text-sm">Online preparation target<select aria-label="Online preparation target" className={field} disabled={busy} value={nodeId} onChange={e=>{setNodeId(e.target.value);onSelected(null);}}><option value="">Select a compatible machine</option>{nodes.filter(node=>hasOnlinePlanner(node,now)).map(node=><option key={node.id} value={node.id}>{node.hostname} ({node.id.slice(0,8)})</option>)}</select></label>
    {nodeId&&<><label className="block text-sm">Installed package names<input className={field} value={packages} placeholder="liblzma5" onChange={e=>setPackages(e.target.value)}/></label><Button size="sm" disabled={busy||!loaded||job.status==='running'||job.status==='queued'} onClick={prepare}>Prepare exact online plan</Button><Button size="sm" variant="outline" disabled={busy} onClick={()=>{setError('');setBusy(true);client.getOnlinePreparation(nodeId).then(result=>{setJob(result);setLoaded(true);}).catch(err=>setError(err instanceof Error?err.message:'Cannot refresh status.')).finally(()=>setBusy(false));}}>Refresh online preparation</Button></>}
    {job.status!=='idle'&&<p role="status" className="text-xs">Preparation: {job.status}. {job.message} {job.job_id&&`Request ${job.job_id.slice(0,8)}.`}</p>}
    {plan&&<><p className="text-xs">Available prepared scope: {plan.requested.join(', ')}.</p><Button size="sm" variant="outline" disabled={busy||job.status==='running'||job.status==='queued'} onClick={()=>onSelected(plan)}>Use prepared {mode} plan for {target?.hostname}</Button></>}
    {nodeId&&!plan&&<p className="text-xs">No current published {mode} plan is available for this target. Refresh after successful preparation.</p>}
    {error&&<p role="alert" className="text-xs text-destructive">{error}</p>}{pollError&&<p role="alert" className="text-xs text-destructive">Preparation result unavailable: {pollError} Retrying automatically.</p>}
  </section>;
}
