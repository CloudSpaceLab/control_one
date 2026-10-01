import { useCallback, useState } from 'react';
import {
  ExternalLink,
  FileCheck2,
  MoreHorizontal,
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
import { Button } from '../ui/button';
import { useApiClient } from '../../hooks/useApiClient';
import { useTenant } from '../../providers/TenantProvider';
import { entityRoute } from '../../lib/entity';
import { toast } from 'sonner';
import { StatusTag } from './StatusTag';
import type { IPBlockStatus, TenantRemediationConfig } from '../../lib/api';
import type { StateTone } from './types';

export interface IpActionMenuProps {
  ip: string;
  /** Optional override for the trigger element. Defaults to a small icon button. */
  trigger?: React.ReactNode;
  onActionTaken?: () => void;
  showInvestigateLink?: boolean;
  showCopyAction?: boolean;
}

type BlockScope = 'affected' | 'fleet';

type IpResponseIntent = {
  id: 'block-default' | 'block-affected' | 'block-fleet' | 'allow';
  action: 'block' | 'allow';
  scope?: BlockScope;
  ttlSeconds?: number;
  reason: string;
};

const ALLOW: IpResponseIntent = {
  id: 'allow',
  action: 'allow',
  reason: 'Manual IP allow',
};

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
  const [status, setStatus] = useState<IPBlockStatus | null>(null);
  const [policy, setPolicy] = useState<TenantRemediationConfig | null>(null);
  const [stateLoading, setStateLoading] = useState(false);
  const [statusError, setStatusError] = useState(false);

  const loadState = useCallback(async () => {
    if (!currentTenantId) {
      setStatus(null);
      setPolicy(null);
      return;
    }
    setStateLoading(true);
    setStatusError(false);
    const [statusResult, policyResult] = await Promise.allSettled([
      client.getIPBlockStatus(ip, currentTenantId),
      client.getTenantRemediationConfig(currentTenantId),
    ]);
    if (statusResult.status === 'fulfilled') {
      setStatus(statusResult.value);
    } else {
      setStatusError(true);
    }
    if (policyResult.status === 'fulfilled') {
      setPolicy(policyResult.value);
    }
    setStateLoading(false);
  }, [client, currentTenantId, ip]);

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
          reason: intent.reason,
        },
        { tenantId: currentTenantId },
      );
      const dispatched = resp.nodes_dispatched ?? 0;
      if (dispatched === 0) {
        toast.warning(intent.action === 'block' ? 'No nodes matched this block' : 'No active block found');
      } else {
        toast.success(
          `${intent.action === 'block' ? 'Block' : 'Allow'} queued · ${dispatched} node${dispatched === 1 ? '' : 's'}`,
        );
      }
      await loadState();
      onActionTaken?.();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'IP action failed');
    } finally {
      setBusy(null);
    }
  };

  const defaultScope = policy?.DefaultIPBlockScope;
  const defaultTTL = policy?.DefaultIPBlockTTLSeconds;
  const defaultBlock: IpResponseIntent = {
    id: 'block-default',
    action: 'block',
    scope: defaultScope,
    ttlSeconds: defaultTTL,
    reason: 'Manual IP block',
  };
  const alternateScope: BlockScope | null = policy
    ? (defaultScope === 'fleet' ? 'affected' : 'fleet')
    : null;
  const alternateBlock: IpResponseIntent | null = alternateScope
    ? {
        id: alternateScope === 'fleet' ? 'block-fleet' : 'block-affected',
        action: 'block',
        scope: alternateScope,
        ttlSeconds: defaultTTL,
        reason: alternateScope === 'fleet' ? 'Manual fleet-wide IP block' : 'Manual affected-node IP block',
      }
    : null;
  const extendFleet: IpResponseIntent = {
    id: 'block-fleet',
    action: 'block',
    scope: 'fleet',
    ttlSeconds: defaultTTL,
    reason: 'Manual fleet-wide IP block',
  };

  const unblockInProgress = status?.state === 'unblocking';
  const effectiveBlocked = !!status?.active && status.state !== 'failed';

  return (
    <DropdownMenu onOpenChange={(open) => {
      if (open) void loadState();
    }}>
      <DropdownMenuTrigger asChild>
        {trigger ?? (
          <Button variant="ghost" size="icon" className="h-7 w-7" aria-label={`Actions for ${ip}`}>
            <MoreHorizontal className="h-4 w-4" />
          </Button>
        )}
      </DropdownMenuTrigger>

      <DropdownMenuContent align="end" className="w-72">
        <DropdownMenuLabel className="space-y-2">
          <div className="flex items-center justify-between gap-3">
            <span className="truncate font-mono text-xs">{ip}</span>
            {!stateLoading && status && (
              <StatusTag tone={blockStatusTone(status)}>
                {blockStatusLabel(status)}
              </StatusTag>
            )}
          </div>
          <div className="text-[0.7rem] font-normal normal-case tracking-normal text-text-muted">
            {stateLoading
              ? 'Checking block status…'
              : statusError
                ? 'Block status unavailable'
                : status
                  ? blockStatusDetail(status)
                  : 'Select a tenant to view block status'}
          </div>
        </DropdownMenuLabel>

        <DropdownMenuSeparator />

        {unblockInProgress ? (
          <DropdownMenuItem disabled>
            <ShieldOff className="mr-2 h-4 w-4" />
            <span>Allow in progress</span>
          </DropdownMenuItem>
        ) : !effectiveBlocked ? (
          <>
            <DropdownMenuItem disabled={!!busy} onClick={() => void dispatch(defaultBlock)}>
              <Shield className="mr-2 h-4 w-4" />
              <span className="flex min-w-0 flex-1 items-center justify-between gap-3">
                <span>Block IP</span>
                <span className="text-[0.68rem] text-text-muted">
                  {policy ? `${scopeShortLabel(defaultScope!)} · ${ttlLabel(defaultTTL)}` : 'Tenant default'}
                </span>
              </span>
            </DropdownMenuItem>
            {alternateBlock && (
              <DropdownMenuItem disabled={!!busy} onClick={() => void dispatch(alternateBlock)}>
                <Shield className="mr-2 h-4 w-4" />
                <span className="flex min-w-0 flex-1 items-center justify-between gap-3">
                  <span>{alternateScope === 'fleet' ? 'Block fleet-wide' : 'Block affected nodes'}</span>
                  <span className="text-[0.68rem] text-text-muted">{ttlLabel(defaultTTL)}</span>
                </span>
              </DropdownMenuItem>
            )}
          </>
        ) : (
          <>
            <DropdownMenuItem disabled={!!busy} onClick={() => void dispatch(ALLOW)}>
              <ShieldOff className="mr-2 h-4 w-4" />
              <span>Allow IP</span>
            </DropdownMenuItem>
            {status?.scope !== 'fleet' && (
              <DropdownMenuItem disabled={!!busy} onClick={() => void dispatch(extendFleet)}>
                <Shield className="mr-2 h-4 w-4" />
                <span className="flex min-w-0 flex-1 items-center justify-between gap-3">
                  <span>Extend to fleet</span>
                  <span className="text-[0.68rem] text-text-muted">{policy ? ttlLabel(defaultTTL) : 'Tenant default'}</span>
                </span>
              </DropdownMenuItem>
            )}
          </>
        )}

        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <Link to="/security/network?tab=blocks" className="flex items-center">
            <FileCheck2 className="mr-2 h-4 w-4" />
            View block details
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
  );
}

