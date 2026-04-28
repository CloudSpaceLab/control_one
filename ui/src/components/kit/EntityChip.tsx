import { ExternalLink } from 'lucide-react';
import { Link } from 'react-router-dom';
import { cn } from '@/lib/utils';
import { type EntityType } from './types';

export interface EntityChipProps {
  type: EntityType;
  value: string;
  label?: string;
  classification?: string;
  className?: string;
  onClick?: () => void;
}

const TYPE_LABEL: Record<EntityType, string> = {
  ip: 'IP',
  process: 'PROC',
  file: 'FILE',
  hash: 'HASH',
  user: 'USER',
  host: 'HOST',
  domain: 'DOM',
  url: 'URL',
  session: 'SESS',
  alert: 'ALRT',
  rule: 'RULE',
  tenant: 'TNT',
};

export function EntityChip({ type, value, label, classification, className, onClick }: EntityChipProps) {
  const display = label ?? value;
  const href = `/investigate/${type}/${encodeURIComponent(value)}`;
  const content = (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-sm border border-border-subtle bg-surface-2 px-1.5 py-0.5 font-mono text-[0.7rem] text-foreground transition-colors hover:border-border-strong hover:bg-hover',
        className,
      )}
    >
      <span className="text-[0.6rem] uppercase tracking-wider text-text-muted">{TYPE_LABEL[type]}</span>
      <span className="truncate max-w-[24ch]">{display}</span>
      {classification && (
        <span className="rounded-[3px] border border-state-warning/40 bg-state-warning/10 px-1 text-[0.55rem] uppercase tracking-wider text-state-warning">
          {classification}
        </span>
      )}
      <ExternalLink className="h-3 w-3 shrink-0 text-text-muted" aria-hidden />
    </span>
  );
  if (onClick) {
    return (
      <button type="button" onClick={onClick} className="inline-flex">
        {content}
      </button>
    );
  }
  return (
    <Link to={href} className="inline-flex">
      {content}
    </Link>
  );
}
