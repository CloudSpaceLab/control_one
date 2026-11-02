import {useEffect, useState} from 'react';
import type {OfflinePatchContent} from '@/lib/api';
import {formatTs} from '@/lib/format';
import {requireCurrentOfflinePlan} from '@/lib/offlinePatchContent';

export function OfflineContentReview({content}: {content:OfflinePatchContent}): JSX.Element {
  const [now,setNow]=useState(Date.now);
  useEffect(()=>{const timer=setInterval(()=>setNow(Date.now()),1000);return ()=>clearInterval(timer);},[]);
  let current=true;
  try {requireCurrentOfflinePlan(content,now);} catch {current=false;}
  return <section aria-label="Offline content review" className="space-y-2 rounded border border-border p-3 text-xs">
    <p className="font-semibold">Exact offline installation plan</p>
    <p>{content.suite} / {content.component} / {content.architecture}</p>
    {content.review ? <>
      <p role="status">{current?'Time-bounded plan':'Expired or not yet valid — prepare and review a fresh plan'}. Prepared {formatTs(content.review.prepared_at)}; expires {formatTs(content.review.expires_at)}.</p>
      <table className="w-full text-left"><caption className="text-left font-semibold">Exact requested and dependency changes</caption><thead><tr><th>Package</th><th>Current baseline</th><th>Proposed</th><th>Scope</th></tr></thead><tbody>{content.review.changes.map(p=><tr key={p.name}><td className="break-all">{p.name}</td><td className="break-all">{p.installed_version||'Not installed'}</td><td className="break-all">{p.proposed_version}{p.installed_version===p.proposed_version?' (reinstall)':''}</td><td>{p.role==='requested'?'Requested':'Required dependency'}</td></tr>)}</tbody></table>
    </> : <><p role="status">Legacy plan: preparation age, absolute expiry and before/after review unavailable. Prepare a new plan for time-bounded review.</p><p>Requested: {content.requested.map(p=>`${p.name} = ${p.version}`).join(', ')}</p><p className="font-semibold">Packages to install or reinstall, including dependencies</p><ul>{content.resolved.map(p=><li key={p.name} className="font-mono break-all">{p.name} = {p.version}</li>)}</ul></>}
    <p><strong>Reboot impact: unknown.</strong> Current machine reboot state does not predict the effect of this plan. Review package/vendor requirements and arrange a reboot window when needed.</p>
    <p><strong>Service impact: requires operator review.</strong> Define service probes and recovery steps before approval. Maintainer scripts may restart services; exact dependency resolution does not prove application compatibility.</p>
    <details><summary>Content and machine integrity checks</summary><p className="break-all">Manifest SHA-256: {content.manifest_sha256}</p><p className="break-all">Installed-state SHA-256: {content.installed_status_sha256}</p><p className="break-all">Signing-key SHA-256: {content.keyring_sha256}</p></details>
    <p>Approval applies to this exact staged content, dependency plan, review and machine baseline. A changed or expired format-3 plan requires preparation and a new approval request. Reinstalling an existing version does not prove vulnerability remediation.</p>
  </section>;
}
