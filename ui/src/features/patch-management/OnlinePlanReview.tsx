import {useEffect,useState} from 'react';
import type {ExactOnlinePlan} from '@/lib/api';
import {requireCurrentOnlinePlan} from '@/lib/onlinePatchPlan';
import {formatTs} from '@/lib/format';

export function OnlinePlanReview({plan}:{plan:ExactOnlinePlan}):JSX.Element{
  const[now,setNow]=useState(Date.now);useEffect(()=>{const timer=setInterval(()=>setNow(Date.now()),1000);return()=>clearInterval(timer);},[]);
  let valid=true;try{requireCurrentOnlinePlan(plan,now);}catch{valid=false;}
  return <section aria-label="Exact online plan review" className="space-y-2 rounded border border-border p-3 text-xs">
    <p className="font-semibold">Exact {plan.mode} update plan</p>
    <p role="status">{valid?'Time-bounded acquired plan':'Expired or invalid plan — prepare and approve again'}. Prepared {formatTs(plan.review.prepared_at)}; expires {formatTs(plan.review.expires_at)}.</p>
    {plan.mode==='proxy'&&<p>Approved repository hosts: {plan.repository_hosts?.join(', ')||'Unavailable'}.</p>}
    <p>Requested packages: {plan.requested.join(', ')}. Packages are acquired before approval using the reviewed {plan.mode==='proxy'?'repository proxy':'direct repository route'}. Installation uses this acquired content without choosing newer versions or downloading replacements.</p>
    {plan.review.changes.length?<table className="w-full text-left"><caption className="text-left font-semibold">Exact requested and dependency operations</caption><thead><tr><th>Package</th><th>Current baseline</th><th>Proposed</th><th>Scope</th></tr></thead><tbody>{plan.review.changes.map(p=><tr key={p.name}><td className="break-all">{p.name}</td><td className="break-all">{p.installed_version||'Not installed'}</td><td className="break-all">{p.proposed_version}</td><td>{p.role==='requested'?'Requested':'Required dependency'}</td></tr>)}</tbody></table>:<p>No package changes were resolved. A successful no-op does not establish a security fix.</p>}
    <p><strong>Reboot impact: unknown.</strong> Review vendor requirements and arrange a window when needed. Current machine reboot state does not predict this plan&apos;s impact.</p>
    <p><strong>Service impact: requires operator review.</strong> Define application probes and recovery steps before approval. Installation success alone does not verify service health or remediation.</p>
    <details><summary>Plan identity and baseline checks</summary><p className="break-all">Plan SHA-256: {plan.sha256}</p><p className="break-all">Installed-state SHA-256: {plan.installed_state_sha256}</p><p className="break-all">Sources/preferences/trust SHA-256: {plan.inputs_sha256}</p><p className="break-all">Acquired content SHA-256: {plan.manifest_sha256}</p><p className="break-all">Transport configuration SHA-256: {plan.transport_context}</p></details>
    <p>Approval covers one machine, this exact scope, transport configuration and expiry. Changed baseline, content, sources, plan selection or proxy settings require a fresh preparation and approval.</p>
  </section>;
}
