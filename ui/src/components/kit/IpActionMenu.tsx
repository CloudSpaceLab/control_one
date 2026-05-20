import { useState } from 'react';
import { ExternalLink, MoreHorizontal, Shield, ShieldOff } from 'lucide-react';
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

// IpActionMenu is the consolidated context menu wherever an IP appears in the
// app (Connections table, ThreatFeeds, Investigate, alert detail, …). Block
// and Allow dispatch firewall jobs via the entityAction endpoint; the UI
// surfaces the per-node fan-out result via toast.
//
// "Block on affected nodes" is the safer default — only nodes that have
// recently seen traffic to/from the IP get the firewall rule. "Block fleet
// wide" applies the rule on every enrolled node, including ones that have no
// observed contact with the IP.
export interface IpActionMenuProps {
  ip: string;
  /** Optional override for the trigger element. Defaults to a small icon button. */
  trigger?: React.ReactNode;
  onActionTaken?: () => void;
  showInvestigateLink?: boolean;
  showCopyAction?: boolean;
}

export function IpActionMenu({
  ip,
  trigger,
  onActionTaken,
  showInvestigateLink = true,
  showCopyAction = true,
}: IpActionMenuProps): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [busy, setBusy] = useState<null | 'block-affected' | 'block-fleet' | 'allow'>(null);

  const dispatch = async (
    action: 'block' | 'allow',
    scope: 'affected' | 'fleet',
    busyKey: 'block-affected' | 'block-fleet' | 'allow',
  ) => {
    if (!currentTenantId) {
      toast.error('Select a tenant first');
      return;
    }
    setBusy(busyKey);
    try {
      const resp = await client.entityAction('ip', ip, { action, scope }, { tenantId: currentTenantId });
      const dispatched = resp.nodes_dispatched ?? 0;
      const verb = action === 'block' ? 'Block' : 'Unblock';
      if (dispatched === 0) {
        toast.warning(`${verb} ${ip}: no nodes affected`, {
          description: scope === 'affected'
            ? 'No traffic seen for this IP in the last 7 days. Try fleet-wide scope.'
            : 'Tenant has no enrolled nodes.',
        });
      } else {
        toast.success(`${verb} ${ip}: dispatched to ${dispatched} node${dispatched === 1 ? '' : 's'}`, {
          description: 'Outcome will appear in Active Blocks once the agents report back.',
        });
      }
      onActionTaken?.();
    } catch (err) {
      toast.error(`Failed: ${err instanceof Error ? err.message : 'unknown'}`);
    } finally {
      setBusy(null);
    }
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        {trigger ?? (
          <Button variant="ghost" size="icon" className="h-7 w-7" aria-label={`Actions for ${ip}`}>
            <MoreHorizontal className="h-4 w-4" />
          </Button>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuLabel className="text-xs uppercase tracking-wider">{ip}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          disabled={!!busy}
          onClick={() => dispatch('block', 'affected', 'block-affected')}
        >
          <Shield className="mr-2 h-4 w-4" />
          Block on affected nodes
        </DropdownMenuItem>
        <DropdownMenuItem
          disabled={!!busy}
          onClick={() => dispatch('block', 'fleet', 'block-fleet')}
        >
          <Shield className="mr-2 h-4 w-4" />
          Block fleet-wide
        </DropdownMenuItem>
        <DropdownMenuItem
          disabled={!!busy}
          onClick={() => dispatch('allow', 'affected', 'allow')}
        >
          <ShieldOff className="mr-2 h-4 w-4" />
          Unblock (allow)
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
