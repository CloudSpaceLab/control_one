import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AuthProvider, useAuth } from './AuthProvider';

function SignOutHarness(): JSX.Element {
  const { isAuthenticated, signOut } = useAuth();
  return (
    <button type="button" onClick={() => { void signOut(); }}>
      {isAuthenticated ? 'Sign out' : 'Signed out'}
    </button>
  );
}

function ProfileRefreshHarness(): JSX.Element {
  const {profile, loading, refreshProfile, error} = useAuth();
  return <><p>{error}</p><p>{loading ? 'Loading profile' : 'Profile ready'}</p><p>Roles: {profile?.roles.join(',')}</p><button onClick={() => void refreshProfile({background: true})}>Refresh current authority</button></>;
}

function CandidateSignInHarness(): JSX.Element {
 const {signIn,signOut,isAuthenticated,error}=useAuth();
 return <><p>{isAuthenticated?'Authenticated':'Unauthenticated'}</p><p>{error}</p><button onClick={()=>void signIn('candidate-fixture-token').catch(()=>{})}>Verify candidate</button><button onClick={()=>void signOut()}>Cancel candidate</button></>;
}

describe('AuthProvider', () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
  });

  afterEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
  });

  it('updates revoked roles in the same session without triggering a foreground loading state', async () => {
    window.localStorage.setItem('control-one-token', 'existing-operator-session');
    let roles = ['operator', 'ciso'];
    const response = () => new Response(JSON.stringify({subject: 'operator', name: 'Operator', email: 'operator@local', roles, groups: [], permissions: [], type: 'user'}), {status: 200, headers: {'Content-Type': 'application/json'}});
    let finishRefresh: (value: Response) => void = () => {};
    const fetchMock = vi.fn().mockResolvedValueOnce(response()).mockImplementationOnce(() => new Promise<Response>(resolve => {finishRefresh = resolve;}));
    vi.stubGlobal('fetch', fetchMock);
    render(<AuthProvider><ProfileRefreshHarness /></AuthProvider>);
    expect(await screen.findByText('Roles: operator,ciso')).toBeVisible();
    roles = ['operator'];
    await userEvent.click(screen.getByRole('button', {name: 'Refresh current authority'}));
    expect(screen.getByText('Profile ready')).toBeVisible();
    expect(screen.getByText('Roles: operator,ciso')).toBeVisible();
    await act(async () => {finishRefresh(response());});
    expect(await screen.findByText('Roles: operator')).toBeVisible();
    expect(screen.getByText('Profile ready')).toBeVisible();
    expect(window.localStorage.getItem('control-one-token')).toBe('existing-operator-session');
    expect(fetchMock.mock.calls.length).toBe(2);
  });

  it('revokes the active backend session before clearing local sign-in state', async () => {
    window.localStorage.setItem('control-one-token', 'active-session-token');
    const fetchMock = vi.fn(async (url: RequestInfo | URL, init?: RequestInit) => {
      void init;
      const href = String(url);
      if (href.endsWith('/api/v1/me')) {
        return new Response(
          JSON.stringify({
            subject: 'user-1',
            email: 'admin@local',
            display_name: 'Default Admin',
            roles: ['admin'],
            groups: [],
            permissions: [],
            type: 'user',
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        );
      }
      if (href.endsWith('/api/v1/auth/logout')) {
        return new Response(null, { status: 200 });
      }
      return new Response('not found', { status: 404 });
    });
    vi.stubGlobal('fetch', fetchMock);

    render(
      <AuthProvider>
        <SignOutHarness />
      </AuthProvider>,
    );

    await screen.findByText('Sign out');
    await userEvent.click(screen.getByRole('button', { name: 'Sign out' }));

    await waitFor(() => {
      const logoutCall = fetchMock.mock.calls.find(([url]) => String(url).endsWith('/api/v1/auth/logout'));
      expect(logoutCall?.[1]?.method).toBe('POST');
      expect((logoutCall?.[1]?.headers as Record<string, string>).Authorization).toBe('Bearer active-session-token');
    });
    await waitFor(() => expect(window.localStorage.getItem('control-one-token')).toBeNull());
    expect(await screen.findByText('Signed out')).toBeInTheDocument();
  });
  it('explains a failed directory recheck when the current session ends', async () => {
    window.localStorage.setItem('control-one-token', 'directory-fixture-session');
    const fetchMock = vi.fn().mockResolvedValue(new Response('Directory recheck failed', {status: 401, headers: {'X-ControlOne-Auth-Reason': 'directory_verification_required'}}));
    vi.stubGlobal('fetch', fetchMock);
    render(<AuthProvider><ProfileRefreshHarness /></AuthProvider>);
    expect(await screen.findByText(/Directory access changed or could not be verified/)).toBeVisible();
    await waitFor(() => expect(window.localStorage.getItem('control-one-token')).toBeNull());
  });

  it('never publishes or stores a candidate before profile verification, then accepts a verified identity',async()=>{
    let finish:(value:Response)=>void=()=>{};
    const profile=()=>new Response(JSON.stringify({subject:'verified',roles:['viewer'],groups:[],type:'user'}),{status:200,headers:{'Content-Type':'application/json'}});
    vi.stubGlobal('fetch',vi.fn().mockImplementationOnce(()=>new Promise<Response>(resolve=>{finish=resolve;})).mockImplementation(async()=>profile()));
    render(<AuthProvider><CandidateSignInHarness/></AuthProvider>);await userEvent.click(screen.getByRole('button',{name:'Verify candidate'}));
    expect(screen.getByText('Unauthenticated')).toBeVisible();expect(window.localStorage.getItem('control-one-token')).toBeNull();
    await act(async()=>finish(profile()));expect(await screen.findByText('Authenticated')).toBeVisible();await waitFor(()=>expect(window.localStorage.getItem('control-one-token')).toBe('candidate-fixture-token'));
  });
  it('reports initial profile refusal as unverified sign-in and discards the candidate',async()=>{
    vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response('Forbidden identity fixture',{status:401})));
    render(<AuthProvider><CandidateSignInHarness/></AuthProvider>);await userEvent.click(screen.getByRole('button',{name:'Verify candidate'}));
    expect(await screen.findByText(/Sign-in could not be verified/)).toBeVisible();expect(screen.getByText('Unauthenticated')).toBeVisible();expect(window.localStorage.getItem('control-one-token')).toBeNull();expect(screen.queryByText(/Session has expired/)).not.toBeInTheDocument();
  });
  it('keeps expiry messaging for a previously established session',async()=>{
    window.localStorage.setItem('control-one-token','expired-established-session');vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response('Expired',{status:401})));
    render(<AuthProvider><ProfileRefreshHarness/></AuthProvider>);expect(await screen.findByText('Session has expired. Please sign in again.')).toBeVisible();await waitFor(()=>expect(window.localStorage.getItem('control-one-token')).toBeNull());
  });

  it('does not resurrect a candidate after sign-out while verification is pending',async()=>{
    let finish:(value:Response)=>void=()=>{};vi.stubGlobal('fetch',vi.fn().mockImplementationOnce(()=>new Promise<Response>(resolve=>{finish=resolve;})));
    render(<AuthProvider><CandidateSignInHarness/></AuthProvider>);await userEvent.click(screen.getByRole('button',{name:'Verify candidate'}));await userEvent.click(screen.getByRole('button',{name:'Cancel candidate'}));
    await act(async()=>finish(new Response(JSON.stringify({subject:'candidate',roles:['viewer'],groups:[],type:'user'}),{status:200,headers:{'Content-Type':'application/json'}})));
    expect(screen.getByText('Unauthenticated')).toBeVisible();expect(window.localStorage.getItem('control-one-token')).toBeNull();
  });

});