function blockStatusLabel(status: IPBlockStatus): string {
  switch (status.state) {
    case 'blocking':
      return 'Blocking';
    case 'blocked':
      return 'Blocked';
    case 'unblocking':
      return 'Unblocking';
    case 'partial':
      return 'Partial';
    case 'failed':
      return 'Failed';
    case 'unblocked':
    default:
      return 'Not blocked';
  }
}

function blockStatusTone(status: IPBlockStatus): StateTone {
  switch (status.state) {
    case 'blocked':
      return 'healthy';
    case 'blocking':
    case 'unblocking':
      return 'warning';
    case 'partial':
    case 'failed':
      return 'critical';
    case 'unblocked':
    default:
      return 'unknown';
  }
}

function blockStatusDetail(status: IPBlockStatus): string {
  if (!status.active) return 'No active Control One block';
  const provenance = status.provenance === 'auto' ? 'Auto-blocked' : 'Manually blocked';
  const scope = status.scope === 'fleet' ? 'Fleet-wide' : 'Affected nodes';
  const coverage = `${status.nodes_applied}/${status.target_nodes} applied`;
  const pending = status.nodes_pending > 0 ? ` · ${status.nodes_pending} pending` : '';
  const removing = status.nodes_removing > 0 ? ` · ${status.nodes_removing} removing` : '';
  const failed = status.nodes_failed > 0 ? ` · ${status.nodes_failed} failed` : '';
  return `${provenance} · ${scope} · ${coverage}${pending}${removing}${failed}`;
}

function scopeShortLabel(scope: BlockScope): string {
  return scope === 'fleet' ? 'Fleet' : 'Affected';
}

function ttlLabel(seconds?: number): string {
  switch (seconds) {
    case 900:
      return '15m';
    case 86400:
      return '24h';
    case 3600:
    default:
      return '1h';
  }
}
