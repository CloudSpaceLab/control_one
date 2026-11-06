import { StrictMode } from 'react';
import { act, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { beforeEach, expect, it, vi } from 'vitest';
import { AuthCallback } from './AuthCallback';
const mocks=vi.hoisted(()=>({exchange:vi.fn(),signIn:vi.fn().mockResolvedValue(undefined),clear:vi.fn()}));
vi.mock('../lib/oidc',()=>({exchangeCodeForToken:mocks.exchange,clearOidcSession:mocks.clear}));
vi.mock('../providers/AuthProvider',()=>({useAuth:()=>({signIn:mocks.signIn,isAuthenticated:false})}));
beforeEach(()=>{vi.clearAllMocks();});
it('exchanges a one-use callback only once during StrictMode effect replay',async()=>{
 let resolve!:(value:{token:string})=>void;
 mocks.exchange.mockImplementation(()=>new Promise(r=>{resolve=r;}));
 render(<StrictMode><MemoryRouter initialEntries={['/auth/callback?code=fixture&state=expected']}><AuthCallback/></MemoryRouter></StrictMode>);
 expect(mocks.exchange).toHaveBeenCalledTimes(1);
 await act(async()=>{resolve({token:'fixture-token'});});
 await waitFor(()=>expect(mocks.signIn).toHaveBeenCalledTimes(1));
 expect(mocks.signIn).toHaveBeenCalledWith('fixture-token');
});
it('shows a failed one-use exchange without retrying it',async()=>{
 mocks.exchange.mockRejectedValue(new Error('Invalid transaction'));
 render(<StrictMode><MemoryRouter initialEntries={['/auth/callback?code=fixture&state=expected']}><AuthCallback/></MemoryRouter></StrictMode>);
 expect(await screen.findByText('Invalid transaction')).toBeVisible();
 expect(mocks.exchange).toHaveBeenCalledTimes(1);expect(mocks.signIn).not.toHaveBeenCalled();
});
it('consumes a provider refusal without exchanging a code or displaying raw provider details',async()=>{
 render(<MemoryRouter initialEntries={['/auth/callback?error=access_denied&error_description=PRIVATE_PROVIDER_DETAIL&state=fixture']}><AuthCallback/></MemoryRouter>);
 expect(await screen.findByText(/Organization sign-in was cancelled or refused/)).toBeVisible();
 expect(screen.queryByText(/PRIVATE_PROVIDER_DETAIL/)).not.toBeInTheDocument();
 expect(mocks.clear).toHaveBeenCalled();expect(mocks.exchange).not.toHaveBeenCalled();expect(mocks.signIn).not.toHaveBeenCalled();
});
it('clears an incomplete callback transaction and offers a fresh sign-in',async()=>{
 render(<MemoryRouter initialEntries={['/auth/callback?state=fixture']}><AuthCallback/></MemoryRouter>);
 expect(await screen.findByText('Missing authorization parameters')).toBeVisible();
 expect(screen.getByRole('button',{name:'Return to sign-in'})).toBeEnabled();
 expect(mocks.clear).toHaveBeenCalled();expect(mocks.signIn).not.toHaveBeenCalled();
});
function CurrentPath(){const location=useLocation();return <span data-testid="destination">{location.pathname}{location.search}</span>;}
it.each(['//external.invalid','/\\external.invalid','/\nexternal.invalid'])('does not navigate to unsafe return path %j',async returnTo=>{
 mocks.exchange.mockResolvedValue({token:'fixture',returnTo});
 render(<MemoryRouter initialEntries={['/auth/callback?code=fixture&state=expected']}><AuthCallback/><CurrentPath/></MemoryRouter>);
 await waitFor(()=>expect(screen.getByTestId('destination')).toHaveTextContent(/^\/$/));
});
it('retains a local requested return path after successful exchange',async()=>{
 mocks.exchange.mockResolvedValue({token:'fixture',returnTo:'/nodes?tab=packages'});
 render(<MemoryRouter initialEntries={['/auth/callback?code=fixture&state=expected']}><AuthCallback/><CurrentPath/></MemoryRouter>);
 await waitFor(()=>expect(screen.getByTestId('destination')).toHaveTextContent('/nodes?tab=packages'));
});

it.each(['code=one&code=two&state=expected','code=one&state=expected&state=other'])('refuses ambiguous callback parameters before exchange: %s',async query=>{
 render(<MemoryRouter initialEntries={['/auth/callback?'+query]}><AuthCallback/></MemoryRouter>);
 expect(await screen.findByText('Invalid authorization parameters. Start a new sign-in.')).toBeVisible();expect(mocks.clear).toHaveBeenCalled();expect(mocks.exchange).not.toHaveBeenCalled();expect(mocks.signIn).not.toHaveBeenCalled();
});
