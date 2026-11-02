import {act, render, screen} from '@testing-library/react';
import {afterEach, expect, it, vi} from 'vitest';
import type {OfflinePatchContent} from '@/lib/api';
import {OfflineContentReview} from './OfflineContentReview';

const plan=():OfflinePatchContent=>({format:3,manifest_sha256:'a'.repeat(64),installed_status_sha256:'b'.repeat(64),keyring_sha256:'c'.repeat(64),suite:'noble-updates',component:'main',architecture:'amd64',requested:[{name:'liba',version:'2'}],resolved:[{name:'liba',version:'2'},{name:'libb',version:'3'}],review:{prepared_at:'2026-10-30T10:00:00Z',expires_at:'2026-10-30T11:00:00Z',reboot:'unknown',service_impact:'requires_operator_review',changes:[{name:'liba',installed_version:'1',proposed_version:'2',role:'requested'},{name:'libb',installed_version:'3',proposed_version:'3',role:'dependency'}]}});
afterEach(()=>vi.useRealTimers());
it('shows exact baseline, requested/dependency scope and unknown impact',()=>{
  vi.useFakeTimers();vi.setSystemTime(new Date('2026-10-30T10:59:59Z'));
  render(<OfflineContentReview content={plan()}/>);
  expect(screen.getByRole('table')).toHaveTextContent('liba');
  expect(screen.getByRole('table')).toHaveTextContent('Required dependency');
  expect(screen.getByRole('table')).toHaveTextContent('3 (reinstall)');
  expect(screen.getByText('Reboot impact: unknown.')).toBeVisible();
  expect(screen.getByText('Service impact: requires operator review.')).toBeVisible();
  expect(screen.getByRole('status')).toHaveTextContent('Time-bounded plan');
  act(()=>vi.advanceTimersByTime(1000));
  expect(screen.getByRole('status')).toHaveTextContent('Expired or not yet valid');
});
it('does not invent review or expiry for legacy plans',()=>{
  const legacy=plan();legacy.format=2;delete legacy.review;
  render(<OfflineContentReview content={legacy}/>);
  expect(screen.getByRole('status')).toHaveTextContent('Legacy plan');
  expect(screen.queryByRole('table')).not.toBeInTheDocument();
});
