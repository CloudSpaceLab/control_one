import { motion } from 'framer-motion';
import './RiskScoreCard.css';

export interface RiskScore {
  score: number;
  max_score: number;
  percent: number;
  trend_direction: 'up' | 'down' | 'stable';
  trend_delta: number;
  components: RiskComponent[];
  calculated_at: string;
}

export interface RiskComponent {
  name: string;
  weight: number;
  raw_score: number;
  max_score: number;
  description: string;
}

interface RiskScoreCardProps {
  score: RiskScore | null;
  loading?: boolean;
}

export function RiskScoreCard({ score, loading }: RiskScoreCardProps): JSX.Element {
  const getScoreColor = (percent: number): string => {
    if (percent >= 80) return '#10b981'; // green
    if (percent >= 60) return '#f59e0b'; // amber
    return '#ef4444'; // red
  };

  const getTrendIcon = (direction: string): string => {
    switch (direction) {
      case 'up':
        return '↗';
      case 'down':
        return '↘';
      default:
        return '→';
    }
  };

  const getTrendColor = (direction: string): string => {
    switch (direction) {
      case 'up':
        return '#10b981'; // green - risk improving
      case 'down':
        return '#ef4444'; // red - risk worsening
      default:
        return '#6b7280'; // gray
    }
  };

  if (loading) {
    return (
      <div className="risk-score-card skeleton">
        <div className="skeleton-title" />
        <div className="skeleton-score" />
        <div className="skeleton-trend" />
      </div>
    );
  }

  if (!score) {
    return (
      <div className="risk-score-card empty">
        <h3>Risk Score</h3>
        <p>No data available</p>
      </div>
    );
  }

  const scoreColor = getScoreColor(score.percent);
  const trendIcon = getTrendIcon(score.trend_direction);
  const trendColor = getTrendColor(score.trend_direction);

  return (
    <motion.div
      className="risk-score-card"
      initial={{ opacity: 0, y: 20 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5, ease: 'easeOut' }}
    >
      <div className="risk-score-header">
        <h3>Executive Risk Score</h3>
        <span className="last-updated">
          Updated {new Date(score.calculated_at).toLocaleTimeString()}
        </span>
      </div>

      <div className="risk-score-main">
        <motion.div
          className="score-circle"
          style={{ borderColor: scoreColor }}
          initial={{ scale: 0 }}
          animate={{ scale: 1 }}
          transition={{ duration: 0.5, delay: 0.2, type: 'spring' }}
        >
          <motion.span
            className="score-value"
            style={{ color: scoreColor }}
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            transition={{ duration: 0.3, delay: 0.5 }}
          >
            {score.score}
          </motion.span>
          <span className="score-max">/{score.max_score}</span>
        </motion.div>

        <div className="score-details">
          <div className="percent-display">
            <motion.span
              className="percent-value"
              style={{ color: scoreColor }}
              initial={{ opacity: 0, x: -20 }}
              animate={{ opacity: 1, x: 0 }}
              transition={{ duration: 0.3, delay: 0.6 }}
            >
              {score.percent.toFixed(1)}%
            </motion.span>
          </div>

          {score.trend_direction && (
            <motion.div
              className="trend-indicator"
              style={{ color: trendColor }}
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              transition={{ duration: 0.3, delay: 0.7 }}
            >
              <span className="trend-icon">{trendIcon}</span>
              <span className="trend-value">
                {Math.abs(score.trend_delta).toFixed(1)}%
              </span>
              <span className="trend-label">
                {score.trend_direction === 'up' ? 'improving' : score.trend_direction === 'down' ? 'worsening' : 'stable'}
              </span>
            </motion.div>
          )}
        </div>
      </div>

      {score.components.length > 0 && (
        <motion.div
          className="score-components"
          initial={{ opacity: 0, y: 10 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.3, delay: 0.8 }}
        >
          <h4>Risk Components</h4>
          <div className="components-list">
            {score.components.map((component, index) => (
              <motion.div
                key={component.name}
                className="component-item"
                initial={{ opacity: 0, x: -10 }}
                animate={{ opacity: 1, x: 0 }}
                transition={{ duration: 0.2, delay: 0.9 + index * 0.1 }}
              >
                <div className="component-header">
                  <span className="component-name">{component.name}</span>
                  <span className="component-score">
                    {component.raw_score.toFixed(1)}/{component.max_score}
                  </span>
                </div>
                <div className="component-bar">
                  <motion.div
                    className="component-fill"
                    style={{
                      width: `${(component.raw_score / component.max_score) * 100}%`,
                      backgroundColor: getScoreColor((component.raw_score / component.max_score) * 100),
                    }}
                    initial={{ width: 0 }}
                    animate={{ width: `${(component.raw_score / component.max_score) * 100}%` }}
                    transition={{ duration: 0.5, delay: 1 + index * 0.1 }}
                  />
                </div>
                <span className="component-description">{component.description}</span>
              </motion.div>
            ))}
          </div>
        </motion.div>
      )}
    </motion.div>
  );
}
