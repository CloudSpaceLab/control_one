import type {ExactOnlinePlan,NodeSummary} from './api';

export function hasOnlinePlanner(node:NodeSummary,now=Date.now()):boolean{
  const runtime=node.labels?.['patch.runtime'] as {online_plan_version?:number;os?:string;manager?:string;observed_at?:string}|undefined;
  const observed=Date.parse(runtime?.observed_at??'');
  return node.state==='active'&&runtime?.online_plan_version===1&&runtime.os==='linux'&&runtime.manager==='apt'&&Number.isFinite(observed)&&observed<=now&&now-observed<=300000;
}

export function currentOnlinePlan(node:NodeSummary,mode:'direct'|'proxy',now=Date.now()):ExactOnlinePlan|null{
  const raw=node.labels?.['patch.online_plan'] as {status?:string;plan?:ExactOnlinePlan;job_id?:string;observed_at?:string}|undefined;
  const p=raw?.plan;const observed=Date.parse(raw?.observed_at??'');
  if(!hasOnlinePlanner(node,now)||raw?.status!=='ready'||!raw.job_id||!Number.isFinite(observed)||observed>now||now-observed>300000||!p||p.version!==1||p.node_id!==node.id||p.mode!==mode)return null;
  try{requireCurrentOnlinePlan(p,now);}catch{return null;}
  return p;
}

export function requireCurrentOnlinePlan(p:ExactOnlinePlan,now=Date.now()):void{
  const validHash=(s:unknown)=>typeof s==='string'&&/^[a-f0-9]{64}$/.test(s);
  const prepared=Date.parse(p.review?.prepared_at??'');const expires=Date.parse(p.review?.expires_at??'');
  if(p.version!==1||!validHash(p.sha256)||!validHash(p.manifest_sha256)||!validHash(p.installed_state_sha256)||!validHash(p.inputs_sha256)||!validHash(p.transport_context)||!Number.isFinite(prepared)||!Number.isFinite(expires)||prepared>now||expires<=now||expires<=prepared||expires-prepared>3600000)throw new Error('Exact online plan expired or is invalid. Prepare and review a fresh plan.');
  if(!Array.isArray(p.requested)||!p.requested.length||p.requested.length>100||!Array.isArray(p.resolved)||p.resolved.length>100||!Array.isArray(p.review.changes)||p.review.changes.length!==p.resolved.length||p.review.reboot!=='unknown'||p.review.service_impact!=='requires_operator_review')throw new Error('Exact online plan scope is unavailable. Prepare a new plan.');
  if(!['direct','proxy'].includes(p.mode)||typeof p.node_id!=='string'||!/^[a-f0-9-]{36}$/.test(p.node_id)||p.requested.some((name,i)=>typeof name!=='string'||!/^[a-z0-9][a-z0-9+.-]*(?::[a-z0-9_-]+)?$/.test(name)||(i>0&&p.requested[i-1]>=name)))throw new Error('Exact online target or package scope is invalid.');
  p.resolved.forEach((pkg,i)=>{const c=p.review.changes[i];if(!c||c.name!==pkg.name||c.proposed_version!==pkg.version||typeof c.installed_version!=='string'||!['requested','dependency'].includes(c.role))throw new Error('Exact online plan operations and review differ.');});
}

export function onlinePlanForReview(raw:unknown):ExactOnlinePlan|null{
 if(!raw||typeof raw!=='object'||Array.isArray(raw))return null;
 const p=raw as ExactOnlinePlan;
 try{requireCurrentOnlinePlan(p,Date.parse(p.review?.prepared_at??''));return p;}catch{return null;}
}
