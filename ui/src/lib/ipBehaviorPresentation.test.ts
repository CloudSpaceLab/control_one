import { describe, expect, it } from 'vitest';
import { describeIPBehaviorFinding } from './ipBehaviorPresentation';

describe('describeIPBehaviorFinding', () => {
  it('turns backend IP behavior reasons into concise operator signals', () => {
    const finding = describeIPBehaviorFinding({
      source_ip: '102.89.69.217',
      metric: 'credential_attack',
      severity: 'critical',
      observed_value: 100,
      reason: 'credential_attack behavior from 102.89.69.217 scored 100: country/country-app behavior is being evaluated as a baseline dimension; ASN/app behavior is being evaluated as a baseline dimension; request burst: 21 requests in 1m; auth failure spike: 21 401/403 responses; node_country_app auth failure ratio 100% exceeded learned 2%',
    }, { countryLabel: 'Nigeria', maxSignals: 3 });

    expect(finding.categoryLabel).toBe('Credential attack');
    expect(finding.confidence).toBe(100);
    expect(finding.alertLabel).toBe('Auto-alerted at 100%');
    expect(finding.signals).toEqual([
      '21 requests in 1m',
      '21 auth failures',
      'Auth failures 100% vs learned 2%',
    ]);
    expect(finding.summary).not.toContain('baseline dimension');
  });
});
