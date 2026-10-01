import { useEffect, useState } from 'react';
import { Button } from '../ui/button';
import { Input } from '../ui/input';
import { Label } from '../ui/label';
import { Panel, SelectField, StatusTag } from '../kit';
import { useApiClient } from '../../hooks/useApiClient';
import type { TenantRemediationConfig } from '../../lib/api';

interface IPResponseSettingsProps {
  tenantId?: string | null;
}

type IPResponsePolicyDraft = Pick<
  TenantRemediationConfig,
  | 'AutoBlockEnabled'
  | 'AutoBlockMinConfidence'
  | 'DefaultIPBlockScope'
  | 'DefaultIPBlockTTLSeconds'
  | 'RequireCorroboratingThreatIntel'
>;

const DEFAULT_POLICY: IPResponsePolicyDraft = {
  AutoBlockEnabled: true,
  AutoBlockMinConfidence: 100,
  DefaultIPBlockScope: 'affected',
  DefaultIPBlockTTLSeconds: 3600,
  RequireCorroboratingThreatIntel: true,
};

export function IPResponseSettings({ tenantId }: IPResponseSettingsProps): JSX.Element {
  const client = useApiClient();
  const [policy, setPolicy] = useState<IPResponsePolicyDraft>(DEFAULT_POLICY);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    if (!tenantId) {
      setPolicy(DEFAULT_POLICY);
      setError('');
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError('');
    void client.getTenantRemediationConfig(tenantId)
      .then((config) => {
        if (cancelled) return;
        setPolicy({
          AutoBlockEnabled: config.AutoBlockEnabled,
          AutoBlockMinConfidence: config.AutoBlockMinConfidence,
          DefaultIPBlockScope: config.DefaultIPBlockScope,
          DefaultIPBlockTTLSeconds: config.DefaultIPBlockTTLSeconds,
          RequireCorroboratingThreatIntel: config.RequireCorroboratingThreatIntel,
        });
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : 'IP response settings failed to load');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [client, tenantId]);

  const save = async () => {
    if (!tenantId) return;
    setSaving(true);
    setSaved(false);
    setError('');
    try {
      const updated = await client.upsertTenantRemediationConfig(tenantId, policy);
      setPolicy({
        AutoBlockEnabled: updated.AutoBlockEnabled,
        AutoBlockMinConfidence: updated.AutoBlockMinConfidence,
        DefaultIPBlockScope: updated.DefaultIPBlockScope,
        DefaultIPBlockTTLSeconds: updated.DefaultIPBlockTTLSeconds,
        RequireCorroboratingThreatIntel: updated.RequireCorroboratingThreatIntel,
      });
      setSaved(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'IP response settings failed to save');
    } finally {
      setSaving(false);
    }
  };

  return (
    <Panel
      padding="md"
      eyebrow="IP RESPONSE"
      title="Automatic IP blocking"
      actions={<StatusTag tone={policy.AutoBlockEnabled ? 'healthy' : 'unknown'}>{policy.AutoBlockEnabled ? 'On' : 'Off'}</StatusTag>}
    >
      {!tenantId ? (
        <p className="text-sm text-text-secondary">Select a tenant to configure IP response.</p>
      ) : (
        <div className="flex flex-col gap-4">
          <label className="flex items-center gap-3 text-sm font-medium text-foreground">
            <input
              type="checkbox"
              checked={policy.AutoBlockEnabled}
              disabled={loading || saving}
              onChange={(event) => {
                setSaved(false);
                setPolicy((current) => ({ ...current, AutoBlockEnabled: event.target.checked }));
              }}
              className="h-4 w-4 rounded border-border"
            />
            Auto-block high-confidence IPs
          </label>

          <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="ip-auto-confidence">Minimum confidence</Label>
              <Input
                id="ip-auto-confidence"
                type="number"
                min={70}
                max={100}
                step={1}
                value={policy.AutoBlockMinConfidence}
                disabled={!policy.AutoBlockEnabled || loading || saving}
                onChange={(event) => {
                  setSaved(false);
                  setPolicy((current) => ({
                    ...current,
                    AutoBlockMinConfidence: Math.max(70, Math.min(100, Number(event.target.value) || 70)),
                  }));
                }}
              />
            </div>

            <SelectField
              id="ip-default-scope"
              label="Default scope"
              value={policy.DefaultIPBlockScope}
              disabled={loading || saving}
              onChange={(event) => {
                setSaved(false);
                setPolicy((current) => ({
                  ...current,
                  DefaultIPBlockScope: event.target.value as IPResponsePolicyDraft['DefaultIPBlockScope'],
                }));
              }}
            >
              <option value="affected">Affected nodes</option>
              <option value="fleet">Fleet</option>
            </SelectField>

            <SelectField
              id="ip-default-ttl"
              label="Block for"
              value={String(policy.DefaultIPBlockTTLSeconds)}
              disabled={loading || saving}
              onChange={(event) => {
                setSaved(false);
                setPolicy((current) => ({
                  ...current,
                  DefaultIPBlockTTLSeconds: Number(event.target.value) as IPResponsePolicyDraft['DefaultIPBlockTTLSeconds'],
                }));
              }}
            >
              <option value="900">15 minutes</option>
              <option value="3600">1 hour</option>
              <option value="86400">24 hours</option>
            </SelectField>
          </div>

          <label className="flex items-center gap-3 text-sm text-text-secondary">
            <input
              type="checkbox"
              checked={policy.RequireCorroboratingThreatIntel}
              disabled={!policy.AutoBlockEnabled || loading || saving}
              onChange={(event) => {
                setSaved(false);
                setPolicy((current) => ({ ...current, RequireCorroboratingThreatIntel: event.target.checked }));
              }}
              className="h-4 w-4 rounded border-border"
            />
            Require threat-intel match
          </label>

          <p className="text-xs text-text-muted">
            Manual blocks use the same default scope and duration. Protected targets and enforcement safety limits still apply.
          </p>

          {error ? <p role="alert" className="text-sm text-state-critical">{error}</p> : null}
          {saved ? <p className="text-sm text-state-healthy">Saved</p> : null}

          <div>
            <Button type="button" size="sm" onClick={() => void save()} disabled={loading || saving}>
              {saving ? 'Saving...' : 'Save'}
            </Button>
          </div>
        </div>
      )}
    </Panel>
  );
}
