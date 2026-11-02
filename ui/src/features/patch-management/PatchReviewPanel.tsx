import { useEffect, useRef, useState } from 'react';
import { useApiClient } from '@/hooks/useApiClient';
import { Button } from '@/components/ui/button';
import type { PatchFindingTracking, PatchPriorityDecision, PatchPriorityInput, PatchReviewFinding, PatchReviewPolicy, PatchReviewResponse } from './reviewTypes';
const emptyPolicy: PatchReviewPolicy = { name: '', owner: '', mode: 'recommend_only', retention_days: 30, rules: [] };
const field = 'w-full rounded border border-border bg-surface-1 p-2 text-sm';
const labExample: PatchReviewPolicy = { name: 'Local Development example — review only', owner: 'Local Development operator', mode: 'recommend_only', retention_days: 30, rules: [{ name: 'Known exploited and exposed', minimum_severity: 'high', require_kev: true, exposure: 'exposed', importance: 'any', priority: 'urgent', due_hours: 24 }, { name: 'Critical severity review', minimum_severity: 'critical', require_kev: false, exposure: 'any', importance: 'any', priority: 'high', due_hours: 72 }, { name: 'Other supported findings', minimum_severity: 'info', require_kev: false, exposure: 'any', importance: 'any', priority: 'normal', due_hours: 336 }] };
export function PatchReviewPanel({ tenantId }: {
    tenantId: string;
}): JSX.Element { return <ReviewWorkspace key={tenantId} tenantId={tenantId}/>; }
function ReviewWorkspace({ tenantId }: {
    tenantId: string;
}): JSX.Element {
    const api = useApiClient();
    const alive = useRef(true);
    const [data, setData] = useState<PatchReviewResponse | null>(null);
    const [policy, setPolicy] = useState<PatchReviewPolicy>(emptyPolicy);
    const [revision, setRevision] = useState(0);
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    const [message, setMessage] = useState('');
    const [simulation, setSimulation] = useState<PatchPriorityDecision | null>(null);
    const [input, setInput] = useState<PatchPriorityInput>({ severity: 'critical', kev: true, exposure: 'exposed', importance: 'critical', compatibility: 'unknown', evidence_current: true, first_seen_at: new Date().toISOString() });
    const [selected, setSelected] = useState<PatchReviewFinding | null>(null);
    const pageRef=useRef({cursor:'',historyCursor:0,limit:25});
    const [pageSize,setPageSize]=useState(25);
    const [previousPages,setPreviousPages]=useState<string[]>([]);
    const [previousHistory,setPreviousHistory]=useState<number[]>([]);
    const navigate=async(cursor:string,historyCursor:number,limit=pageRef.current.limit)=>{const result=await api.getPatchReviews(tenantId,false,{cursor,historyCursor,limit});if(alive.current){pageRef.current={cursor,historyCursor,limit};setData(result);setSelected(null);}};
    useEffect(()=>{
        let cancelled=false;
        const timer=setInterval(()=>{const page=pageRef.current;api.getPatchReviews(tenantId,false,page).then(result=>{if(!cancelled&&page===pageRef.current)setData(result);}).catch(()=>{if(!cancelled){setData(null);setError('Current review evidence is unavailable. Refresh before acting.');}});},60000);
        return()=>{cancelled=true;clearInterval(timer);};
    },[api,tenantId]);
    useEffect(() => { alive.current = true; api.getPatchReviews(tenantId).then(result => { if (alive.current) {
        setData(result);
        setPolicy(result.policy?.policy ?? emptyPolicy);
        setRevision(result.policy?.revision ?? 0);
    } }).catch(err => { if (alive.current)
        setError(String(err.message ?? err)); }); return () => { alive.current = false; }; }, [api, tenantId]);
    const run = async (action: () => Promise<void>) => { setBusy(true); setError(''); setMessage(''); try {
        await action();
    }
    catch (err) {
        if (alive.current)
            setError(err instanceof Error ? err.message : 'Review operation failed. Refresh before retrying.');
    }
    finally {
        if (alive.current)
            setBusy(false);
    } };
    const reload = async () => { const result = await api.getPatchReviews(tenantId,false,pageRef.current); if (alive.current) {
        setData(result);
        setPolicy(result.policy?.policy ?? emptyPolicy);
        setRevision(result.policy?.revision ?? 0);
        setSimulation(null);
    } };
    const save = () => run(async () => { const saved = await api.savePatchReviewPolicy(tenantId, revision, policy); if (alive.current) {
        setPolicy(saved.policy);
        setRevision(saved.revision);
        setSimulation(null);
        setMessage(`Policy revision ${saved.revision} saved. No installation authorized.`);
        await reload();
    } });
    const exportEvidence = () => run(async () => { const evidence = await api.downloadPatchReviewEvidence(tenantId); if (!alive.current)
        return; const url = URL.createObjectURL(evidence.blob); const link = document.createElement('a'); link.href = url; link.download = evidence.filename??'controlone-patch-review.json'; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000); setMessage(`Complete database snapshot exported. Archive ID: ${evidence.archiveId??'Unavailable'}; SHA-256: ${evidence.sha256??'Unavailable'}. Retain and verify this archive before a retention purge.`); });
    return <section aria-label="Patch review governance" className="space-y-5 rounded border border-border p-4">
  <h2 className="text-lg font-semibold">Review policy and ownership</h2><p className="text-sm">Recommendation only. These controls create no installation jobs, approvals or automatic promotion. Owners and escalation destinations identify accountable teams; they do not grant access or send messages.</p>
  {error && <p role="alert" className="text-destructive">{error}</p>}{message && <p role="status">{message}</p>}
  <div className="flex gap-2"><Button disabled={busy} onClick={() => void run(reload)}>Refresh review evidence</Button><Button disabled={busy || !data} onClick={() => void exportEvidence()}>Export review evidence</Button></div>
  {!data ? <p>Review evidence unavailable until loading succeeds.</p> : <>
  <p>Saved policy revision: {revision || 'Not configured'}. {data.total_findings} active known findings. Coverage is partial; zero findings does not prove clean.</p><p className="text-xs">Evidence evaluated {data.generated_at}. Queue expiry and overdue status refresh every minute. Policy edits require an explicit save; concurrent changes require reloading.</p>
  <details open><summary className="font-semibold">Tenant urgency policy (administrator)</summary>
   <p className="text-xs">Rules match from top to bottom. Deadlines run from first finding observation. No universal priority weights or deadlines are assumed.</p>
   <label className="block">Policy name<input className={field} value={policy.name} onChange={e => setPolicy({ ...policy, name: e.target.value })}/></label>
   <label className="block">Accountable policy owner/team<input className={field} value={policy.owner} onChange={e => setPolicy({ ...policy, owner: e.target.value })}/></label>
   <label className="block">History visibility window (days)<input type="number" min={1} max={3650} className={field} value={policy.retention_days} onChange={e => setPolicy({ ...policy, retention_days: Number(e.target.value) })}/></label>
   <p className="text-xs">{data.retention}</p>
   {policy.rules.map((rule, index) => <fieldset key={index} className="my-3 space-y-2 rounded border border-border p-3"><legend>Rule {index + 1}</legend>
    <label>Rule name<input className={field} value={rule.name} onChange={e => setPolicy({ ...policy, rules: policy.rules.map((r, i) => i === index ? { ...r, name: e.target.value } : r) })}/></label>
    {(['minimum_severity', 'exposure', 'importance', 'priority'] as const).map(key => <label key={key} className="block">{({ minimum_severity: 'Minimum severity', exposure: 'Exposure', importance: 'Asset importance', priority: 'Priority' })[key]}<select className={field} value={rule[key]} onChange={e => setPolicy({ ...policy, rules: policy.rules.map((r, i) => i === index ? { ...r, [key]: e.target.value } : r) })}>{({ minimum_severity: ['info', 'low', 'medium', 'high', 'critical'], exposure: ['any', 'exposed', 'internal'], importance: ['any', 'critical', 'standard'], priority: ['urgent', 'high', 'normal', 'low'] })[key].map(v => <option key={v}>{v}</option>)}</select></label>)}
    <label className="block"><input type="checkbox" checked={rule.require_kev} onChange={e => setPolicy({ ...policy, rules: policy.rules.map((r, i) => i === index ? { ...r, require_kev: e.target.checked } : r) })}/> Require known exploitation</label>
    <label>Due within hours of first observation<input type="number" min={1} max={8760} className={field} value={rule.due_hours} onChange={e => setPolicy({ ...policy, rules: policy.rules.map((r, i) => i === index ? { ...r, due_hours: Number(e.target.value) } : r) })}/></label>
    <Button disabled={busy} onClick={() => setPolicy({ ...policy, rules: policy.rules.filter((_, i) => i !== index) })}>Remove rule {index + 1}</Button>
   </fieldset>)}
   <div className="flex flex-wrap gap-2"><Button disabled={busy || policy.rules.length >= 25} onClick={() => setPolicy({ ...policy, rules: [...policy.rules, { name: `Rule ${policy.rules.length + 1}`, minimum_severity: 'high', require_kev: false, exposure: 'any', importance: 'any', priority: 'high', due_hours: 72 }] })}>Add priority rule</Button><Button disabled={busy} onClick={() => { setPolicy(labExample); setSimulation(null); setMessage('Unsaved lab example loaded. These example deadlines are not a production policy.'); }}>Load lab example (unsaved)</Button><Button disabled={busy||!data.permissions.manage_policy} onClick={() => void save()}>Save recommendation policy</Button></div>
  </details>
  <details><summary className="font-semibold">Try a hypothetical priority scenario</summary><p className="text-xs">Simulation uses the saved tenant policy. Inputs here are hypothetical; they are never stored as findings or machine evidence.</p>
    {(['severity', 'exposure', 'importance', 'compatibility'] as const).map(key => <label key={key} className="block">Scenario {key}<select disabled={busy} className={field} value={input[key]} onChange={e => { setInput({ ...input, [key]: e.target.value }); setSimulation(null); }}>{({ severity: ['critical', 'high', 'medium', 'low', 'info', 'unknown'], exposure: ['unknown', 'exposed', 'internal'], importance: ['unknown', 'critical', 'standard'], compatibility: ['unknown', 'reviewed', 'blocked'] })[key].map(v => <option key={v}>{v}</option>)}</select></label>)}
   <label className="block"><input type="checkbox" disabled={busy} checked={input.kev} onChange={e => { setInput({ ...input, kev: e.target.checked }); setSimulation(null); }}/> Scenario known exploitation</label><label className="block"><input type="checkbox" disabled={busy} checked={input.evidence_current} onChange={e => { setInput({ ...input, evidence_current: e.target.checked }); setSimulation(null); }}/> Scenario evidence current</label>
   <Button disabled={busy || !revision} onClick={() => void run(async () => { const result = await api.simulatePatchPriority(tenantId, input); if (alive.current)
            setSimulation(result.decision); })}>Evaluate hypothetical scenario</Button>
   {simulation && <div role="status"><p>Hypothetical result: {simulation.priority}. Due: {simulation.due_at ?? 'Unavailable'}.</p><ul>{simulation.reasons.map(reason => <li key={reason}>{reason}</li>)}</ul></div>}
  </details>
  <h3 className="font-semibold">Finding review and escalation queue</h3><p>Overdue or unassigned work and expired exceptions require review. Escalation appears here. Optional in-app delivery is configured separately in operational controls.</p>
  <label>Records per page<select className={field} disabled={busy} value={pageSize} onChange={e=>{const size=Number(e.target.value);void run(async()=>{await navigate('',0,size);if(alive.current){setPageSize(size);setPreviousPages([]);setPreviousHistory([]);}});}}>{[1,25,50,100].map(size=><option key={size} value={size}>{size}</option>)}</select></label><p>Showing {data.findings.length} findings on this page of {data.total_findings} active known findings. Export includes the complete snapshot.</p>
  <div className="flex gap-2"><Button disabled={busy||!previousPages.length} onClick={()=>void run(async()=>{const previous=previousPages[previousPages.length-1];await navigate(previous,pageRef.current.historyCursor);if(alive.current)setPreviousPages(previousPages.slice(0,-1));})}>Previous findings</Button><Button disabled={busy||!data.pagination?.next_cursor} onClick={()=>void run(async()=>{const current=pageRef.current.cursor;await navigate(data.pagination!.next_cursor!,pageRef.current.historyCursor);if(alive.current)setPreviousPages([...previousPages,current]);})}>Next findings</Button></div>
  {!data.findings.length && <p>No active findings on this page. Assessment coverage remains separate.</p>}
  {data.findings.map(row => <article key={row.finding.id} className="space-y-2 rounded border border-border p-3"><h4>{row.finding.cve_id} · {row.finding.package_name} {row.finding.installed_version}</h4><p>Node {row.finding.node_id.slice(0, 8)} · priority {row.decision.priority} · owner {row.tracking?.tracking.owner ?? 'Unassigned'} · due {row.decision.due_at ?? 'Not set'}</p><p>Evidence: {row.evidence_current ? 'Current selected advisory' : 'Unavailable, stale or outside current coverage'} · exception {row.exception_status} · escalation {row.escalation_required ? 'Required' : 'Not currently required'}</p><ul>{row.decision.reasons.map(reason => <li key={reason}>{reason}</li>)}</ul><Button disabled={busy||!data.permissions.review_findings} onClick={() => setSelected(row)}>Review {row.finding.cve_id} on {row.finding.node_id.slice(0, 8)}</Button></article>)}
  {selected && <FindingEditor key={selected.finding.id} finding={selected} busy={busy} canException={data.permissions.manage_policy} onClose={() => setSelected(null)} onSave={tracking => void run(async () => { await api.savePatchFindingReview(tenantId, selected.finding.id, selected.tracking?.revision ?? 0, tracking); if (alive.current) {
            setSelected(null);
            setMessage('Finding review saved. No finding resolved or installation authorized.');
            await reload();
        } })}/>}
  <details><summary>Review history ({data.events.length} on this page)</summary>{data.events.map(event => <p key={event.id}>{event.created_at} · {event.kind} · revision {event.revision} · actor {event.actor}</p>)}<div className="flex gap-2"><Button disabled={busy||!previousHistory.length} onClick={()=>void run(async()=>{const previous=previousHistory[previousHistory.length-1];await navigate(pageRef.current.cursor,previous);if(alive.current)setPreviousHistory(previousHistory.slice(0,-1));})}>Newer review history</Button><Button disabled={busy||!data.pagination?.next_history_cursor} onClick={()=>void run(async()=>{const current=pageRef.current.historyCursor;await navigate(pageRef.current.cursor,data.pagination!.next_history_cursor);if(alive.current)setPreviousHistory([...previousHistory,current]);})}>Older review history</Button></div></details>
  <h3 className="font-semibold">Automation safety boundary</h3><p>Effective mode: {data.automation.effective_mode}. Scheduled and automatic execution require a separately enabled scoped policy, exact authorization and current health/canary gates. Existing manual approvals remain a separate workflow.</p><ul>{data.automation.stop_conditions.map(reason => <li key={reason}>{reason}</li>)}</ul>
  </>}
 </section>;
}
function FindingEditor({ finding, busy, canException, onClose, onSave }: {
    finding: PatchReviewFinding;
    busy: boolean;
    canException: boolean;
    onClose: () => void;
    onSave: (tracking: PatchFindingTracking) => void;
}): JSX.Element {
    const [value, setValue] = useState<PatchFindingTracking>(()=>{
        const saved=finding.tracking?.tracking;
        if(!saved)return {owner:'',escalate_to:'',exposure:'unknown',importance:'unknown',compatibility:'unknown',reason:''};
        const next={...saved};
        if(next.exception_until&&Date.parse(next.exception_until)<=Date.now())next.exception_until=undefined;
        if(next.context_expires_at&&Date.parse(next.context_expires_at)<=Date.now()){next.context_expires_at=undefined;next.exposure='unknown';next.importance='unknown';next.compatibility='unknown';}
        return next;
    });
    return <fieldset className="space-y-2 rounded border border-border p-4"><legend>Review {finding.finding.cve_id}</legend><p>Owner and escalation destination are accountable team labels, not access grants. An exception needs an administrator and expires within 90 days; it never hides or resolves the finding.</p>
 {(['owner', 'escalate_to', 'reason'] as const).map(key => <label key={key} className="block">{({ owner: 'Finding owner/team', escalate_to: 'Escalation destination', reason: 'Review reason' })[key]}<input className={field} value={value[key]} onChange={e => setValue({ ...value, [key]: e.target.value })}/></label>)}
 {(['exposure', 'importance', 'compatibility'] as const).map(key => <label key={key} className="block">Review {key}<select className={field} value={value[key]} onChange={e => setValue({ ...value, [key]: e.target.value })}>{({ exposure: ['unknown', 'exposed', 'internal'], importance: ['unknown', 'critical', 'standard'], compatibility: ['unknown', 'reviewed', 'blocked'] })[key].map(v => <option key={v}>{v}</option>)}</select></label>)}
 {(['context_expires_at','due_at', 'exception_until'] as const).map(key => <label key={key} className="block">{key==='context_expires_at'?'Reviewed context expiry (UTC)':key === 'due_at' ? 'Due date override (UTC)' : 'Exception expiry (UTC, administrator)'}<input type="datetime-local" disabled={key==='exception_until'&&!canException} className={field} value={value[key]?.slice(0, 16) ?? ''} onInput={e => {const raw=e.currentTarget.value;const parsed=raw?new Date(raw+'Z'):null;setValue({...value,[key]:parsed&&!Number.isNaN(parsed.getTime())?parsed.toISOString():undefined});}}/></label>)}
 <p>Expired exceptions and context stay in history. A new review starts without an expired exception and treats expired context as unknown; renew explicitly when appropriate.</p>
 <p role="status">Pending review dates: due {value.due_at??'Not set'}; exception {value.exception_until??'None'}; context valid until {value.context_expires_at??'Not set'}. All dates are UTC.</p>
 {!canException&&value.exception_until&&<p>An administrator must review changes while this active exception is included.</p>}
<Button disabled={busy||(!canException&&!!value.exception_until)} onClick={() => onSave(value)}>Save finding review</Button><Button disabled={busy} onClick={onClose}>Cancel finding review</Button></fieldset>;
}
