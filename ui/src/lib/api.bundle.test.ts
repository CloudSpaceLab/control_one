import { afterEach, describe, expect, it, vi } from 'vitest';
import { APIClient } from './api';

afterEach(() => vi.unstubAllGlobals());

describe('Authenticated offline bundle download', () => {
  it('sends the user credential as a header and returns the file', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('signed-bundle', { headers: { 'Content-Disposition': 'attachment; filename="bundle.tar.gz"', 'Content-Type': 'application/gzip' } }));
    vi.stubGlobal('fetch', fetchMock);
    const client = new APIClient({ baseUrl: 'https://cp.example.test', token: 'fixture-user-session' });
    const file = await client.downloadAgentBundle({ os: 'linux', arch: 'amd64', token: 'cot_fixture' });
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe('https://cp.example.test/api/v1/agent/bundle?os=linux&arch=amd64&token=cot_fixture');
    expect(url).not.toContain('fixture-user-session');
    expect(options.headers.Authorization).toBe('Bearer fixture-user-session');
    expect(file.filename).toBe('bundle.tar.gz');
    expect(file.contentType).toBe('application/gzip');
    expect(await file.blob.text()).toBe('signed-bundle');
  });

  it('preserves actionable enrollment errors without logging the user out', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('enrollment token has expired', { status: 401 })));
    const client = new APIClient({ baseUrl: '', token: 'fixture-user-session' });
    const logout = vi.fn();
    client.onUnauthorized(logout);
    await expect(client.downloadAgentBundle({ os: 'linux', arch: 'amd64', token: 'cot_expired_fixture' })).rejects.toThrow('enrollment token has expired');
    expect(logout).not.toHaveBeenCalled();
  });
});
