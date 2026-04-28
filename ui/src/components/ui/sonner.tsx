import { Toaster as SonnerToaster, type ToasterProps } from 'sonner';
import { useTheme } from '@/providers/ThemeProvider';

export function Toaster(props: ToasterProps) {
  const { theme } = useTheme();
  return (
    <SonnerToaster
      theme={theme}
      position="bottom-right"
      richColors
      closeButton
      toastOptions={{
        classNames: {
          toast:
            'group toast group-[.toaster]:bg-elevated group-[.toaster]:text-foreground group-[.toaster]:border group-[.toaster]:border-border-subtle group-[.toaster]:shadow-[var(--shadow-panel)]',
          description: 'group-[.toast]:text-text-secondary',
          actionButton: 'group-[.toast]:bg-brand-500 group-[.toast]:text-[#0f172a]',
          cancelButton: 'group-[.toast]:bg-surface-2 group-[.toast]:text-text-secondary',
        },
      }}
      {...props}
    />
  );
}
