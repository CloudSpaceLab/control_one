import { useState } from 'react';
import { FileText } from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog';
import { LogDumpPanel } from './LogDumpPanel';

interface LogDumpDialogProps {
  tenantId: string;
  fixedNodeId?: string;
  entityFilter?: Record<string, string>;
}

export function LogDumpDialog(props: LogDumpDialogProps): JSX.Element {
  const [open, setOpen] = useState(false);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="secondary" size="sm">
          <FileText className="h-3.5 w-3.5" /> Raw logs
        </Button>
      </DialogTrigger>
      <DialogContent className="max-h-[90vh] max-w-6xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Raw logs</DialogTitle>
        </DialogHeader>
        <LogDumpPanel {...props} />
      </DialogContent>
    </Dialog>
  );
}
