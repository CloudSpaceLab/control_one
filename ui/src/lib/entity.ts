import type { EntityType } from '@/components/kit';

const RX = {
  ipv4: /^(?:(?:25[0-5]|2[0-4]\d|[01]?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d?\d)$/,
  md5: /^[a-f0-9]{32}$/i,
  sha1: /^[a-f0-9]{40}$/i,
  sha256: /^[a-f0-9]{64}$/i,
  uuid: /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i,
  email: /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/,
  domain: /^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$/i,
  url: /^https?:\/\/[^\s/$.?#].[^\s]*$/i,
  path: /^(?:[a-zA-Z]:\\|\/)[^\s]+$/,
};

function isIPv6(v: string): boolean {
  if (v === '::' || v === '::1') return true;
  if (!/^[0-9a-fA-F:]+$/.test(v)) return false;
  const doubleColons = (v.match(/::/g) ?? []).length;
  if (doubleColons > 1) return false;
  const parts = v.split(':');
  if (parts.length > 8) return false;
  // Each part must be empty (only legal next to ::) or 1-4 hex chars.
  for (const p of parts) {
    if (p === '') continue;
    if (p.length > 4) return false;
    if (!/^[0-9a-fA-F]+$/.test(p)) return false;
  }
  // Without `::`, must be exactly 8 groups.
  if (doubleColons === 0 && parts.length !== 8) return false;
  // Must contain at least 2 colons to count as IPv6 (not bare hex).
  return v.includes(':');
}

export interface ClassifyResult {
  type: EntityType | 'unknown';
  confidence: number;
  display?: string;
}

export function classifyValue(raw: string): ClassifyResult {
  const v = raw.trim();
  if (!v) return { type: 'unknown', confidence: 0 };

  if (RX.ipv4.test(v) || isIPv6(v)) return { type: 'ip', confidence: 0.99 };
  if (RX.sha256.test(v)) return { type: 'hash', confidence: 0.99, display: 'SHA256' };
  if (RX.sha1.test(v)) return { type: 'hash', confidence: 0.97, display: 'SHA1' };
  if (RX.md5.test(v)) return { type: 'hash', confidence: 0.95, display: 'MD5' };
  if (RX.uuid.test(v)) return { type: 'session', confidence: 0.7, display: 'UUID' };
  if (RX.email.test(v)) return { type: 'user', confidence: 0.85 };
  if (RX.url.test(v)) return { type: 'url', confidence: 0.9 };
  if (RX.domain.test(v)) return { type: 'domain', confidence: 0.85 };
  if (RX.path.test(v)) return { type: 'file', confidence: 0.7 };

  return { type: 'unknown', confidence: 0 };
}

export function entityRoute(type: EntityType, id: string): string {
  return `/investigate/${type}/${encodeURIComponent(id)}`;
}

export const ENTITY_TYPE_LABELS: Record<EntityType, string> = {
  ip: 'IP Address',
  process: 'Process',
  file: 'File',
  hash: 'Hash',
  user: 'User',
  host: 'Host',
  domain: 'Domain',
  url: 'URL',
  session: 'Session',
  alert: 'Alert',
  rule: 'Rule',
  tenant: 'Tenant',
};
