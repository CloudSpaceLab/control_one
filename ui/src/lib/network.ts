import type { ConnectionRow } from './api';

export function normalizePeerAddress(value?: string | null): string {
  const trimmed = (value ?? '').trim();
  if (!trimmed || trimmed === '-' || trimmed === '*' || trimmed === '0.0.0.0' || trimmed === '::') return '';
  return trimmed.replace(/^\[/, '').replace(/\]$/, '');
}

function parseIPv4(ip: string): number[] | null {
  const parts = ip.split('.');
  if (parts.length !== 4) return null;
  const nums = parts.map((part) => Number(part));
  if (nums.some((n, idx) => !/^\d+$/.test(parts[idx]) || !Number.isInteger(n) || n < 0 || n > 255)) return null;
  return nums;
}

export function isPublicIP(ip: string): boolean {
  const normalized = normalizePeerAddress(ip).toLowerCase();
  if (!normalized) return false;
  if (normalized.startsWith('::ffff:')) {
    return isPublicIP(normalized.slice('::ffff:'.length));
  }
  const v4 = parseIPv4(normalized);
  if (v4) {
    const [a, b, c] = v4;
    if (a === 0 || a === 10 || a === 127 || a === 255) return false;
    if (a === 100 && b >= 64 && b <= 127) return false;
    if (a === 169 && b === 254) return false;
    if (a === 172 && b >= 16 && b <= 31) return false;
    if (a === 192 && b === 168) return false;
    if (a === 192 && b === 0 && c === 2) return false;
    if (a === 198 && (b === 18 || b === 19)) return false;
    if (a === 198 && b === 51 && c === 100) return false;
    if (a === 203 && b === 0 && c === 113) return false;
    if (a >= 224) return false;
    return true;
  }
  if (!normalized.includes(':')) return false;
  if (
    normalized === '::1' ||
    normalized.startsWith('fe80:') ||
    normalized.startsWith('fc') ||
    normalized.startsWith('fd') ||
    normalized.startsWith('ff') ||
    normalized.startsWith('2001:db8:')
  ) {
    return false;
  }
  return true;
}

export function connectionPeerIp(row: ConnectionRow): string {
  if (row.direction === 'inbound') return normalizePeerAddress(row.src_ip) || normalizePeerAddress(row.dst_ip);
  if (row.direction === 'outbound') return normalizePeerAddress(row.dst_ip) || normalizePeerAddress(row.src_ip);
  return normalizePeerAddress(row.dst_ip) || normalizePeerAddress(row.src_ip);
}

export function isExternalConnection(row: ConnectionRow): boolean {
  return isPublicIP(connectionPeerIp(row));
}

export function isListeningConnection(row: ConnectionRow): boolean {
  const direction = (row.direction ?? '').toLowerCase();
  if (direction === 'listening' || direction === 'listen') return true;
  return direction === 'inbound' && !normalizePeerAddress(row.src_ip) && !normalizePeerAddress(row.dst_ip);
}

export function hasConnectionShape(row: ConnectionRow): boolean {
  if (connectionPeerIp(row)) return true;
  if (!isListeningConnection(row)) return false;
  return Boolean(row.src_port || row.dst_port || row.process_name || row.protocol);
}

export function connectionServicePort(row: ConnectionRow): number | undefined {
  if (row.direction === 'inbound' || isListeningConnection(row)) return row.dst_port ?? row.src_port;
  return row.dst_port ?? row.src_port;
}
