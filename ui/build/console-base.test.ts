// @vitest-environment node
import { createServer as createHTTPServer, request } from 'node:http';
import { afterAll, beforeAll, expect, it } from 'vitest';
import { canonicalConsoleBase } from './console-base';

const server = createHTTPServer((req, res) => canonicalConsoleBase(req, res, () => {
  res.statusCode = 204;
  res.end();
}));
let port: number;
beforeAll(async () => {
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  port = (server.address() as { port: number }).port;
});
afterAll(async () => { await new Promise<void>(resolve => server.close(() => resolve())); });

async function probe(path: string, method = 'GET') {
  return new Promise<{ status: number; location?: string; body: string }>((resolve, reject) => {
    const req = request({ hostname: '127.0.0.1', port, path, method }, res => {
      let body = '';
      res.on('data', chunk => { body += chunk; });
      res.on('end', () => resolve({ status: res.statusCode!, location: res.headers.location, body }));
    });
    req.on('error', reject);
    req.end();
  });
}
it('redirects an exact console root and retains encoded query parameters', async () => {
  expect(await probe('/console?period=30d&next=%2Fnodes')).toEqual({
    status: 308, location: '/console/?period=30d&next=%2Fnodes', body: '',
  });
});
it('supports HEAD without a response body', async () => {
  expect(await probe('/console', 'HEAD')).toEqual({ status: 308, location: '/console/', body: '' });
});
it.each(['/console/', '/console/login', '/console/auth/callback?code=fixture&state=fixture', '/console-other', '/api/v1/me'])(
  'leaves other paths unchanged: %s', async path => { expect((await probe(path)).status).toBe(204); },
);
it('does not redirect a mutation', async () => { expect((await probe('/console', 'POST')).status).toBe(204); });
