import { useEffect, useRef, useState } from 'react';
import { useApiClient } from '../../hooks/useApiClient';
import type { ApprovedOfflineRepository, NodeSummary, OfflinePreparationStatus } from '../../lib/api';
import { Button } from '../../components/ui/button';

const emptyRepository = (): ApprovedOfflineRepository => ({id:'', name:'', keyring_sha256:'', suite:'noble-updates', component:'main', enabled:true});

export function OfflinePlanPreparation({nodes, onPrepared}: {nodes: NodeSummary[]; onPrepared: () => void}): JSX.Element {
  const client = useApiClient();
  const [nodeId,setNodeId] = useState('');
  const [repository,setRepository] = useState<ApprovedOfflineRepository | null>(null);
  const [draft,setDraft] = useState(emptyRepository);
  const [path,setPath] = useState('');
  const [packages,setPackages] = useState('');
  const [job,setJob] = useState<OfflinePreparationStatus>({status:'idle'});
  const [busy,setBusy] = useState(false);
  const [error,setError] = useState('');
  const [pollError,setPollError] = useState('');
  const handled = useRef('');
  const field = 'mt-1 w-full rounded border border-border bg-surface-1 px-2 py-1 text-sm';

  useEffect(() => {
    let cancelled = false;
    setRepository(null); setDraft(emptyRepository()); setPath(''); setJob({status:'idle'}); setError(''); setPollError(''); handled.current='';
    if (!nodeId) return;
    setBusy(true);
    Promise.all([client.getOfflineRepository(nodeId),client.getOfflinePreparation(nodeId)]).then(([repo,status]) => {
      if(cancelled) return;
      setRepository(repo.repository);setDraft(repo.repository ?? emptyRepository());setPath(repo.repository_path ?? '');setJob(status);
    }).catch(err => {if(!cancelled)setError(err instanceof Error?err.message:'Cannot load offline preparation settings.');})
      .finally(()=>{if(!cancelled)setBusy(false);});
    return ()=>{cancelled=true;};
  },[client,nodeId]);

  useEffect(()=>{
    if(job.status==='succeeded' && job.job_id && handled.current!==job.job_id){handled.current=job.job_id;onPrepared();}
    if(!job.job_id || (job.status!=='running' && job.status!=='queued'))return;
    let cancelled=false;
    const timer=window.setInterval(()=>{client.getOfflinePreparation(nodeId,job.job_id).then(status=>{if(!cancelled){setJob(status);setPollError('');}}).catch(err=>{if(!cancelled)setPollError(err instanceof Error?err.message:'Cannot refresh preparation result.');});},3000);
    return()=>{cancelled=true;window.clearInterval(timer);};
  },[client,nodeId,job.job_id,job.status,onPrepared]);

  const save = async () => {
    setBusy(true);setError('');
    try {const response=await client.saveOfflineRepository(nodeId,draft);setRepository(response.repository);setDraft(response.repository);setPath(response.repository_path);}
    catch(err){setError(err instanceof Error?err.message:'Repository registration failed.');}
    finally{setBusy(false);}
  };

  const prepare = async () => {
    if(!repository?.enabled)return;
    const selected=packages.split(/[\s,]+/).filter(Boolean).map(raw=>{const index=raw.indexOf('=');return {name:raw.slice(0,index),version:index<0?'':raw.slice(index+1)};});
    if(!selected.length || selected.some(pkg=>!pkg.name || !pkg.version)){setError('Enter exact selections such as libudev1=255.4-1ubuntu8.18.');return;}
    setBusy(true);setError('');setPollError('');
    try {setJob(await client.prepareOfflinePlan(nodeId,repository.id,selected));}
    catch(err){setError(err instanceof Error?err.message:'Preparation failed.');}
    finally{setBusy(false);}
  };

  return <details className="rounded border border-border p-3">
    <summary className="cursor-pointer text-sm font-medium">Prepare a new offline plan</summary>
    <div className="mt-3 space-y-3">
      <p className="text-xs text-text-secondary">Ask the enrolled agent to validate approved local content and prepare a plan. This does not install packages or grant installation approval.</p>
      <label className="block text-sm">Preparation target<select aria-label="Preparation target" className={field} value={nodeId} disabled={busy} onChange={e=>setNodeId(e.target.value)}>
        <option value="">Select a machine</option>{nodes.map(node=><option key={node.id} value={node.id}>{node.hostname} ({node.id.slice(0,8)})</option>)}
      </select></label>
      {nodeId && <>
        <p className="text-sm">Approved repository: {repository ? `${repository.name}${repository.enabled?'':' (disabled)'}` : 'Not registered'}</p>
        {path && <p className="break-all text-xs text-text-secondary">Content destination on this machine: <code>{path}</code>. An administrator must transfer the signed repository and required package archives here first.</p>}
        <details className="rounded border border-border p-2"><summary className="cursor-pointer text-xs">Administrator: register approved repository</summary>
          <div className="mt-2 space-y-2">
            <p className="text-xs text-text-secondary">Administrator permission required. Use the trusted Ubuntu archive keyring hash approved for this environment. Registration does not download content.</p>
            <label className="block text-sm">Repository name<input className={field} value={draft.name} onChange={e=>setDraft({...draft,name:e.target.value})}/></label>
            <label className="block text-sm">Trusted keyring SHA-256<input className={field} value={draft.keyring_sha256} onChange={e=>setDraft({...draft,keyring_sha256:e.target.value.trim()})}/></label>
            <label className="block text-sm">Repository suite<input className={field} value={draft.suite} onChange={e=>setDraft({...draft,suite:e.target.value.trim()})}/></label>
            <label className="block text-sm">Repository component<input className={field} value={draft.component} onChange={e=>setDraft({...draft,component:e.target.value.trim()})}/></label>
            <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={draft.enabled} onChange={e=>setDraft({...draft,enabled:e.target.checked})}/>Allow preparation from this repository</label>
            <Button size="sm" variant="outline" disabled={busy} onClick={save}>Save approved repository</Button>
          </div>
        </details>
        <label className="block text-sm">Exact packages to prepare<textarea className={field} placeholder="libudev1=255.4-1ubuntu8.18" value={packages} onChange={e=>setPackages(e.target.value)}/></label>
        <Button size="sm" disabled={busy || !repository?.enabled || job.status==='running' || job.status==='queued'} onClick={prepare}>Prepare offline plan</Button>
        {job.status!=='idle' && <div role="status" className="text-xs">Preparation: {job.status}. {job.message} {job.job_id && <span>Request {job.job_id.slice(0,8)}.</span>}</div>}
      </>}
      {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
      {pollError && <p role="alert" className="text-xs text-destructive">Cannot refresh preparation status: {pollError} Retrying automatically.</p>}
    </div>
  </details>;
}
