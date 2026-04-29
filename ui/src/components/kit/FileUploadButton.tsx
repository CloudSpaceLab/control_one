import { useRef } from 'react';
import { Upload } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

export interface FileUploadButtonProps {
  /** Called with the file's text content and its filename */
  onContent: (text: string, filename: string) => void;
  /** file input accept string, default covers common key/cert types */
  accept?: string;
  label?: string;
  size?: 'sm' | 'md';
  className?: string;
}

export function FileUploadButton({
  onContent,
  accept = '.pem,.key,.pub,.crt,.cer,text/plain',
  label = 'Upload file',
  size = 'sm',
  className,
}: FileUploadButtonProps) {
  const inputRef = useRef<HTMLInputElement>(null);

  return (
    <>
      <input
        ref={inputRef}
        type="file"
        accept={accept}
        className="hidden"
        onChange={async (e) => {
          const file = e.target.files?.[0];
          if (!file) return;
          const text = await file.text();
          onContent(text, file.name);
          // reset so the same file can be re-uploaded
          e.target.value = '';
        }}
      />
      <Button
        type="button"
        variant="secondary"
        size={size}
        className={cn('gap-1.5', className)}
        onClick={() => inputRef.current?.click()}
      >
        <Upload className="h-3.5 w-3.5" />
        {label}
      </Button>
    </>
  );
}
