import * as ToggleGroupPrimitive from '@radix-ui/react-toggle-group';
import { cva, type VariantProps } from 'class-variance-authority';
import {
  createContext,
  forwardRef,
  useContext,
  type ComponentPropsWithoutRef,
  type ElementRef,
} from 'react';
import { cn } from '@/lib/utils';

const toggleVariants = cva(
  'inline-flex items-center justify-center rounded-sm text-xs font-medium uppercase tracking-wider ring-offset-background transition-colors hover:bg-hover hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 data-[state=on]:bg-brand-500/15 data-[state=on]:text-brand-400 data-[state=on]:border-border-strong',
  {
    variants: {
      size: {
        sm: 'h-7 px-2.5',
        md: 'h-8 px-3',
        lg: 'h-9 px-4',
      },
    },
    defaultVariants: { size: 'md' },
  },
);

const ToggleGroupContext = createContext<VariantProps<typeof toggleVariants>>({ size: 'md' });

const ToggleGroup = forwardRef<
  ElementRef<typeof ToggleGroupPrimitive.Root>,
  ComponentPropsWithoutRef<typeof ToggleGroupPrimitive.Root> & VariantProps<typeof toggleVariants>
>(({ className, size, children, ...props }, ref) => (
  <ToggleGroupPrimitive.Root
    ref={ref}
    className={cn(
      'inline-flex items-center gap-1 rounded-md border border-border-subtle bg-surface-2 p-1',
      className,
    )}
    {...props}
  >
    <ToggleGroupContext.Provider value={{ size }}>{children}</ToggleGroupContext.Provider>
  </ToggleGroupPrimitive.Root>
));
ToggleGroup.displayName = 'ToggleGroup';

const ToggleGroupItem = forwardRef<
  ElementRef<typeof ToggleGroupPrimitive.Item>,
  ComponentPropsWithoutRef<typeof ToggleGroupPrimitive.Item> & VariantProps<typeof toggleVariants>
>(({ className, children, size, ...props }, ref) => {
  const ctx = useContext(ToggleGroupContext);
  return (
    <ToggleGroupPrimitive.Item
      ref={ref}
      className={cn(toggleVariants({ size: size ?? ctx.size }), className)}
      {...props}
    >
      {children}
    </ToggleGroupPrimitive.Item>
  );
});
ToggleGroupItem.displayName = 'ToggleGroupItem';

export { ToggleGroup, ToggleGroupItem };
