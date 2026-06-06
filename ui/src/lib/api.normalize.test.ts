import { afterEach, describe, expect, it, vi } from 'vitest';
import { APIClient } from './api';

describe('APIClient.normalizeBase', () => {
  it('returns DEFAULT for null/undefined (env unset)', () => {
    expect(APIClient.normalizeBase(undefined)).toMatch(/^(http:\/\/localhost:8443|)$/);
    expect(APIClient.normalizeBase(null)).toMatch(/^(http:\/\/localhost:8443|)$/);
  });

  it('keeps empty string (same-origin)', () => {
    expect(APIClient.normalizeBase('')).toBe('');
  });

  it('strips trailing slashes', () => {
    expect(APIClient.normalizeBase('https://example.com/')).toBe('https://example.com');
    expect(APIClient.normalizeBase('https://example.com////')).toBe('https://example.com');
  });

  it('strips trailing /api', () => {
    expect(APIClient.normalizeBase('https://example.com/api')).toBe('https://example.com');
    expect(APIClient.normalizeBase('https://example.com/api/')).toBe('https://example.com');
  });

  it('strips trailing /api/v1', () => {
    expect(APIClient.normalizeBase('https://example.com/api/v1')).toBe('https://example.com');
    expect(APIClient.normalizeBase('https://example.com/api/v1/')).toBe('https://example.com');
  });

  it('preserves arbitrary path prefixes that are not /api', () => {
    expect(APIClient.normalizeBase('https://example.com/control')).toBe('https://example.com/control');
  });

  it('leaves a clean host alone', () => {
    expect(APIClient.normalizeBase('https://example.com')).toBe('https://example.com');
  });

  it('regression: composing path always yields a single /api', () => {
    const cases = [
      'https://example.com',
      'https://example.com/',
      'https://example.com/api',
      'https://example.com/api/',
      'https://example.com/api/v1',
      'https://example.com/api/v1/',
      '',
    ];
    for (const c of cases) {
      const url = APIClient.normalizeBase(c) + '/api/v1/auth/login';
      expect(url).not.toMatch(/\/api\/api\//);
      expect(url.endsWith('/api/v1/auth/login')).toBe(true);
    }
  });
});

describe('APIClient.getCoverageMatrix', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('calls the coverage matrix route and normalizes data envelopes', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          generated_at: '2026-05-19T10:00:00Z',
          data: [{ domain: 'compliance', name: 'Password policy evidence', state: 'unsupported' }],
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    );
    vi.stubGlobal('fetch', fetchMock);

    const client = new APIClient({ baseUrl: 'https://cp.example.com/api/v1', token: 'session-token' });
    const matrix = await client.getCoverageMatrix({ tenant_id: 'tenant-1', domain: 'compliance' });
    const [url, init] = fetchMock.mock.calls[0];

    expect(url).toBe('https://cp.example.com/api/v1/coverage/matrix?tenant_id=tenant-1&domain=compliance');
    expect(init.headers.Authorization).toBe('Bearer session-token');
    expect(matrix.generated_at).toBe('2026-05-19T10:00:00Z');
    expect(matrix.rows).toEqual([{ domain: 'compliance', name: 'Password policy evidence', state: 'unsupported' }]);
  });

  it('normalizes backend catalog matrix envelopes', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          catalog_version: 'coverage.truth.v1',
          matrix: [{ domain: 'parser', title: 'Typed parser coverage', state: 'raw_only', quality: ['fixture_tested'] }],
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    );
    vi.stubGlobal('fetch', fetchMock);

    const client = new APIClient({ baseUrl: 'https://cp.example.com', token: 'session-token' });
    const matrix = await client.getCoverageMatrix();

    expect(matrix.rows).toEqual([
      { domain: 'parser', title: 'Typed parser coverage', state: 'raw_only', quality: ['fixture_tested'] },
    ]);
  });
});

describe('APIClient.listTopTalkers', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('normalizes backend data envelopes', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          data: [{ ip: '203.0.113.10', bytes_out: 20, bytes_in: 10, conn_count: 2, threat_match: false }],
          source: 'small-analytics-pending',
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    );
    vi.stubGlobal('fetch', fetchMock);

    const client = new APIClient({ baseUrl: 'https://cp.example.com', token: 'session-token' });
    const talkers = await client.listTopTalkers({ tenantId: 'tenant-1', limit: 5 });
    const [url] = fetchMock.mock.calls[0];

    expect(url).toBe('https://cp.example.com/api/v1/connections/top-talkers?tenant_id=tenant-1&limit=5');
    expect(talkers).toEqual([
      { ip: '203.0.113.10', bytes_out: 20, bytes_in: 10, conn_count: 2, threat_match: false },
    ]);
  });
});
