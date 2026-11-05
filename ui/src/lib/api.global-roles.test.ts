import {afterEach, expect, it, vi} from 'vitest';
import {APIClient} from './api';

afterEach(() => vi.unstubAllGlobals());

it('explicitly identifies a global role replacement on the wire', async () => {
  const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({id: 'existing', external_id: 'reviewer', roles: ['viewer'], role_scope: 'global', created_at: '2026-01-01T00:00:00Z'}), {status: 200, headers: {'Content-Type': 'application/json'}}));
  vi.stubGlobal('fetch', fetchMock);
  const api = new APIClient({baseUrl: 'http://controlone.test', token: 'fixture-session'});
  const result = await api.updateUserRoles('existing', {roles: ['viewer'], expected_revision: 'reviewed-snapshot'});
  expect(JSON.parse(fetchMock.mock.calls[0][1]?.body as string)).toEqual({roles: ['viewer'], role_scope: 'global', expected_revision: 'reviewed-snapshot'});
  expect(result.role_scope).toBe('global');
});
