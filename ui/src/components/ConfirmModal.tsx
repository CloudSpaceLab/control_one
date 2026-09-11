import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import type { ReactNode } from 'react';

interface ConfirmModalProps {
  open: boolean;
  title: string;
  body?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  confirmDisabled?: boolean;
  cancelDisabled?: boolean;
  variant?: 'default' | 'danger';
  children?: ReactNode;
  onConfirm: () => void;
  onCancel: () => void;
}

export function ConfirmModal({
  open,
  title,
  body,
  confirmLabel = 'Confirm',
  cancelLabel = 'Cancel',
  confirmDisabled = false,
  cancelDisabled = false,
  variant = 'default',
  children,
  onConfirm,
  onCancel,
}: ConfirmModalProps): JSX.Element {
  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next && !cancelDisabled) onCancel(); }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {body ? <DialogDescription>{body}</DialogDescription> : null}
        </DialogHeader>
        {children}
        <DialogFooter>
          <Button type="button" variant="secondary" onClick={onCancel} disabled={cancelDisabled}>
            {cancelLabel}
          </Button>
          <Button
            type="button"
            variant={variant === 'danger' ? 'danger' : 'primary'}
            onClick={onConfirm}
            disabled={confirmDisabled}
          >
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
