export type StateTone = 'healthy' | 'warning' | 'degraded' | 'critical' | 'info' | 'unknown';

export type EntityType =
  | 'ip'
  | 'process'
  | 'file'
  | 'hash'
  | 'user'
  | 'host'
  | 'domain'
  | 'url'
  | 'session'
  | 'alert'
  | 'rule'
  | 'tenant';

export const TONE_COLOR: Record<StateTone, string> = {
  healthy: 'var(--state-healthy)',
  warning: 'var(--state-warning)',
  degraded: 'var(--state-degraded)',
  critical: 'var(--state-critical)',
  info: 'var(--state-info)',
  unknown: 'var(--state-unknown)',
};

export const TONE_CLASS: Record<StateTone, string> = {
  healthy: 'text-state-healthy',
  warning: 'text-state-warning',
  degraded: 'text-state-degraded',
  critical: 'text-state-critical',
  info: 'text-state-info',
  unknown: 'text-state-unknown',
};
