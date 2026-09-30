import { Check, ChevronsUpDown, UserRound, X } from 'lucide-react';
import { useState } from 'react';
import { Button } from '@/components/ui/button';
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { cn } from '@/lib/utils';
import type { TeamUser } from '@/lib/api';

export interface AssigneePickerProps {
  value: TeamUser | null;
  options: TeamUser[];
  onChange: (value: TeamUser | null) => void;
  disabled?: boolean;
}

export function AssigneePicker({
  value,
  options,
  onChange,
  disabled = false,
}: AssigneePickerProps): JSX.Element {
  const [open, setOpen] = useState(false);

  return (
    <div className="flex items-center gap-1">
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            variant="secondary"
            role="combobox"
            aria-label="Assignee"
            aria-expanded={open}
            disabled={disabled}
            className="h-8 min-w-0 justify-between gap-1.5 px-2"
          >
            <UserRound className="h-3.5 w-3.5 text-text-muted" aria-hidden />
            <span className="truncate text-xs">{value?.name ?? 'Unassigned'}</span>
            <ChevronsUpDown className="h-3.5 w-3.5 text-text-muted" aria-hidden />
          </Button>
        </PopoverTrigger>
        <PopoverContent aria-label="Assignee picker" align="start" className="w-72 p-0">
          <Command label="Search assignees">
            <CommandInput aria-label="Search assignees" placeholder="Search assignees" />
            <CommandList>
              <CommandEmpty>No matching assignees.</CommandEmpty>
              <CommandGroup heading="Assignees">
                {options.map((option) => (
                  <CommandItem
                    key={option.id}
                    value={`${option.name} ${option.email ?? ''}`}
                    onSelect={() => {
                      onChange(option);
                      setOpen(false);
                    }}
                  >
                    <Check
                      className={cn('h-4 w-4', value?.id === option.id ? 'opacity-100' : 'opacity-0')}
                      aria-hidden
                    />
                    <div className="min-w-0">
                      <p className="truncate text-sm">{option.name}</p>
                      {option.email ? (
                        <p className="truncate text-xs text-text-muted">{option.email}</p>
                      ) : null}
                    </div>
                  </CommandItem>
                ))}
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
      {value ? (
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label="Clear assignee"
          disabled={disabled}
          onClick={() => onChange(null)}
        >
          <X aria-hidden />
        </Button>
      ) : null}
    </div>
  );
}
