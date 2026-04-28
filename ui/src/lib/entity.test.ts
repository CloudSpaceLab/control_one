import { describe, expect, it } from 'vitest';
import { classifyValue, entityRoute } from './entity';

describe('classifyValue', () => {
  it('detects IPv4', () => {
    expect(classifyValue('139.162.40.237').type).toBe('ip');
    expect(classifyValue('10.0.0.1').type).toBe('ip');
    expect(classifyValue('255.255.255.255').type).toBe('ip');
  });

  it('detects IPv6', () => {
    expect(classifyValue('2001:db8::1').type).toBe('ip');
    expect(classifyValue('::1').type).toBe('ip');
  });

  it('detects SHA256', () => {
    const sha = 'a'.repeat(64);
    const r = classifyValue(sha);
    expect(r.type).toBe('hash');
    expect(r.display).toBe('SHA256');
  });

  it('detects SHA1', () => {
    const r = classifyValue('a'.repeat(40));
    expect(r.type).toBe('hash');
    expect(r.display).toBe('SHA1');
  });

  it('detects MD5', () => {
    const r = classifyValue('d41d8cd98f00b204e9800998ecf8427e');
    expect(r.type).toBe('hash');
    expect(r.display).toBe('MD5');
  });

  it('detects email as user', () => {
    expect(classifyValue('admin@example.com').type).toBe('user');
  });

  it('detects domain', () => {
    expect(classifyValue('control-one.example.com').type).toBe('domain');
  });

  it('detects URL', () => {
    expect(classifyValue('https://example.com/path').type).toBe('url');
  });

  it('detects UUID as session', () => {
    expect(classifyValue('123e4567-e89b-12d3-a456-426614174000').type).toBe('session');
  });

  it('classifies unknown gibberish', () => {
    expect(classifyValue('not an entity').type).toBe('unknown');
    expect(classifyValue('   ').type).toBe('unknown');
  });

  it('trims whitespace', () => {
    expect(classifyValue('  8.8.8.8  ').type).toBe('ip');
  });

  it('prefers more specific hash over UUID-shaped MD5 collision rare', () => {
    // 32 hex without dashes = MD5, 32 hex with dashes = UUID.
    const md5 = 'd41d8cd98f00b204e9800998ecf8427e';
    expect(classifyValue(md5).display).toBe('MD5');
  });
});

describe('entityRoute', () => {
  it('encodes value', () => {
    expect(entityRoute('ip', '139.162.40.237')).toBe('/investigate/ip/139.162.40.237');
    expect(entityRoute('domain', 'a.b.c')).toBe('/investigate/domain/a.b.c');
    expect(entityRoute('user', 'admin@example.com')).toBe(
      '/investigate/user/admin%40example.com',
    );
  });
});
