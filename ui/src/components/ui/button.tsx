import { Slot } from '@radix-ui/react-slot';
import { cva, type VariantProps } from 'class-variance-authority';
import { forwardRef, type ButtonHTMLAttributes } from 'react';
import { cn } from '@/lib/utils';

const buttonVariants = cva(
  'inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium ring-offset-background transition-all duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0',
  {
    variants: {
      variant: {
        primary:
          'bg-gradient-to-r from-brand-500 to-accent-500 text-[#0f172a] shadow-[0_10px_20px_rgba(99,102,241,0.35)] hover:-translate-y-0.5 hover:shadow-[0_15px_25px_rgba(99,102,241,0.45)]',
        secondary:
          'bg-elevated text-foreground border border-border-subtle hover:border-border-strong hover:bg-hover',
        ghost:
          'bg-transparent text-foreground hover:bg-hover hover:text-foreground',
        outline:
          'border border-border-subtle bg-transparent text-foreground hover:bg-hover hover:border-border-strong',
        danger:
          'bg-state-critical text-[#0f172a] shadow-[0_10px_18px_rgba(248,113,113,0.35)] hover:-translate-y-0.5',
        link:
          'text-brand-500 underline-offset-4 hover:underline hover:text-brand-400 px-0 h-auto',
      },
      size: {
        sm: 'h-8 px-3 text-xs',
        md: 'h-9 px-4 text-sm',
        lg: 'h-10 px-5 text-sm',
        icon: 'h-9 w-9',
      },
    },
    defaultVariants: { variant: 'primary', size: 'md' },
  },
);

export interface ButtonProps
  extends ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
  loading?: boolean;
  shimmer?: boolean;
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild, loading, shimmer, children, disabled, ...props }, ref) => {
    const Comp = asChild ? Slot : 'button';
    return (
      <Comp
        ref={ref}
        className={cn(
          buttonVariants({ variant, size }),
          !asChild && shimmer && variant === 'primary' && 'relative overflow-hidden',
          className,
        )}
        disabled={disabled || loading}
        {...props}
      >
        {asChild ? (
          children
        ) : (
          <>
            {shimmer && variant === 'primary' && (
              <span
                aria-hidden
                className="pointer-events-none absolute inset-0 -translate-x-full bg-gradient-to-r from-transparent via-white/20 to-transparent co-anim-shimmer"
              />
            )}
            {loading && (
              <span
                className="size-3.5 rounded-full border-2 border-current border-t-transparent animate-spin"
                aria-hidden
              />
            )}
            {children}
          </>
        )}
      </Comp>
    );
  },
);

Button.displayName = 'Button';

export { buttonVariants };
