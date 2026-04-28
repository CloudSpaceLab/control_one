import {
  CategoryScale,
  Chart as ChartJS,
  Filler,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  BarElement,
  ArcElement,
  TimeScale,
  Title,
  Tooltip,
  type ChartData,
  type ChartOptions,
  type ChartType,
} from 'chart.js';
import 'chartjs-adapter-date-fns';
import { useMemo } from 'react';
import { Bar, Doughnut, Line } from 'react-chartjs-2';
import { cn } from '@/lib/utils';
import { defaultBarOptions, defaultDoughnutOptions, defaultLineOptions } from '@/lib/chartTheme';

ChartJS.register(
  CategoryScale,
  LinearScale,
  LineElement,
  PointElement,
  BarElement,
  ArcElement,
  TimeScale,
  Title,
  Tooltip,
  Legend,
  Filler,
);

type Kind = 'line' | 'bar' | 'doughnut';

export interface ChartProps<K extends Kind = Kind> {
  kind: K;
  data: ChartData<K extends 'line' ? 'line' : K extends 'bar' ? 'bar' : 'doughnut'>;
  options?: ChartOptions<K extends 'line' ? 'line' : K extends 'bar' ? 'bar' : 'doughnut'>;
  height?: number;
  className?: string;
  ariaLabel?: string;
}

function mergeOptions(kind: Kind, override?: ChartOptions<ChartType>) {
  if (kind === 'line') return { ...defaultLineOptions(), ...(override ?? {}) };
  if (kind === 'bar') return { ...defaultBarOptions(), ...(override ?? {}) };
  return { ...defaultDoughnutOptions(), ...(override ?? {}) };
}

export function Chart({ kind, data, options, height = 220, className, ariaLabel }: ChartProps) {
  const mergedOpts = useMemo(() => mergeOptions(kind, options as ChartOptions<ChartType>), [kind, options]);
  const style = { height };
  return (
    <div className={cn('relative w-full', className)} style={style} aria-label={ariaLabel} role="img">
      {kind === 'line' && <Line data={data as ChartData<'line'>} options={mergedOpts as ChartOptions<'line'>} />}
      {kind === 'bar' && <Bar data={data as ChartData<'bar'>} options={mergedOpts as ChartOptions<'bar'>} />}
      {kind === 'doughnut' && (
        <Doughnut data={data as ChartData<'doughnut'>} options={mergedOpts as ChartOptions<'doughnut'>} />
      )}
    </div>
  );
}
