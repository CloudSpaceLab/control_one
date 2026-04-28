import type { ChartOptions } from 'chart.js';

function readVar(name: string, fallback: string): string {
  if (typeof window === 'undefined') return fallback;
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return v || fallback;
}

export function chartColors() {
  return {
    text: readVar('--text-secondary', '#94a3b8'),
    muted: readVar('--text-muted', '#64748b'),
    grid: readVar('--border-subtle', 'rgba(148,163,184,0.18)'),
    surface: readVar('--bg-elevated', 'rgba(15,23,42,0.85)'),
    brand: readVar('--brand-500', '#6366f1'),
    accent: readVar('--accent-500', '#38bdf8'),
    healthy: readVar('--state-healthy', '#22d3a4'),
    warning: readVar('--state-warning', '#fbbf24'),
    critical: readVar('--state-critical', '#f87171'),
  };
}

export function defaultLineOptions(): ChartOptions<'line'> {
  const c = chartColors();
  return {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: 'index', intersect: false },
    plugins: {
      legend: {
        display: true,
        position: 'top',
        labels: { color: c.text, boxWidth: 10, boxHeight: 10, usePointStyle: true, font: { size: 11 } },
      },
      tooltip: {
        backgroundColor: c.surface,
        borderColor: c.grid,
        borderWidth: 1,
        titleColor: c.text,
        bodyColor: c.text,
        padding: 10,
        cornerRadius: 6,
      },
    },
    scales: {
      x: {
        grid: { color: c.grid, drawTicks: false },
        ticks: { color: c.muted, font: { size: 11 }, maxRotation: 0 },
      },
      y: {
        grid: { color: c.grid, drawTicks: false },
        ticks: { color: c.muted, font: { size: 11 } },
        beginAtZero: true,
      },
    },
    elements: {
      line: { tension: 0.35, borderWidth: 2 },
      point: { radius: 0, hoverRadius: 4 },
    },
  };
}

export function defaultBarOptions(): ChartOptions<'bar'> {
  const c = chartColors();
  return {
    responsive: true,
    maintainAspectRatio: false,
    plugins: {
      legend: { display: false },
      tooltip: {
        backgroundColor: c.surface,
        borderColor: c.grid,
        borderWidth: 1,
        titleColor: c.text,
        bodyColor: c.text,
        padding: 10,
        cornerRadius: 6,
      },
    },
    scales: {
      x: { grid: { color: 'transparent' }, ticks: { color: c.muted, font: { size: 11 } } },
      y: { grid: { color: c.grid }, ticks: { color: c.muted, font: { size: 11 } }, beginAtZero: true },
    },
  };
}

export function defaultDoughnutOptions(): ChartOptions<'doughnut'> {
  const c = chartColors();
  return {
    responsive: true,
    maintainAspectRatio: false,
    cutout: '70%',
    plugins: {
      legend: {
        position: 'right',
        labels: { color: c.text, boxWidth: 10, boxHeight: 10, usePointStyle: true, font: { size: 11 } },
      },
      tooltip: {
        backgroundColor: c.surface,
        borderColor: c.grid,
        borderWidth: 1,
        titleColor: c.text,
        bodyColor: c.text,
      },
    },
  };
}

export function gradientFill(ctx: CanvasRenderingContext2D, color: string, height = 200): CanvasGradient {
  const g = ctx.createLinearGradient(0, 0, 0, height);
  g.addColorStop(0, color + 'aa');
  g.addColorStop(1, color + '00');
  return g;
}
