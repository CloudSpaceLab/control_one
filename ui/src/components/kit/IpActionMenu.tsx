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
import type { IPBlockStatus } from '../../lib/api';
import type { StateTone } from './types';

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
  scope?: 'affected' | 'fleet';
  ttlSeconds?: number;
  label: string;
  reason: string;
};

const BLOCK_AFFECTED: IpResponseIntent = {
  id: 'block-affected',
  action: 'block',
  scope: 'affected',
  ttlSeconds: 86400,
  label: 'Block IP',
  reason: 'Manual IP block',
};

const BLOCK_FLEET: IpResponseIntent = {
  id: 'block-fleet',
  action: 'block',
  scope: 'fleet',
  ttlSeconds: 86400,
  label: 'Block fleet-wide',
  reason: 'Manual fleet-wide IP block',
};

const ALLOW: IpResponseIntent = {
  id: 'allow',
  action: 'allow',
  label: 'Allow IP',
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
  const [statusLoading, setStatusLoading] = useState(false);
  const [statusError, setStatusError] = useState(false);

  const loadStatus = useCallback(async () => {
    if (!currentTenantId) {
      setStatus(null);
      return;
    }
    setStatusLoading(true);
    setStatusError(false);
    try {
      setStatus(await client.getIPBlockStatus(ip, currentTenantId));
    } catch {
      setStatusError(true);
    } finally {
      setStatusLoading(false);
    }
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
      await loadStatus();
      onActionTaken?.();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'IP action failed');
    } finally {
      setBusy(null);
    }
  };

  const unblockInProgress = status?.state === 'unblocking';
  const effectiveBlocked = !!status?.active && status.state !== 'failed';

  return (
    <DropdownMenu onOpenChange={(open) => {
      if (open) void loadStatus();
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
            {!statusLoading && status && (
              <StatusTag tone={blockStatusTone(status)}>
                {blockStatusLabel(status)}
              </StatusTag>
            )}
          </div>
          <div className="text-[0.7rem] font-normal normal-case tracking-normal text-text-muted">
            {statusLoading
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
            <DropdownMenuItem disabled={!!busy} onClick={() => void dispatch(BLOCK_AFFECTED)}>
              <Shield className="mr-2 h-4 w-4" />
              <span className="flex min-w-0 flex-1 items-center justify-between gap-3">
                <span>{BLOCK_AFFECTED.label}</span>
                <span className="text-[0.68rem] text-text-muted">Affected · 24h</span>
              </span>
            </DropdownMenuItem>
            <DropdownMenuItem disabled={!!busy} onClick={() => void dispatch(BLOCK_FLEET)}>
              <Shield className="mr-2 h-4 w-4" />
              <span className="flex min-w-0 flex-1 items-center justify-between gap-3">
                <span>{BLOCK_FLEET.label}</span>
                <span className="text-[0.68rem] text-text-muted">24h</span>
              </span>
            </DropdownMenuItem>
          </>
        ) : (
          <>
            <DropdownMenuItem disabled={!!busy} onClick={() => void dispatch(ALLOW)}>
              <ShieldOff className="mr-2 h-4 w-4" />
              <span>{ALLOW.label}</span>
            </DropdownMenuItem>
            {status?.scope !== 'fleet' && (
              <DropdownMenuItem disabled={!!busy} onClick={() => void dispatch(BLOCK_FLEET)}>
                <Shield className="mr-2 h-4 w-4" />
                <span className="flex min-w-0 flex-1 items-center justify-between gap-3">
                  <span>Extend to fleet</span>
                  <span className="text-[0.68rem] text-text-muted">24h</span>
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
  const scope = status.scope === 'fleet' ? 'Fleet-wide' : 'Affected nodes';
  const coverage = `${status.nodes_applied}/${status.target_nodes} applied`;
  const pending = status.nodes_pending > 0 ? ` · ${status.nodes_pending} pending` : '';
  const removing = status.nodes_removing > 0 ? ` · ${status.nodes_removing} removing` : '';
  const failed = status.nodes_failed > 0 ? ` · ${status.nodes_failed} failed` : '';
  return `${scope} · ${coverage}${pending}${removing}${failed}`;
}
