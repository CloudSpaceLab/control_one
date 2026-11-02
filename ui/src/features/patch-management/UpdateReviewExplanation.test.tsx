import {render,screen} from '@testing-library/react';
import {describe,it,expect} from 'vitest';
import type {UpdateCandidate} from '@/lib/api';
import {UpdateReviewExplanation} from './UpdateReviewExplanation';
const now=Date.parse('2026-10-30T14:00:00Z');
const review={state:'security_fix_review',summary:'Candidate meets selected fix boundaries.',evaluated_at:'2026-10-30T13:59:00Z',evidence_valid_until:'2026-10-30T14:10:00Z',findings_addressed:['CVE-DEMO'],dependencies:'Dependency scope unknown',reboot:'Reboot impact unknown',health:'Verify service probes after installation',urgency:'Not assigned by tenant policy',required_steps:['Review exact scope','Obtain approval']};
const record={name:'demo',review,repository_observation:{observed_at:'2026-10-30T13:55:00Z'}} as UpdateCandidate;
describe('explainable update review',()=>{
 it('shows rationale, age, unknown impacts and required approval without authorizing installation',()=>{
  render(<UpdateReviewExplanation record={record} now={now}/>);
  expect(screen.getByText('Security fix ready for review')).toBeInTheDocument();expect(screen.getByText(/5 minutes old/)).toBeInTheDocument();expect(screen.getByText(/CVE-DEMO/)).toBeInTheDocument();expect(screen.getByText('Dependency scope unknown')).toBeInTheDocument();expect(screen.getByText(/does not create an approval/)).toBeInTheDocument();expect(screen.queryByRole('button')).not.toBeInTheDocument();
 });
 it('withdraws a saved positive explanation after its validity expires',()=>{
  render(<UpdateReviewExplanation record={record} now={now+11*60_000}/>);
  expect(screen.getByText(/Review evidence expired/)).toBeInTheDocument();expect(screen.queryByText('Candidate meets selected fix boundaries.')).not.toBeInTheDocument();expect(screen.queryByText(/CVE-DEMO/)).not.toBeInTheDocument();
 });
 it('does not invent an explanation for an older server',()=>{
  render(<UpdateReviewExplanation record={{...record,review:undefined}} now={now}/>);expect(screen.getByText(/Update review unavailable/)).toBeInTheDocument();
 });
 it('refuses a positive claim without a valid deadline',()=>{
  render(<UpdateReviewExplanation record={{...record,review:{...review,evidence_valid_until:undefined}}} now={now}/>);expect(screen.getByText(/Review evidence expired/)).toBeInTheDocument();
 });
});
