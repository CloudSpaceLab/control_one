import {act,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import {onlineFixture} from '@/lib/onlinePatchPlan.testFixtures';
import {OnlinePlanReview} from './OnlinePlanReview';

afterEach(()=>vi.useRealTimers());
it('shows exact dependency operations, required checks and expiry without authorizing execution',()=>{
  vi.useFakeTimers();const now=Date.now();vi.setSystemTime(now);const plan=onlineFixture(now);plan.review.expires_at=new Date(now+1000).toISOString();
  render(<OnlinePlanReview plan={plan}/>);expect(screen.getByRole('table')).toHaveTextContent('libb');expect(screen.getByRole('table')).toHaveTextContent('Required dependency');expect(screen.getByRole('table')).toHaveTextContent('Not installed');expect(screen.getByText('Reboot impact: unknown.')).toBeVisible();
  expect(screen.getByRole('status')).toHaveTextContent('Time-bounded acquired plan');act(()=>vi.advanceTimersByTime(1000));expect(screen.getByRole('status')).toHaveTextContent('Expired or invalid');
});
it('describes an empty operation list as a no-op, not remediation',()=>{
  const plan=onlineFixture();plan.resolved=[];plan.review.changes=[];render(<OnlinePlanReview plan={plan}/>);expect(screen.getByText(/No package changes were resolved/)).toBeVisible();expect(screen.queryByRole('table')).not.toBeInTheDocument();
});
