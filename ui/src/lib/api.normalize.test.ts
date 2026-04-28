import { describe, expect, it } from 'vitest';
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
