import { Building2, Check, ChevronsUpDown } from 'lucide-react';
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
import { useTenant } from '@/providers/TenantProvider';
import { cn } from '@/lib/utils';

export function TenantSwitcher() {
  const { tenants, currentTenant, currentTenantId, setCurrentTenantId, loading } = useTenant();
  const [open, setOpen] = useState(false);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="secondary"
          role="combobox"
          aria-expanded={open}
          aria-label="Select tenant"
          className="h-9 max-w-[220px] justify-between gap-2"
        >
          <Building2 className="h-4 w-4 text-text-muted" />
          <span className="truncate text-xs">
            {loading ? 'Loading…' : currentTenant?.name ?? 'All tenants'}
          </span>
          <ChevronsUpDown className="h-3.5 w-3.5 text-text-muted" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-72 p-0">
        <Command>
          <CommandInput placeholder="Search tenants…" />
          <CommandList>
            <CommandEmpty>No tenants found.</CommandEmpty>
            <CommandGroup heading="Tenants">
              <CommandItem
                value="__all__"
                onSelect={() => {
                  setCurrentTenantId(null);
                  setOpen(false);
                }}
              >
                <Check className={cn('h-4 w-4', currentTenantId ? 'opacity-0' : 'opacity-100')} />
                <span>All tenants</span>
              </CommandItem>
              {tenants.map((t) => (
                <CommandItem
                  key={t.id}
                  value={`${t.name} ${t.id}`}
                  onSelect={() => {
                    setCurrentTenantId(t.id);
                    setOpen(false);
                  }}
                >
                  <Check
                    className={cn('h-4 w-4', currentTenantId === t.id ? 'opacity-100' : 'opacity-0')}
                  />
                  <div className="flex min-w-0 flex-col">
                    <span className="truncate text-sm text-foreground">{t.name}</span>
                    <span className="truncate font-mono text-[0.65rem] text-text-muted">{t.id}</span>
                  </div>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
