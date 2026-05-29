import { useState } from 'react';
import {
  ArrowRight,
  Clock3,
  ExternalLink,
  FileCheck2,
  MoreHorizontal,
  RotateCcw,
  Shield,
  ShieldOff,
} from 'lucide-react';
import { Link } from 'react-router-dom';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '../ui/dropdown-menu';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../ui/dialog';
import { Button } from '../ui/button';
import { useApiClient } from '../../hooks/useApiClient';
import { useTenant } from '../../providers/TenantProvider';
import { entityRoute } from '../../lib/entity';
import { toast } from 'sonner';
import { StatusTag } from './StatusTag';
import type { StateTone } from './types';

// Consolidated IP response menu used across investigation, alerts, and network
// tables. It opens a review dialog before dispatching firewall work so scope,
// TTL, receipt, and rollback expectations are visible before enforcement.
export interface IpActionMenuProps {
  ip: string;
  /** Optional override for the trigger element. Defaults to a small icon button. */
  trigger?: React.ReactNode;
  onActionTaken?: () => void;
  showInvestigateLink?: boolean;
  showCopyAction?: boolean;
}

type IpResponseIntent = {
  id: 'block-affected' | 'block-fleet' | 'allow';
  action: 'block' | 'allow';
  scope: 'affected' | 'fleet';
  title: string;
  menuLabel: string;
  description: string;
  safetyClass: string;
  ttlSeconds?: number;
  ttlLabel: string;
  tone: StateTone;
  confirmLabel: string;
};

const RESPONSE_INTENTS: IpResponseIntent[] = [
  {
    id: 'block-affected',
    action: 'block',
    scope: 'affected',
    title: 'Review affected-node block',
    menuLabel: 'Review affected-node block',
    description: 'Dispatch only to nodes that have observed this IP. Use this before fleet-wide containment when the blast radius is known.',
    safetyClass: 'Narrow containment',
    ttlSeconds: 86400,
    ttlLabel: '24h TTL',
    tone: 'warning',
    confirmLabel: 'Dispatch affected-node block',
  },
  {
    id: 'block-fleet',
    action: 'block',
    scope: 'fleet',
    title: 'Review fleet-wide block',
    menuLabel: 'Review fleet-wide block',
    description: 'Dispatch to every enrolled node in this tenant. Reserve for high-confidence threats, blacklist hits, or active spread across groups.',
    safetyClass: 'Broad containment',
    ttlSeconds: 86400,
    ttlLabel: '24h TTL',
    tone: 'critical',
    confirmLabel: 'Dispatch fleet-wide block',
  },
  {
    id: 'allow',
    action: 'allow',
    scope: 'affected',
    title: 'Review unblock',
    menuLabel: 'Review unblock',
    description: 'Remove the block from affected nodes and keep the audit trail. Use after evidence shows the source is approved or the block is stale.',
    safetyClass: 'Rollback',
    ttlLabel: 'Immediate removal',
    tone: 'info',
    confirmLabel: 'Dispatch unblock',
  },
];

