export interface IPBehaviorPresentationFinding {
  source_ip?: string;
  country_code?: string;
  asn?: string;
  category?: string;
  metric?: string;
  severity?: string;
  reason?: string;
  score?: number;
  observed_value?: number;
  evidence?: Record<string, unknown>;
}

export interface IPBehaviorPresentation {
  category: string;
  categoryLabel: string;
  confidence: number;
  source: string;
  headline: string;
  summary: string;
  signals: string[];
  hiddenSignalCount: number;
  alertLabel?: string;
}

const TECHNICAL_REASON_PATTERNS = [
  /behavior is being evaluated as a baseline dimension/i,
  /^country\/country-app behavior/i,
  /^asn\/app behavior/i,
];

export function ipBehaviorConfidence(finding?: IPBehaviorPresentationFinding): number {
  if (!finding) return 0;
  const value = typeof finding.score === 'number' && Number.isFinite(finding.score)
    ? finding.score
    : typeof finding.observed_value === 'number' && Number.isFinite(finding.observed_value)
      ? finding.observed_value
      : 0;
  return Math.max(0, Math.min(100, Math.round(value ?? 0)));
}

export function describeIPBehaviorFinding(
  finding?: IPBehaviorPresentationFinding,
  options: { countryLabel?: string; maxSignals?: number } = {},
): IPBehaviorPresentation {
  const confidence = ipBehaviorConfidence(finding);
  const category = cleanCategory(finding?.category || finding?.metric || 'ip_behavior');
  const categoryLabel = formatIPBehaviorCategory(category);
  const source = finding?.source_ip || sourceFromReason(finding?.reason) || finding?.country_code || finding?.asn || 'unknown source';
  const allSignals = signalList(finding);
  const maxSignals = options.maxSignals ?? 4;
  const signals = allSignals.slice(0, maxSignals);
  const fallbackSignal = allSignals[0] || 'behavior exceeded its learned normal pattern';
  const location = options.countryLabel || finding?.country_code || 'network traffic';
  const summary = `${categoryLabel} from ${source} reached ${confidence}% confidence in ${location}: ${fallbackSignal}${signals.length > 1 ? `, ${signals.slice(1, 3).join(', ')}` : ''}.`;
  return {
    category,
    categoryLabel,
    confidence,
    source,
    headline: `${categoryLabel} from ${source}`,
    summary,
    signals,
    hiddenSignalCount: Math.max(0, allSignals.length - signals.length),
    alertLabel: confidence >= 100 ? 'Auto-alerted at 100%' : confidence >= 85 ? 'Critical review' : undefined,
  };
}

export function formatIPBehaviorCategory(category: string): string {
  const normalized = cleanCategory(category);
  const labels: Record<string, string> = {
    credential_attack: 'Credential attack',
    exploit_attempt: 'Exploit attempt',
    exfiltration_risk: 'Exfiltration risk',
    scanner_probe: 'Scanner probe',
    slow_distributed_attack: 'Distributed attack',
    webshell_callback: 'Webshell callback',
    partner_drift: 'Partner drift',
    ip_behavior: 'IP behavior shift',
  };
  return labels[normalized] || titleCase(normalized.replace(/_/g, ' '));
}

function signalList(finding?: IPBehaviorPresentationFinding): string[] {
  const raw = splitReason(finding?.reason).filter((reason) => !TECHNICAL_REASON_PATTERNS.some((pattern) => pattern.test(reason)));
  const signals = raw.map(humanizeReason).filter(Boolean);
  const evidenceSignals = signalsFromEvidence(finding?.evidence);
  return uniqueStrings([...signals, ...evidenceSignals]);
}

function splitReason(reason?: string): string[] {
  if (!reason) return [];
  const body = reason.includes(':') ? reason.slice(reason.indexOf(':') + 1) : reason;
  return body.split(';').map((part) => part.trim()).filter(Boolean);
}

