import { useCallback, useEffect, useState } from 'react';
import { RefreshCw, ShieldCheck, Clock3, AlertTriangle } from 'lucide-react';
import { Button } from '../../components/ui/button';
import { useApiClient } from '../../hooks/useApiClient';
import type { GlobalRoleSourceEvidence } from '../../lib/api';

export function GlobalRoleSources({userID, onReviewed}: {userID: string; onReviewed: () => void}) {
  const api = useApiClient();
  const [evidence, setEvidence] = useState<GlobalRoleSourceEvidence | null>(null);
  const [reason, setReason] = useState('');
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [busy, setBusy] = useState(false);
  const [deadline, setDeadline] = useState('');
  const [manualDeadline, setManualDeadline] = useState(false);
  const [change, setChange] = useState<{assignment: string; role: string; revision: string; action: 'set_expiry' | 'revoke'; reason: string; expires_at?: string} | null>(null);
  const load = useCallback(async () => {
    setBusy(true); setError(''); setChange(null); setSuccess('');
    try { setEvidence(await api.getGlobalRoleSources(userID)); }
    catch { setEvidence(null); setError('Role source evidence is unavailable. Refresh before reviewing.'); }
    finally { setBusy(false); }
  }, [api, userID]);
  useEffect(() => { void load(); }, [load]);
  useEffect(() => {
    if (!evidence) return;
    const next = evidence.grants.map(g => g.expires_at ? Date.parse(g.expires_at) : NaN).filter(t => Number.isFinite(t) && t > Date.now()).sort((a,b) => a-b)[0];
    if (next === undefined) return;
    const timer = window.setTimeout(() => { void load(); }, Math.min(next - Date.now() + 100, 2147483647));
    return () => window.clearTimeout(timer);
  }, [evidence, load]);

  const review = async (assignment: string) => {
    if (!evidence || busy) return;
    setBusy(true); setError('');
    try {
      setEvidence(await api.reviewGlobalRoleSource(userID, {assignment_id: assignment, expected_revision: evidence.revision, reason: reason.trim()}));
      setReason(''); setSuccess('Administrator ownership recorded. The role and expiry are unchanged.'); onReviewed();
    } catch (err) { setEvidence(null); setError(err instanceof Error ? err.message : 'Review failed. Refresh and review again.'); }
    finally { setBusy(false); }
  };
  const prepareChange = (assignment: string, role: string, action: 'set_expiry' | 'revoke') => {
    if (!evidence || busy || reason.trim().length < 10) return;
    const expires = deadline ? new Date(deadline) : null;
    if (action === 'set_expiry' && (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(deadline) || !expires || !Number.isFinite(expires.getTime()) || expires.getTime() <= Date.now())) { setError('Select a future local deadline.'); return; }
    setError(''); setChange({assignment,role,revision:evidence.revision,action,reason:reason.trim(),expires_at:action === 'set_expiry' ? expires!.toISOString() : undefined});
  };
  const applyChange = async () => {
    if (!change || busy) return;
    setBusy(true); setError('');
    try {
      setEvidence(await api.changeAdministratorRoleSource(userID, {assignment_id:change.assignment,expected_revision:change.revision,reason:change.reason,action:change.action,expires_at:change.expires_at}));
      setChange(null); setReason(''); setSuccess('Administrator grant changed. Directory grants were preserved.'); onReviewed();
    } catch(err) { setChange(null); setEvidence(null); setError(err instanceof Error ? err.message : 'Change failed. Refresh and review again.'); }
    finally { setBusy(false); }
  };
  return <section aria-label="Global role sources" aria-busy={busy} className="border-b border-border-subtle bg-surface/40 p-5 space-y-5">
    <div className="flex items-start justify-between gap-4">
      <div className="min-w-0">
        <h3 className="flex items-center gap-2 text-base font-semibold text-foreground"><ShieldCheck className="size-4 text-brand-500" aria-hidden="true" />Global role sources</h3>
        <p className="mt-1 text-xs leading-relaxed text-text-secondary">See who manages each grant and review changes before applying.</p>
      </div>
      <Button type="button" variant="outline" size="sm" aria-label="Refresh role sources" disabled={busy} onClick={() => void load()}>
        <RefreshCw className={busy ? 'animate-spin' : ''} aria-hidden="true" /><span>Refresh</span>
      </Button>
    </div>
    {error && <p role="alert" className="rounded-lg border border-state-critical/30 bg-state-critical/10 p-3 text-sm text-state-critical">{error}</p>}
    {success && <p role="status" className="rounded-lg border border-state-healthy/30 bg-state-healthy/10 p-3 text-sm text-foreground">{success}</p>}
    {evidence && <>
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <span className="text-text-secondary">Effective global roles:</span>
        {evidence.effective_roles.length ? evidence.effective_roles.map(role => <span key={role} className="rounded-full border border-brand-500/25 bg-brand-500/10 px-2.5 py-1 font-medium text-foreground">{role}</span>) : <span className="text-text-secondary">None</span>}
      </div>
      <div className="rounded-xl border border-border-subtle bg-elevated p-4 space-y-4">
        <label className="block text-xs font-medium text-foreground">Review reason
          <textarea aria-label="Role source review reason" rows={2} maxLength={2000} value={reason} onChange={e => setReason(e.target.value)} placeholder="Explain the authorization or change being reviewed…" className="mt-1.5 block w-full resize-y rounded-lg border border-border-subtle bg-surface p-2.5 text-sm text-foreground placeholder:text-text-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500" />
          <span className="mt-1 block font-normal text-text-secondary">At least 10 characters. Retained with your identity in the audit record.</span>
        </label>
        {evidence.administrator_changes_available && evidence.grants.some(g => g.source === 'administrator') && <div>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <label htmlFor={`source-expiry-${userID}`} className="text-xs font-medium text-foreground">Administrator expiry <span className="font-normal text-text-secondary">· local time</span></label>
            <Button type="button" variant="link" size="sm" className="text-xs" disabled={busy || !!change} onClick={() => { setManualDeadline(!manualDeadline); setDeadline(''); }}>{manualDeadline ? 'Use date picker' : 'Enter deadline as text'}</Button>
          </div>
          <input id={`source-expiry-${userID}`} aria-label="Administrator grant expiry" type={manualDeadline ? 'text' : 'datetime-local'} placeholder="YYYY-MM-DDTHH:mm" value={deadline} onChange={e => setDeadline(e.target.value)} className="mt-1.5 block w-full rounded-lg border border-border-subtle bg-surface p-2.5 text-sm text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500" />
          <p className="mt-1 text-xs text-text-secondary">{manualDeadline ? 'Use YYYY-MM-DDTHH:mm. ' : ''}The exact UTC deadline appears in the review.</p>
        </div>}
      </div>
      <ul className="space-y-3">{evidence.grants.map(g => <li key={`${g.assignment_id}-${g.source}`} className="rounded-xl border border-border-subtle bg-elevated p-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <strong className="text-sm text-foreground">{g.role}</strong>
          {g.expires_at && Date.parse(g.expires_at) <= Date.now() && <span className="rounded-full bg-state-critical/10 px-2.5 py-1 text-[11px] font-medium text-state-critical">Expired</span>}
          <span className={`rounded-full px-2.5 py-1 text-[11px] font-medium ${g.source === 'legacy_unknown' ? 'bg-state-warning/15 text-foreground' : 'bg-brand-500/10 text-foreground'}`}>{g.source === 'legacy_unknown' ? 'Unknown origin — review required' : g.source}</span>
        </div>
        <p className="mt-2 flex items-center gap-1.5 text-xs text-text-secondary"><Clock3 className="size-3.5" aria-hidden="true" />{g.expires_at ? `Expires ${new Date(g.expires_at).toLocaleString()}` : 'No expiry'}</p>
        {g.source === 'administrator' && evidence.administrator_changes_available && <div className="mt-3 flex flex-wrap gap-2 border-t border-border-subtle pt-3">
          <Button type="button" variant="outline" size="sm" disabled={busy || !!change || reason.trim().length < 10} onClick={() => prepareChange(g.assignment_id,g.role,'set_expiry')}>Review expiry for {g.role}</Button>
          <Button type="button" variant="ghost" size="sm" className="text-state-critical" disabled={busy || !!change || reason.trim().length < 10} onClick={() => prepareChange(g.assignment_id,g.role,'revoke')}>Review revoke {g.role}</Button>
        </div>}
        {g.source === 'legacy_unknown' && <div className="mt-3 space-y-2 border-t border-border-subtle pt-3">
          <p className="text-xs leading-relaxed text-text-secondary">Administrator ownership preserves this grant’s role and deadline. Record the authorization in your reason.</p>
          <Button type="button" variant="outline" size="sm" className="h-auto min-h-8 whitespace-normal text-left" disabled={busy || reason.trim().length < 10} onClick={() => void review(g.assignment_id)}>Record administrator ownership of {g.role}</Button>
        </div>}
      </li>)}</ul>
      {!evidence.grants.length && <p className="rounded-lg border border-dashed border-border-subtle p-4 text-center text-sm text-text-secondary">No global grants recorded.</p>}
      {change && <div role="group" aria-label="Review administrator grant change" className="rounded-xl border border-brand-500/40 bg-brand-500/5 p-4 space-y-3">
        <h4 className="flex items-center gap-2 text-sm font-semibold"><AlertTriangle className="size-4" aria-hidden="true" />Confirm reviewed change</h4>
        <p className="text-sm">{change.action === 'revoke' ? 'Revoke' : 'Set expiry for'} administrator grant: <strong>{change.role}</strong></p>
        {change.expires_at && <p className="break-all text-xs text-text-secondary">Exact deadline: {change.expires_at}</p>}
        <p className="break-words text-sm">Reason: {change.reason}</p>
        <div className="flex flex-wrap gap-2">
          <Button type="button" size="sm" className="h-auto min-h-8 whitespace-normal" disabled={busy} onClick={() => void applyChange()}>Apply reviewed administrator grant change</Button>
          <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => setChange(null)}>Cancel grant change</Button>
        </div>
      </div>}
      <p className="text-xs leading-relaxed text-text-secondary">Directory and authentication grants are managed by their identity providers. Provisioning grants require origin review. Changes here apply only to administrator-owned grants.</p>
    </>}
  </section>;
}