export function IpActionMenu({
  ip,
  trigger,
  onActionTaken,
  showInvestigateLink = true,
  showCopyAction = true,
}: IpActionMenuProps): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [busy, setBusy] = useState<null | IpResponseIntent['id']>(null);
  const [reviewIntent, setReviewIntent] = useState<IpResponseIntent | null>(null);

  const dispatch = async (intent: IpResponseIntent) => {
    if (!currentTenantId) {
      toast.error('Select a tenant first');
      return;
    }
    setBusy(intent.id);
    try {
      const resp = await client.entityAction(
        'ip',
        ip,
        {
          action: intent.action,
          scope: intent.scope,
          ttl: intent.ttlSeconds,
          reason: governedReason(intent),
        },
        { tenantId: currentTenantId },
      );
      const dispatched = resp.nodes_dispatched ?? 0;
      const verb = intent.action === 'block' ? 'Block' : 'Unblock';
      if (dispatched === 0) {
        toast.warning(`${verb} ${ip}: no nodes affected`, {
          description: intent.scope === 'affected'
            ? 'No traffic seen for this IP in the last 7 days. Try fleet-wide scope.'
            : 'Tenant has no enrolled nodes.',
        });
      } else {
        toast.success(`${verb} ${ip}: dispatched to ${dispatched} node${dispatched === 1 ? '' : 's'}`, {
          description: 'Outcome will appear in Active Blocks once the agents report back.',
        });
      }
      setReviewIntent(null);
      onActionTaken?.();
    } catch (err) {
      toast.error(`Failed: ${err instanceof Error ? err.message : 'unknown'}`);
    } finally {
      setBusy(null);
    }
  };

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          {trigger ?? (
            <Button variant="ghost" size="icon" className="h-7 w-7" aria-label={`Actions for ${ip}`}>
              <MoreHorizontal className="h-4 w-4" />
            </Button>
          )}
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-72">
          <DropdownMenuLabel className="flex flex-col gap-1">
            <span className="font-mono text-xs uppercase tracking-wider">{ip}</span>
            <span className="text-[0.7rem] normal-case tracking-normal text-text-muted">
              Governed response: review scope, TTL, receipts, and rollback before dispatch.
            </span>
          </DropdownMenuLabel>
          <DropdownMenuSeparator />
          {RESPONSE_INTENTS.map((intent) => (
            <DropdownMenuItem
              key={intent.id}
              disabled={!!busy}
              onClick={() => setReviewIntent(intent)}
            >
              {intent.action === 'allow' ? <ShieldOff className="mr-2 h-4 w-4" /> : <Shield className="mr-2 h-4 w-4" />}
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span>{intent.menuLabel}</span>
                <span className="text-[0.68rem] text-text-muted">{intent.scope} / {intent.ttlLabel}</span>
              </span>
            </DropdownMenuItem>
          ))}
          <DropdownMenuSeparator />
          <DropdownMenuItem asChild>
            <Link to="/security/network?tab=blocks" className="flex items-center">
              <FileCheck2 className="mr-2 h-4 w-4" />
              Active block receipts
            </Link>
          </DropdownMenuItem>
          {(showInvestigateLink || showCopyAction) && <DropdownMenuSeparator />}
          {showInvestigateLink && (
            <DropdownMenuItem asChild>
              <Link to={entityRoute('ip', ip)} className="flex items-center">
                <ExternalLink className="mr-2 h-4 w-4" />
                View in Investigate
              </Link>
            </DropdownMenuItem>
          )}
          {showCopyAction && (
            <DropdownMenuItem onClick={() => navigator.clipboard.writeText(ip)}>
              Copy IP
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>

      <Dialog open={!!reviewIntent} onOpenChange={(open) => !open && setReviewIntent(null)}>
        {reviewIntent && (
          <DialogContent className="max-w-xl">
            <DialogHeader>
              <DialogTitle>{reviewIntent.title}</DialogTitle>
              <DialogDescription>
                {reviewIntent.description} The action is not considered remediated until agent receipts and audit evidence are visible.
              </DialogDescription>
            </DialogHeader>

            <div className="grid gap-3 sm:grid-cols-2">
              <ReviewFact label="Target" value={ip} tone="info" />
              <ReviewFact label="Scope" value={scopeLabel(reviewIntent.scope)} tone={reviewIntent.scope === 'fleet' ? 'critical' : 'warning'} />
              <ReviewFact label="TTL" value={reviewIntent.ttlLabel} tone="info" icon={<Clock3 className="h-3.5 w-3.5" />} />
              <ReviewFact label="Safety class" value={reviewIntent.safetyClass} tone={reviewIntent.tone} />
            </div>

            <div className="rounded-md border border-border-subtle bg-surface p-3">
              <p className="mb-2 font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">Required before closure</p>
              <ul className="space-y-2 text-sm text-text-secondary">
                <li className="flex gap-2">
                  <FileCheck2 className="mt-0.5 h-4 w-4 shrink-0 text-brand-400" />
                  Active Blocks must show node receipts, pending or failed fan-out, and final enforcement status.
                </li>
                <li className="flex gap-2">
                  <RotateCcw className="mt-0.5 h-4 w-4 shrink-0 text-brand-400" />
                  Rollback path is explicit: dispatch unblock and verify removal receipts before closing the case.
                </li>
                <li className="flex gap-2">
                  <ArrowRight className="mt-0.5 h-4 w-4 shrink-0 text-brand-400" />
                  Any fleet-wide action should be backed by blacklist, confidence, or cross-group evidence.
                </li>
              </ul>
            </div>

            <DialogFooter>
              <DialogClose asChild>
                <Button variant="ghost" size="sm" type="button">Cancel</Button>
              </DialogClose>
              <Button
                type="button"
                variant={reviewIntent.tone === 'critical' ? 'danger' : 'primary'}
                size="sm"
                loading={busy === reviewIntent.id}
                onClick={() => void dispatch(reviewIntent)}
              >
                {reviewIntent.confirmLabel}
              </Button>
            </DialogFooter>
          </DialogContent>
        )}
      </Dialog>
    </>
  );
}

function ReviewFact({
  label,
  value,
  tone,
  icon,
}: {
  label: string;
  value: string;
  tone: StateTone;
  icon?: JSX.Element;
}): JSX.Element {
  return (
    <div className="rounded-md border border-border-subtle bg-surface p-3">
      <div className="mb-1 font-mono text-[0.62rem] uppercase tracking-wider text-text-muted">{label}</div>
      <StatusTag tone={tone} icon={icon}>{value}</StatusTag>
    </div>
  );
}

function scopeLabel(scope: IpResponseIntent['scope']): string {
  return scope === 'fleet' ? 'Fleet-wide' : 'Affected nodes';
}

function governedReason(intent: IpResponseIntent): string {
  const ttl = intent.ttlSeconds ? `ttl=${intent.ttlSeconds}s` : 'ttl=none';
  return [
    `Governed IP response: ${intent.title}`,
    `scope=${intent.scope}`,
    ttl,
    `safety_class=${intent.safetyClass.toLowerCase().replace(/\s+/g, '_')}`,
    'receipt_required=true',
    'rollback_required=true',
  ].join('; ');
}
