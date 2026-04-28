import { ArrowRight, Check, Circle } from 'lucide-react';
import { Link } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Eyebrow } from './Eyebrow';
import { cn } from '@/lib/utils';
import type { ReactNode } from 'react';

export interface OnboardingStep {
  id: string;
  title: string;
  description: string;
  to: string;
  cta: string;
  done: boolean;
  required?: boolean;
  external?: boolean;
}

export interface OnboardingChecklistProps {
  eyebrow?: string;
  title?: ReactNode;
  description?: ReactNode;
  steps: OnboardingStep[];
  className?: string;
}

export function OnboardingChecklist({
  eyebrow = 'GET STARTED',
  title = 'Set up Control One',
  description = 'A few quick steps to start monitoring your fleet. You can come back to this list any time.',
  steps,
  className,
}: OnboardingChecklistProps) {
  const total = steps.length;
  const done = steps.filter((s) => s.done).length;
  const pct = total > 0 ? Math.round((done / total) * 100) : 0;

  return (
    <section
      className={cn(
        'relative flex flex-col gap-5 rounded-lg border border-border-default bg-elevated p-6 shadow-[var(--shadow-panel)]',
        className,
      )}
    >
      <header className="flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <Eyebrow tone="brand">{eyebrow}</Eyebrow>
          <h2 className="font-display text-xl font-semibold text-foreground sm:text-2xl">{title}</h2>
          {description && <p className="mt-1 max-w-2xl text-sm text-text-secondary">{description}</p>}
        </div>
        <div className="flex flex-col items-start gap-1 sm:items-end">
          <span className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">
            Progress · {done} / {total}
          </span>
          <div className="h-1.5 w-32 overflow-hidden rounded-full bg-surface-2">
            <div
              className="h-full rounded-full bg-gradient-to-r from-brand-500 to-accent-500 transition-all"
              style={{ width: `${pct}%` }}
            />
          </div>
        </div>
      </header>

      <ol className="flex flex-col gap-2">
        {steps.map((step, i) => (
          <li key={step.id}>
            <Link
              to={step.to}
              target={step.external ? '_blank' : undefined}
              className={cn(
                'group flex items-center gap-4 rounded-md border bg-surface px-4 py-3 transition-colors',
                step.done
                  ? 'border-state-healthy/30 hover:border-state-healthy/60'
                  : 'border-border-subtle hover:border-border-strong hover:bg-hover',
              )}
            >
              <span
                aria-hidden
                className={cn(
                  'flex h-7 w-7 shrink-0 items-center justify-center rounded-full',
                  step.done
                    ? 'bg-state-healthy/15 text-state-healthy'
                    : 'bg-surface-2 text-text-muted',
                )}
              >
                {step.done ? <Check className="h-4 w-4" /> : <Circle className="h-3 w-3" />}
              </span>
              <div className="flex min-w-0 flex-1 flex-col">
                <span className="flex items-center gap-2">
                  <span className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">
                    Step {i + 1}
                  </span>
                  {step.required && (
                    <span className="rounded-sm border border-state-warning/40 bg-state-warning/10 px-1 font-mono text-[0.6rem] uppercase tracking-wider text-state-warning">
                      Required
                    </span>
                  )}
                </span>
                <span className="text-sm font-medium text-foreground">{step.title}</span>
                <span className="text-xs text-text-secondary">{step.description}</span>
              </div>
              <Button
                asChild={false}
                variant={step.done ? 'ghost' : 'primary'}
                size="sm"
                className="pointer-events-none"
              >
                {step.cta}
                <ArrowRight className="h-3.5 w-3.5" />
              </Button>
            </Link>
          </li>
        ))}
      </ol>
    </section>
  );
}
