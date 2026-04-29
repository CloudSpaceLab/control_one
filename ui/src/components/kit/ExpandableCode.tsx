import { useState } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { cn } from '@/lib/utils';

export interface ExpandableCodeProps {
  label?: string;
  content: string;
  defaultOpen?: boolean;
  className?: string;
}

export function ExpandableCode({
  label = 'View details',
  content,
  defaultOpen = false,
  className,
}: ExpandableCodeProps) {
  const [open, setOpen] = useState(defaultOpen);

  return (
    <div className={cn('flex flex-col gap-1', className)}>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="inline-flex w-fit items-center gap-1 text-xs text-text-secondary hover:text-foreground transition-colors"
        aria-expanded={open}
      >
        {open ? (
          <ChevronDown className="h-3 w-3 shrink-0" />
        ) : (
          <ChevronRight className="h-3 w-3 shrink-0" />
        )}
        {label}
      </button>
      {open && (
        <pre className="overflow-x-auto rounded-md border border-border-subtle bg-surface p-3 font-mono text-[0.7rem] leading-relaxed text-foreground">
          {content}
        </pre>
      )}
    </div>
  );
}
