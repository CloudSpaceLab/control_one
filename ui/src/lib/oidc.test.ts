import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest';
import { exchangeCodeForToken } from './oidc';
const jsonResponse = (payload: unknown) => new Response(JSON.stringify(payload), {headers:{'Content-Type':'application/json'}});

vi.mock('../config/oidc', () => ({ oidcConfig: {
  enabled: true, tokenUrl: 'https://provider.invalid/token', clientId: 'console',
  redirectUri: 'http://localhost/console/auth/callback',
} }));

describe('OIDC pending transaction', () => {
  beforeEach(() => {
    sessionStorage.clear();
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({access_token:'fixture',id_token:'identity-only',token_type:'Bearer'})));
  });
  afterEach(() => vi.unstubAllGlobals());
  const save = (overrides = {}) => sessionStorage.setItem('control-one-oidc', JSON.stringify({
    state: 'expected', codeVerifier: 'v'.repeat(64), createdAt: Date.now(), ...overrides,
  }));
  it.each([
    { createdAt: Date.now() - 600_001 }, { createdAt: Date.now() + 600_000 },
    { createdAt: 'invalid' }, { codeVerifier: 'short' }, { state: null },
  ])('refuses malformed or expired transaction before network: %j', async overrides => {
    save(overrides);
    await expect(exchangeCodeForToken('code', 'expected')).rejects.toThrow('session');
    expect(fetch).not.toHaveBeenCalled();
    expect(sessionStorage.getItem('control-one-oidc')).toBeNull();
  });
  it('consumes a mismatched transaction', async () => {
    save();
    await expect(exchangeCodeForToken('code', 'wrong')).rejects.toThrow('state mismatch');
    expect(fetch).not.toHaveBeenCalled();
    await expect(exchangeCodeForToken('code', 'expected')).rejects.toThrow('session');
  });
  it('sends the verifier automatically and rejects a repeated exchange', async () => {
    save();
    await expect(exchangeCodeForToken('code', 'expected')).resolves.toEqual({ token: 'fixture', returnTo: undefined });
    const [, request] = vi.mocked(fetch).mock.calls[0];
    const form = new URLSearchParams(request?.body as string);
    expect(form.get('code_verifier')).toBe('v'.repeat(64));
    expect(form.get('redirect_uri')).toBe('http://localhost/console/auth/callback');
    expect(form.has('client_secret')).toBe(false);
    await expect(exchangeCodeForToken('code', 'expected')).rejects.toThrow('session');
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it('consumes a transaction after a provider/network failure rather than replaying its code', async () => {
    save();
    vi.mocked(fetch).mockRejectedValueOnce(new Error('Network unavailable'));
    await expect(exchangeCodeForToken('code','expected')).rejects.toThrow('could not reach the identity provider');
    await expect(exchangeCodeForToken('code','expected')).rejects.toThrow('session');
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it('does not display a failed provider response body',async()=>{
    save();const body=vi.fn().mockResolvedValue('PRIVATE_PROVIDER_DETAIL');
    vi.mocked(fetch).mockResolvedValueOnce({ok:false,status:400,text:body} as unknown as Response);
    await expect(exchangeCodeForToken('code','expected')).rejects.toThrow('HTTP 400');
    expect(body).not.toHaveBeenCalled();expect(sessionStorage.getItem('control-one-oidc')).toBeNull();
  });
  it('does not expose a malformed provider payload through a JSON parser error',async()=>{
    save();vi.mocked(fetch).mockResolvedValueOnce(new Response('PRIVATE_PROVIDER_DETAIL',{headers:{'Content-Type':'application/json'}}));
    await expect(exchangeCodeForToken('code','expected')).rejects.toThrow('Provider returned an invalid token response');
  });
});

it('never falls back to an ID token when the API access token is absent',async()=>{
 sessionStorage.setItem('control-one-oidc',JSON.stringify({state:'expected',codeVerifier:'v'.repeat(64),createdAt:Date.now()}));
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(jsonResponse({id_token:'identity-only',token_type:'Bearer'})));
 await expect(exchangeCodeForToken('code','expected')).rejects.toThrow('Bearer access token');
 vi.unstubAllGlobals();
});
it('rejects a non-Bearer token response',async()=>{
 sessionStorage.setItem('control-one-oidc',JSON.stringify({state:'expected',codeVerifier:'v'.repeat(64),createdAt:Date.now()}));
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(jsonResponse({access_token:'fixture',token_type:'MAC'})));
 await expect(exchangeCodeForToken('code','expected')).rejects.toThrow('Bearer access token');
 vi.unstubAllGlobals();
});
