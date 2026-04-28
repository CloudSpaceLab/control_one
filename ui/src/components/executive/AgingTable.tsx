import { motion } from 'framer-motion';
import './AgingTable.css';

export interface FindingAging {
  severity: string;
  less_than_7_days: number;
  days_7_to_30: number;
  days_30_to_90: number;
  over_90_days: number;
  total_open: number;
}

interface AgingTableProps {
  aging: FindingAging | null;
  loading?: boolean;
}

export function AgingTable({ aging, loading }: AgingTableProps): JSX.Element {
  const getSeverityColor = (severity: string): string => {
    switch (severity.toLowerCase()) {
      case 'critical':
        return '#ef4444';
      case 'high':
        return '#f59e0b';
      case 'medium':
        return '#3b82f6';
      default:
        return '#6b7280';
    }
  };

  const getAgingColor = (bucket: string): string => {
    switch (bucket) {
      case 'less_than_7_days':
        return '#10b981'; // green - fresh
      case 'days_7_to_30':
        return '#f59e0b'; // amber - aging
      case 'days_30_to_90':
        return '#f97316'; // orange - stale
      case 'over_90_days':
        return '#ef4444'; // red - critical aging
      default:
        return '#6b7280';
    }
  };

  const formatBucketLabel = (bucket: string): string => {
    switch (bucket) {
      case 'less_than_7_days':
        return '< 7 days';
      case 'days_7_to_30':
        return '7-30 days';
      case 'days_30_to_90':
        return '30-90 days';
      case 'over_90_days':
        return '> 90 days';
      default:
        return bucket;
    }
  };

  if (loading) {
    return (
      <div className="aging-table skeleton">
        <div className="skeleton-header" />
        <div className="skeleton-row" />
        <div className="skeleton-row" />
        <div className="skeleton-row" />
      </div>
    );
  }

  if (!aging) {
    return (
      <div className="aging-table empty">
        <h3>Findings Aging</h3>
        <p>No data available</p>
      </div>
    );
  }

  const buckets = [
    { key: 'less_than_7_days', count: aging.less_than_7_days },
    { key: 'days_7_to_30', count: aging.days_7_to_30 },
    { key: 'days_30_to_90', count: aging.days_30_to_90 },
    { key: 'over_90_days', count: aging.over_90_days },
  ];

  const maxCount = Math.max(...buckets.map(b => b.count), 1);

  return (
    <motion.div
      className="aging-table"
      initial={{ opacity: 0, y: 20 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.4 }}
    >
      <div className="aging-header">
        <h3>Findings Aging</h3>
        <div className="severity-badge" style={{ color: getSeverityColor(aging.severity) }}>
          {aging.severity.toUpperCase()}
        </div>
      </div>

      <div className="total-findings">
        <span className="total-label">Total Open:</span>
        <motion.span
          className="total-value"
          initial={{ opacity: 0, scale: 0.5 }}
          animate={{ opacity: 1, scale: 1 }}
          transition={{ duration: 0.5, type: 'spring' }}
        >
          {aging.total_open}
        </motion.span>
      </div>

      <div className="aging-buckets">
        {buckets.map((bucket, index) => (
          <motion.div
            key={bucket.key}
            className="aging-bucket"
            initial={{ opacity: 0, x: -20 }}
            animate={{ opacity: 1, x: 0 }}
            transition={{ duration: 0.3, delay: index * 0.1 }}
          >
            <div className="bucket-info">
              <span className="bucket-label">{formatBucketLabel(bucket.key)}</span>
              <motion.span
                className="bucket-count"
                style={{ color: getAgingColor(bucket.key) }}
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                transition={{ duration: 0.3, delay: 0.2 + index * 0.1 }}
              >
                {bucket.count}
              </motion.span>
            </div>
            <div className="bucket-bar">
              <motion.div
                className="bucket-fill"
                style={{
                  width: `${(bucket.count / maxCount) * 100}%`,
                  backgroundColor: getAgingColor(bucket.key),
                }}
                initial={{ width: 0 }}
                animate={{ width: `${(bucket.count / maxCount) * 100}%` }}
                transition={{ duration: 0.5, delay: 0.3 + index * 0.1 }}
              />
            </div>
          </motion.div>
        ))}
      </div>

      {aging.over_90_days > 0 && (
        <motion.div
          className="aging-alert"
          initial={{ opacity: 0, y: 10 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.3, delay: 0.6 }}
        >
          <span className="alert-icon">⚠️</span>
          <span className="alert-text">
            {aging.over_90_days} findings require immediate attention (&gt;90 days)
          </span>
        </motion.div>
      )}
    </motion.div>
  );
}
