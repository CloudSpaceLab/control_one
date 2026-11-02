import {describe,expect,it} from 'vitest';
import type {ExactOnlinePlan} from './api';
import {currentOnlinePlan,hasOnlinePlanner,onlinePlanForReview,requireCurrentOnlinePlan} from './onlinePatchPlan';

import {onlineFixture,onlineNode} from './onlinePatchPlan.testFixtures';

describe('current exact online evidence',()=>{
  it('requires current capability, matching target/mode, successful publication and unexpired identity',()=>{
    const now=Date.now();const node=onlineNode(now);expect(hasOnlinePlanner(node,now)).toBe(true);expect(currentOnlinePlan(node,'direct',now)?.sha256).toBe('a'.repeat(64));expect(currentOnlinePlan(node,'proxy',now)).toBeNull();
    expect(currentOnlinePlan(node,'direct',now+300001)).toBeNull();expect(hasOnlinePlanner(node,now-1)).toBe(false);
    const raw=node.labels!['patch.online_plan'] as {status:string;plan:ExactOnlinePlan};raw.status='unavailable';expect(currentOnlinePlan(node,'direct',now)).toBeNull();raw.status='ready';raw.plan.node_id='another-node';expect(currentOnlinePlan(node,'direct',now)).toBeNull();
  });
  it.each(['expiry','missing review','extra operation','wrong version','invalid hash'])('rejects %s without enabling a deployment',scenario=>{
    const now=Date.now();const p=onlineFixture(now);
    if(scenario==='expiry')p.review.expires_at=new Date(now).toISOString();
    if(scenario==='missing review')delete (p as Partial<ExactOnlinePlan>).review;
    if(scenario==='extra operation')p.resolved.push({name:'other',version:'1'});
    if(scenario==='wrong version')p.review.changes[0].proposed_version='9';
    if(scenario==='invalid hash')p.sha256='invalid';
    expect(()=>requireCurrentOnlinePlan(p,now)).toThrow();
  });
  it('keeps valid expired history viewable but never current',()=>{
    const now=Date.now();const p=onlineFixture(now-7200000);expect(onlinePlanForReview(p)).toEqual(p);expect(()=>requireCurrentOnlinePlan(p,now)).toThrow('expired');expect(onlinePlanForReview({review:null})).toBeNull();
  });
});
