import { type HTMLAttributes, type ReactNode } from 'react';
import { cn } from '@/lib/utils';

type ColSpan = 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10 | 11 | 12;

export interface DashboardGridProps extends HTMLAttributes<HTMLDivElement> {
  bleed?: boolean;
  children?: ReactNode;
}

export function DashboardGrid({ bleed, className, children, ...props }: DashboardGridProps) {
  return (
    <div
      className={cn(
        'grid w-full grid-cols-12 gap-4 lg:gap-5',
        bleed ? 'max-w-none' : 'max-w-[1920px]',
        className,
      )}
      {...props}
    >
      {children}
    </div>
  );
}

interface ResponsiveSpan {
  base: ColSpan;
  md?: ColSpan;
  lg?: ColSpan;
  xl?: ColSpan;
  '2xl'?: ColSpan;
}

export interface DashboardGridItemProps extends HTMLAttributes<HTMLDivElement> {
  span?: ColSpan | ResponsiveSpan;
}

const SPAN: Record<ColSpan, string> = {
  1: 'col-span-1',
  2: 'col-span-2',
  3: 'col-span-3',
  4: 'col-span-4',
  5: 'col-span-5',
  6: 'col-span-6',
  7: 'col-span-7',
  8: 'col-span-8',
  9: 'col-span-9',
  10: 'col-span-10',
  11: 'col-span-11',
  12: 'col-span-12',
};

const SPAN_MD: Record<ColSpan, string> = {
  1: 'md:col-span-1', 2: 'md:col-span-2', 3: 'md:col-span-3', 4: 'md:col-span-4',
  5: 'md:col-span-5', 6: 'md:col-span-6', 7: 'md:col-span-7', 8: 'md:col-span-8',
  9: 'md:col-span-9', 10: 'md:col-span-10', 11: 'md:col-span-11', 12: 'md:col-span-12',
};
const SPAN_LG: Record<ColSpan, string> = {
  1: 'lg:col-span-1', 2: 'lg:col-span-2', 3: 'lg:col-span-3', 4: 'lg:col-span-4',
  5: 'lg:col-span-5', 6: 'lg:col-span-6', 7: 'lg:col-span-7', 8: 'lg:col-span-8',
  9: 'lg:col-span-9', 10: 'lg:col-span-10', 11: 'lg:col-span-11', 12: 'lg:col-span-12',
};
const SPAN_XL: Record<ColSpan, string> = {
  1: 'xl:col-span-1', 2: 'xl:col-span-2', 3: 'xl:col-span-3', 4: 'xl:col-span-4',
  5: 'xl:col-span-5', 6: 'xl:col-span-6', 7: 'xl:col-span-7', 8: 'xl:col-span-8',
  9: 'xl:col-span-9', 10: 'xl:col-span-10', 11: 'xl:col-span-11', 12: 'xl:col-span-12',
};
const SPAN_2XL: Record<ColSpan, string> = {
  1: '2xl:col-span-1', 2: '2xl:col-span-2', 3: '2xl:col-span-3', 4: '2xl:col-span-4',
  5: '2xl:col-span-5', 6: '2xl:col-span-6', 7: '2xl:col-span-7', 8: '2xl:col-span-8',
  9: '2xl:col-span-9', 10: '2xl:col-span-10', 11: '2xl:col-span-11', 12: '2xl:col-span-12',
};

function spanClasses(span: DashboardGridItemProps['span']): string {
  if (typeof span === 'number') return SPAN[span];
  if (!span) return SPAN[12];
  return cn(
    SPAN[span.base],
    span.md && SPAN_MD[span.md],
    span.lg && SPAN_LG[span.lg],
    span.xl && SPAN_XL[span.xl],
    span['2xl'] && SPAN_2XL[span['2xl']],
  );
}

export function DashboardGridItem({ span = 12, className, children, ...props }: DashboardGridItemProps) {
  return (
    <div className={cn('min-w-0', spanClasses(span), className)} {...props}>
      {children}
    </div>
  );
}

DashboardGrid.Item = DashboardGridItem;
