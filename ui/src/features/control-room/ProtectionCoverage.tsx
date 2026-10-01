import { ChevronRight, ShieldCheck } from 'lucide-react';
import type { ControlRoomExecutiveProtection } from '@/lib/api';
import { EmptyState, Panel, PostureBar } from '@/components/kit';
import { CONTROL_ROOM_COPY, formatPercent } from './copy';

interface ProtectionCoverageProps {
  protection: ControlRoomExecutiveProtection;
  available: boolean;
  onOpen: () => void;
}

export function ProtectionCoverage({ protection, available, onOpen }: ProtectionCoverageProps) {
  return (
    <Panel
      eyebrow={CONTROL_ROOM_COPY.protectionCoverage.toUpperCase()}
      title={available ? formatPercent(protection.percentage) : CONTROL_ROOM_COPY.dataUnavailable}
      toneAccent={!available ? 'warning' : protection.gaps > 0 ? 'warning' : 'healthy'}
      actions={
        available ? (
          <button
            type="button"
            onClick={onOpen}
            className="inline-flex items-center gap-1 text-sm font-medium text-text-secondary hover:text-foreground"
          >
            {CONTROL_ROOM_COPY.viewDetails}
            <ChevronRight className="h-4 w-4" />
          </button>
        ) : undefined
      }
    >
      {!available ? (
        <EmptyState title="Protection coverage unavailable" description="Retry the dashboard." />
      ) : (
        <>
          <PostureBar
            score={protection.percentage}
            ariaLabel={`Protection coverage ${formatPercent(protection.percentage)}`}
          />
          <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
            <span className="text-text-secondary">
              <span className="font-mono font-semibold text-foreground">{protection.protected} / {protection.total}</span>{' '}
              public listeners protected
            </span>
            <span className={protection.gaps > 0 ? 'text-state-warning' : 'text-state-healthy'}>
              {protection.gaps > 0 ? `${protection.gaps} protection ${protection.gaps === 1 ? 'gap' : 'gaps'}` : (
                <span className="inline-flex items-center gap-1"><ShieldCheck className="h-4 w-4" /> No protection gaps</span>
              )}
            </span>
          </div>
        </>
      )}
    </Panel>
  );
}
