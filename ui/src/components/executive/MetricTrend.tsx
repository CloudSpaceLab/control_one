import { motion } from 'framer-motion';
import './MetricTrend.css';

export interface MTTDMetrics {
  severity: string;
  mean_minutes: number;
  median_minutes: number;
  p95_minutes: number;
  event_count: number;
  period: string;
  calculated_at: string;
}

export interface MTTRMetrics {
  severity: string;
  mean_minutes: number;
  median_minutes: number;
  p95_minutes: number;
  remediation_count: number;
  period: string;
  calculated_at: string;
}

export interface RemediationVelocity {
  period: string;
  period_count: number;
  remediations: number;
  avg_per_period: number;
  trend_direction: 'up' | 'down' | 'stable';
  trend_percent: number;
}

interface MetricTrendProps {
  title: string;
  value: number | null;
  unit?: string;
  target?: number;
  trend?: 'up' | 'down' | 'stable';
  trendValue?: number;
  loading?: boolean;
  format?: 'time' | 'number' | 'percent';
  sparkline?: number[];
}

export function MetricTrend({
  title,
  value,
  unit = '',
  target,
  trend,
  trendValue,
  loading,
  format = 'number',
  sparkline,
}: MetricTrendProps): JSX.Element {
  const formatValue = (val: number): string => {
    switch (format) {
      case 'time':
        if (val < 60) return `${Math.round(val)}m`;
        if (val < 1440) return `${(val / 60).toFixed(1)}h`;
        return `${(val / 1440).toFixed(1)}d`;
      case 'percent':
        return `${val.toFixed(1)}%`;
      default:
        return val.toLocaleString();
    }
  };

  const getTrendColor = (dir: string): string => {
    // For MTTD/MTTR, down is good (faster detection/remediation)
    // For velocity, up is good (more remediations)
    switch (dir) {
      case 'up':
        return '#10b981';
      case 'down':
        return '#ef4444';
      default:
        return '#6b7280';
    }
  };

  const getTrendIcon = (dir: string): string => {
    switch (dir) {
      case 'up':
        return '↑';
      case 'down':
        return '↓';
      default:
        return '→';
    }
  };

  const getStatusColor = (val: number, tgt?: number): string => {
    if (!tgt) return '#e2e8f0';
    return val <= tgt ? '#10b981' : '#ef4444';
  };

  if (loading) {
    return (
      <div className="metric-trend skeleton">
        <div className="skeleton-title" />
        <div className="skeleton-value" />
      </div>
    );
  }

  return (
    <motion.div
      className="metric-trend"
      initial={{ opacity: 0, y: 20 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.4 }}
    >
      <div className="metric-header">
        <h4>{title}</h4>
        {target && (
          <span className="target-badge">
            Target: {formatValue(target)}
          </span>
        )}
      </div>

      <div className="metric-value-container">
        <motion.span
          className="metric-value"
          style={{ color: value !== null ? getStatusColor(value, target) : '#e2e8f0' }}
          initial={{ opacity: 0, scale: 0.5 }}
          animate={{ opacity: 1, scale: 1 }}
          transition={{ duration: 0.5, type: 'spring' }}
        >
          {value !== null ? formatValue(value) : '--'}
        </motion.span>
        {unit && <span className="metric-unit">{unit}</span>}
      </div>

      {trend && trendValue !== undefined && (
        <motion.div
          className="metric-trend-indicator"
          style={{ color: getTrendColor(trend) }}
          initial={{ opacity: 0, x: -10 }}
          animate={{ opacity: 1, x: 0 }}
          transition={{ duration: 0.3, delay: 0.2 }}
        >
          <span className="trend-icon">{getTrendIcon(trend)}</span>
          <span className="trend-value">{Math.abs(trendValue).toFixed(1)}%</span>
          <span className="trend-label">vs last period</span>
        </motion.div>
      )}

      {sparkline && sparkline.length > 0 && (
        <div className="sparkline-container">
          <svg viewBox={`0 0 ${sparkline.length} 20`} className="sparkline">
            <motion.path
              d={`M 0 ${20 - (sparkline[0] / Math.max(...sparkline)) * 20} ${sparkline
                .map((v, i) => `L ${i} ${20 - (v / Math.max(...sparkline)) * 20}`)
                .join(' ')}`}
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              initial={{ pathLength: 0 }}
              animate={{ pathLength: 1 }}
              transition={{ duration: 1, delay: 0.3 }}
            />
          </svg>
        </div>
      )}
    </motion.div>
  );
}
