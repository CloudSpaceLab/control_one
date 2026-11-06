import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { beforeEach, expect, it, vi } from 'vitest';
import { Account } from './Account';
import { ProfileMenu } from '../components/shell/ProfileMenu';

const mocks = vi.hoisted(() => ({ auth: {
  profile: {name: 'Default Operator', email: 'operator@local', roles: ['operator', 'operator']} as {name: string; email: string; roles: string[]} | null,
  loading: false, error: null as string | null, refreshProfile: vi.fn().mockResolvedValue(undefined), signOut: vi.fn(),
} }));
vi.mock('../providers/AuthProvider', () => ({useAuth: () => mocks.auth}));
vi.mock('../hooks/useRolePick', () => ({useRolePick: () => ({role: 'operator'})}));
beforeEach(() => {
  vi.clearAllMocks();
  mocks.auth.profile = {name:'Default Operator', email:'operator@local', roles:['operator','operator']};
  mocks.auth.loading = false; mocks.auth.error = null;
});
it('opens the personal account from the actual Profile menu without visiting Users', async () => {
  const user = userEvent.setup();
  render(<MemoryRouter><ProfileMenu/><Routes><Route path="/" element={<p>Control Room</p>}/><Route path="/account" element={<Account/>}/><Route path="/users" element={<p>User administration</p>}/></Routes></MemoryRouter>);
  await user.click(screen.getByRole('button', {name:'Profile menu'}));
  await user.click(screen.getByRole('menuitem', {name:'Account'}));
  expect(await screen.findByRole('heading',{name:'My account'})).toBeVisible();
  expect(screen.queryByText('User administration')).not.toBeInTheDocument();
  expect(screen.getAllByText('operator@local')).toHaveLength(1);
  expect(screen.getByRole('list',{name:'Current account roles'}).children).toHaveLength(1);
  await user.click(screen.getByRole('button',{name:'Refresh account'}));
  await waitFor(()=>expect(mocks.auth.refreshProfile).toHaveBeenCalledTimes(1));
});
it('hides stale identity and roles during refresh or failure', () => {
  mocks.auth.loading = true;
  const {rerender} = render(<Account/>);
  expect(screen.getByRole('status')).toHaveTextContent('Checking current account');
  expect(screen.queryByText('operator@local')).not.toBeInTheDocument();
  mocks.auth.loading = false; mocks.auth.error = 'Account could not be verified';
  rerender(<Account/>);
  expect(screen.getByRole('alert')).toHaveTextContent('Account could not be verified');
  expect(screen.queryByText('operator@local')).not.toBeInTheDocument();
});
it('does not invent identity or roles that were not provided', () => {
  mocks.auth.profile = {name:'',email:'',roles:[]};
  render(<Account/>);
  expect(screen.getAllByText('Not provided')).toHaveLength(2);
  expect(screen.getByText('No current roles were reported.')).toBeVisible();
});