function humanizeReason(reason: string): string {
  let match = /^request burst:\s*([\d,]+)\s+requests?\s+in\s+(.+)$/i.exec(reason);
  if (match) return `${match[1]} requests in ${match[2]}`;

  match = /^auth failure spike:\s*([\d,]+)\s+401\/403 responses/i.exec(reason);
  if (match) return `${match[1]} auth failures`;

  match = /^server error spike:\s*([\d,]+)\s+500\/502\/503 responses/i.exec(reason);
  if (match) return `${match[1]} server errors`;

  match = /^large outbound transfer:\s*(.+)$/i.exec(reason);
  if (match) return `${match[1]} outbound transfer`;

  match = /^IP reputation score\s+([\d,]+)/i.exec(reason);
  if (match) return `IP reputation ${match[1]}`;

  match = /request rate ([\d.]+) exceeded learned (p99|peak) ([\d.]+)/i.exec(reason);
  if (match) return `Request rate ${match[1]} vs ${match[2]} ${match[3]}`;

  match = /bytes out ([\d.]+) exceeded learned (p99|peak) ([\d.]+)/i.exec(reason);
  if (match) return `Bytes out ${match[1]} vs ${match[2]} ${match[3]}`;

  match = /auth failure ratio ([\d.]+)% exceeded learned ([\d.]+)%/i.exec(reason);
  if (match) return `Auth failures ${match[1]}% vs learned ${match[2]}%`;

  match = /server-error ratio ([\d.]+)% exceeded learned ([\d.]+)%/i.exec(reason);
  if (match) return `Server errors ${match[1]}% vs learned ${match[2]}%`;

  if (/sensitive\/admin path probing/i.test(reason)) return 'Admin path probing';
  if (/scanner-like path\/status diversity/i.test(reason)) return 'Scanner-like path spread';
  if (/previously inactive hour/i.test(reason)) return 'Inactive-hour traffic';
  if (/previously inactive weekday/i.test(reason)) return 'Inactive weekday';
  if (/successful request followed prior auth failures/i.test(reason)) return 'Success after failures';
  if (/slow distributed pattern/i.test(reason)) return reason.replace(/^slow distributed pattern:\s*/i, 'Distributed source pattern: ');
  if (/host\/app correlation after web traffic/i.test(reason)) return reason.replace(/^host\/app correlation after web traffic:\s*/i, 'Host correlation: ');

  return sentenceCase(reason);
}

function signalsFromEvidence(evidence?: Record<string, unknown>): string[] {
  if (!evidence) return [];
  const out: string[] = [];
  const requestCount = numberField(evidence, 'request_count');
  const window = stringField(evidence, 'window');
  if (requestCount > 0 && window) out.push(`${requestCount.toLocaleString()} requests in ${window}`);

  const statusCounts = recordField(evidence, 'status_counts');
  const auth = numberField(statusCounts, '401') + numberField(statusCounts, '403');
  const errors = numberField(statusCounts, '500') + numberField(statusCounts, '502') + numberField(statusCounts, '503') + numberField(statusCounts, '5xx');
  if (auth > 0) out.push(`${auth.toLocaleString()} auth failures`);
  if (errors > 0) out.push(`${errors.toLocaleString()} server errors`);

  const bytesOut = numberField(evidence, 'bytes_out');
  if (bytesOut >= 1024 * 1024) out.push(`${formatBytes(bytesOut)} outbound`);

  const paths = evidence.top_paths;
  if (Array.isArray(paths) && paths.length > 0) out.push(`${paths.length} suspicious paths`);
  return out;
}

function sourceFromReason(reason?: string): string {
  if (!reason) return '';
  const match = /\bfrom\s+([^\s]+)\s+scored\b/i.exec(reason);
  return match?.[1] || '';
}

function cleanCategory(category: string): string {
  return category.trim().toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '') || 'ip_behavior';
}

function titleCase(value: string): string {
  return value.replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function sentenceCase(value: string): string {
  if (!value) return '';
  return value.charAt(0).toUpperCase() + value.slice(1);
}

function recordField(source: Record<string, unknown> | undefined, key: string): Record<string, unknown> | undefined {
  const value = source?.[key];
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : undefined;
}

function numberField(source: Record<string, unknown> | undefined, key: string): number {
  const value = source?.[key];
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string') {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  return 0;
}

function stringField(source: Record<string, unknown> | undefined, key: string): string {
  const value = source?.[key];
  return typeof value === 'string' ? value : '';
}

function uniqueStrings(values: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const value of values) {
    const trimmed = value.trim();
    const key = trimmed.toLowerCase();
    if (!trimmed || seen.has(key)) continue;
    seen.add(key);
    out.push(trimmed);
  }
  return out;
}

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let value = bytes;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  return `${value >= 10 || index === 0 ? Math.round(value).toLocaleString() : value.toFixed(1)} ${units[index]}`;
}
