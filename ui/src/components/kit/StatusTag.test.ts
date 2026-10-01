import { describe, expect, it } from 'vitest';
import { severityTone } from './StatusTag';

describe('severityTone', () => {
  it.each([
    ['critical', 'critical'],
    ['high', 'degraded'],
    ['medium', 'warning'],
    ['warning', 'warning'],
    ['low', 'info'],
    ['info', 'info'],
    ['', 'unknown'],
    ['other', 'unknown'],
  ] as const)('maps %s to %s', (severity, tone) => {
    expect(severityTone(severity)).toBe(tone);
  });
});
