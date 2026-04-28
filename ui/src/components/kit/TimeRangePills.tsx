import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { cn } from '@/lib/utils';

export interface TimeRangeOption {
  label: string;
  value: string;
}

export interface TimeRangePillsProps {
  value: string;
  options: TimeRangeOption[];
  onChange: (value: string) => void;
  size?: 'sm' | 'md';
  className?: string;
  ariaLabel?: string;
}

export function TimeRangePills({
  value,
  options,
  onChange,
  size = 'sm',
  className,
  ariaLabel = 'Time range',
}: TimeRangePillsProps) {
  return (
    <ToggleGroup
      type="single"
      value={value}
      onValueChange={(v) => v && onChange(v)}
      aria-label={ariaLabel}
      size={size}
      className={cn(className)}
    >
      {options.map((opt) => (
        <ToggleGroupItem key={opt.value} value={opt.value} size={size}>
          {opt.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  );
}

export const DEFAULT_TIME_RANGES: TimeRangeOption[] = [
  { label: '1H', value: '1h' },
  { label: '24H', value: '24h' },
  { label: '7D', value: '7d' },
  { label: '30D', value: '30d' },
];

export const EXEC_TIME_RANGES: TimeRangeOption[] = [
  { label: '7D', value: '7d' },
  { label: '30D', value: '30d' },
  { label: '90D', value: '90d' },
  { label: 'QTD', value: 'qtd' },
];
