import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest';
import { exchangeCodeForToken } from './oidc';

vi.mock('../config/oidc', () => ({oidcConfig:{enabled:true,tokenUrl:'https://provider.invalid/token',clientId:'console',redirectUri:'http://localhost/console/auth/callback'}}));
const jsonResponse=(payload:unknown)=>new Response(JSON.stringify(payload),{headers:{'Content-Type':'application/json'}});
const save=()=>sessionStorage.setItem('control-one-oidc',JSON.stringify({state:'expected',codeVerifier:'v'.repeat(64),createdAt:Date.now()}));

describe('Bounded provider exchange',()=>{
 beforeEach(()=>{sessionStorage.clear();});
 afterEach(()=>{vi.useRealTimers();vi.unstubAllGlobals();});
 it('times out missing headers, aborts the request and consumes the transaction',async()=>{
  vi.useFakeTimers();save();let signal:AbortSignal|undefined;
  vi.stubGlobal('fetch',vi.fn((_url,request)=>{signal=request.signal;return new Promise(()=>{});}));
  const result=expect(exchangeCodeForToken('code','expected')).rejects.toThrow('Organization sign-in timed out');
  await vi.advanceTimersByTimeAsync(15_000);await result;
  expect(signal?.aborted).toBe(true);expect(sessionStorage.getItem('control-one-oidc')).toBeNull();
  await expect(exchangeCodeForToken('code','expected')).rejects.toThrow('session');expect(fetch).toHaveBeenCalledTimes(1);
 });
 it('times out a body stream that stalls after successful headers',async()=>{
  vi.useFakeTimers();save();const cancel=vi.fn();
  vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(new ReadableStream({start(controller){controller.enqueue(new TextEncoder().encode('{'));},cancel}),{headers:{'Content-Type':'application/json'}})));
  const result=expect(exchangeCodeForToken('code','expected')).rejects.toThrow('timed out');
  await vi.advanceTimersByTimeAsync(15_000);await result;expect(cancel).toHaveBeenCalled();
 });
 it('stops reading an oversized stream without a content-length header',async()=>{
  save();const cancel=vi.fn();
  vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(new ReadableStream({start(controller){controller.enqueue(new Uint8Array(65_537));},cancel}),{headers:{'Content-Type':'application/json'}})));
  await expect(exchangeCodeForToken('code','expected')).rejects.toThrow('invalid token response');expect(cancel).toHaveBeenCalled();
 });
 it('refuses an oversized declared length',async()=>{
  save();vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response('{}',{headers:{'Content-Type':'application/json','Content-Length':'65537'}})));
  await expect(exchangeCodeForToken('code','expected')).rejects.toThrow('invalid token response');
 });
 it.each(['','bad code','x'.repeat(4097)])('rejects invalid authorization code before network: %j',async code=>{
  save();vi.stubGlobal('fetch',vi.fn());await expect(exchangeCodeForToken(code,'expected')).rejects.toThrow('Invalid authorization parameters');expect(fetch).not.toHaveBeenCalled();expect(sessionStorage.getItem('control-one-oidc')).toBeNull();
 });
 it.each([null,[],{access_token:'fixture',token_type:4},{access_token:'bad token',token_type:'Bearer'}])('rejects malformed token envelopes without runtime details: %j',async payload=>{
  save();vi.stubGlobal('fetch',vi.fn().mockResolvedValue(jsonResponse(payload)));await expect(exchangeCodeForToken('code','expected')).rejects.toThrow(/invalid token response|Bearer access token/);
 });
 it('forbids redirects and omits browser credentials',async()=>{
  save();vi.stubGlobal('fetch',vi.fn().mockResolvedValue(jsonResponse({access_token:'fixture',token_type:'Bearer'})));
  await exchangeCodeForToken('code','expected');expect(fetch).toHaveBeenCalledWith(expect.any(String),expect.objectContaining({redirect:'error',credentials:'omit',cache:'no-store',referrerPolicy:'no-referrer',signal:expect.any(AbortSignal)}));
 });
 it('redacts arbitrary network failure and permits only a fresh transaction after recovery',async()=>{
  save();vi.stubGlobal('fetch',vi.fn().mockRejectedValueOnce(new Error('PRIVATE_PROVIDER_DETAIL')).mockResolvedValueOnce(jsonResponse({access_token:'fresh-token',token_type:'Bearer'})));
  await expect(exchangeCodeForToken('code','expected')).rejects.toThrow('could not reach the identity provider');
  await expect(exchangeCodeForToken('code','expected')).rejects.toThrow('session');
  save();await expect(exchangeCodeForToken('new-code','expected')).resolves.toEqual({token:'fresh-token',returnTo:undefined});
 });
});
