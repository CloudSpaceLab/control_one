import { useState } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { X, CheckCircle2, AlertCircle } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { SelectField } from '@/components/kit';
import { StatusTag } from '@/components/kit';
import { useApiClient } from '../hooks/useApiClient';
import type { RulePack } from '../lib/rulePacks';

interface ApplyPackModalProps {
  pack: RulePack | null;
  onClose: () => void;
  tenants: Array<{ id: string; name: string }>;
  defaultTenantId?: string;
  onApplied?: () => void;
}

export function ApplyPackModal({
  pack,
  onClose,
  tenants,
  defaultTenantId,
  onApplied,
}: ApplyPackModalProps): JSX.Element {
  const client = useApiClient();
  const [tenantId, setTenantId] = useState<string>(defaultTenantId ?? '');
  const [applying, setApplying] = useState(false);
  const [results, setResults] = useState<Record<string, 'ok' | 'error'>>({});

  const totalRules = (pack?.portRules.length ?? 0) + (pack?.logRules.length ?? 0);
  const errorCount = Object.values(results).filter((v) => v === 'error').length;
  const allApplied = totalRules > 0 && Object.keys(results).length === totalRules;

  const handleApply = async () => {
    if (!tenantId || !pack) return;
    setApplying(true);
    const newResults: Record<string, 'ok' | 'error'> = {};

    for (let i = 0; i < pack.portRules.length; i++) {
      const r = pack.portRules[i];
      try {
        await client.createPortRule({ ...r, tenant_id: tenantId, action: 'notify', enabled: true });
        newResults[`port-${i}`] = 'ok';
      } catch {
        newResults[`port-${i}`] = 'error';
      }
      setResults({ ...newResults });
    }

    for (let i = 0; i < pack.logRules.length; i++) {
      const r = pack.logRules[i];
      try {
        await client.createLogRule({ ...r, tenant_id: tenantId, action: 'notify', enabled: true });
        newResults[`log-${i}`] = 'ok';
      } catch {
        newResults[`log-${i}`] = 'error';
      }
      setResults({ ...newResults });
    }

    setApplying(false);
    const errors = Object.values(newResults).filter((v) => v === 'error').length;
    if (errors === 0) {
      toast.success(`Applied ${pack.portRules.length + pack.logRules.length} rules for ${pack.name}`);
      onApplied?.();
    } else {
      toast.error(`${errors} rules failed to apply`);
    }
  };

  return (
    <Dialog.Root
      open={!!pack}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-black/60 backdrop-blur-sm" />
        <Dialog.Content aria-describedby={undefined} className="fixed left-1/2 top-1/2 z-50 w-full max-w-2xl -translate-x-1/2 -translate-y-1/2 rounded-xl border border-border-subtle bg-elevated shadow-2xl overflow-y-auto max-h-[90vh]">
          {/* Header */}
          <div className="flex items-start justify-between gap-4 border-b border-border-subtle p-6">
            <div>
              <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                {pack?.category}
              </p>
              <Dialog.Title className="mt-0.5 font-display text-xl font-semibold text-foreground">
                {pack?.name}
              </Dialog.Title>
              <p className="mt-1 text-sm text-text-secondary">{pack?.description}</p>
            </div>
            <Dialog.Close asChild>
              <Button variant="ghost" size="icon">
                <X className="h-4 w-4" />
              </Button>
            </Dialog.Close>
          </div>

          {/* Body */}
          <div className="flex flex-col gap-5 p-6">
            {/* Tenant selector */}
            <SelectField
              id="apply-pack-tenant"
              label="Apply to tenant"
              value={tenantId}
              onChange={(e) => setTenantId(e.target.value)}
            >
              <option value="">Select tenant…</option>
              {tenants.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </SelectField>

            {/* Port rules preview */}
            {pack && pack.portRules.length > 0 && (
              <div>
                <p className="mb-2 font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                  Port rules ({pack.portRules.length})
                </p>
                <div className="flex flex-col gap-2">
                  {pack.portRules.map((r, i) => (
                    <div
                      key={i}
                      className="flex items-center gap-3 rounded-md border border-border-subtle bg-surface px-3 py-2"
                    >
                      {results[`port-${i}`] === 'ok' && (
                        <CheckCircle2 className="h-4 w-4 shrink-0 text-state-healthy" />
                      )}
                      {results[`port-${i}`] === 'error' && (
                        <AlertCircle className="h-4 w-4 shrink-0 text-state-critical" />
                      )}
                      {!results[`port-${i}`] && <span className="h-4 w-4 shrink-0" />}
                      <code className="font-mono text-xs text-foreground">
                        {r.protocol.toUpperCase()} :{r.port}
                      </code>
                      <span className="text-xs text-text-secondary">{r.name}</span>
                      <StatusTag
                        tone={r.expected_state === 'open' ? 'info' : 'warning'}
                        className="ml-auto"
                      >
                        {r.expected_state}
                      </StatusTag>
                      <StatusTag
                        tone={
                          r.severity === 'critical'
                            ? 'critical'
                            : r.severity === 'high'
                              ? 'warning'
                              : 'info'
                        }
                      >
                        {r.severity}
                      </StatusTag>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* Log rules preview */}
            {pack && pack.logRules.length > 0 && (
              <div>
                <p className="mb-2 font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                  Log rules ({pack.logRules.length})
                </p>
                <div className="flex flex-col gap-2">
                  {pack.logRules.map((r, i) => (
                    <div
                      key={i}
                      className="flex flex-col gap-1 rounded-md border border-border-subtle bg-surface px-3 py-2"
                    >
                      <div className="flex items-center gap-3">
                        {results[`log-${i}`] === 'ok' && (
                          <CheckCircle2 className="h-4 w-4 shrink-0 text-state-healthy" />
                        )}
                        {results[`log-${i}`] === 'error' && (
                          <AlertCircle className="h-4 w-4 shrink-0 text-state-critical" />
                        )}
                        {!results[`log-${i}`] && <span className="h-4 w-4 shrink-0" />}
                        <span className="text-sm font-medium text-foreground">{r.name}</span>
                        <StatusTag
                          tone={
                            r.severity === 'critical'
                              ? 'critical'
                              : r.severity === 'high'
                                ? 'warning'
                                : 'info'
                          }
                          className="ml-auto"
                        >
                          {r.severity}
                        </StatusTag>
                      </div>
                      <code
                        className="ml-8 font-mono text-[0.7rem] text-text-secondary truncate"
                        title={r.pattern}
                      >
                        {r.log_source}: {r.pattern}
                      </code>
                      <p className="ml-8 text-xs text-text-muted">
                        {r.threshold}× in {r.window_seconds}s
                      </p>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* Error summary */}
            {errorCount > 0 && (
              <p className="text-sm text-state-critical">
                {errorCount} rule(s) failed to apply. Others were created successfully.
              </p>
            )}
          </div>

          {/* Footer */}
          <div className="flex items-center justify-end gap-3 border-t border-border-subtle p-6">
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button
              variant="primary"
              size="lg"
              loading={applying}
              disabled={!tenantId || applying || allApplied}
              onClick={handleApply}
            >
              {allApplied ? 'Applied ✓' : `Apply ${totalRules} rules`}
            </Button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
