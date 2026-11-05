import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest';
import { exchangeCodeForToken } from './oidc';

vi.mock('../config/oidc', () => ({ oidcConfig: {
  enabled: true, tokenUrl: 'https://provider.invalid/token', clientId: 'console',
  redirectUri: 'http://localhost/console/auth/callback',
} }));

describe('OIDC pending transaction', () => {
  beforeEach(() => {
    sessionStorage.clear();
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ id_token: 'fixture' }) }));
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
});
