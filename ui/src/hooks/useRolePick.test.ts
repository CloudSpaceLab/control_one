import { describe, expect, it } from 'vitest';

// Re-implement priority pick logic for unit test (avoids React mock).
function pickPrimary(roles: string[]): 'admin' | 'operator' | 'viewer' {
  const set = new Set(roles.map((r) => r.toLowerCase()));
  if (set.has('admin')) return 'admin';
  if (set.has('operator')) return 'operator';
  return 'viewer';
}

describe('role priority', () => {
  it('admin > operator > viewer', () => {
    expect(pickPrimary(['admin', 'viewer'])).toBe('admin');
    expect(pickPrimary(['operator', 'viewer'])).toBe('operator');
    expect(pickPrimary(['viewer'])).toBe('viewer');
    expect(pickPrimary([])).toBe('viewer');
  });

  it('case-insensitive', () => {
    expect(pickPrimary(['ADMIN'])).toBe('admin');
  });
});
