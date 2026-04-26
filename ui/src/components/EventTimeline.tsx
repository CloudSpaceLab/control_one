import { ForensicEvent } from '../lib/api';
import './EventTimeline.css';

interface Props {
  events: ForensicEvent[];
}

const SOURCE_COLOR: Record<string, string> = {
  event: 'var(--state-info)',
  file: 'var(--state-warning)',
  db: 'var(--state-degraded)',
  log: 'var(--state-unknown)',
  alert: 'var(--state-critical)',
  process: 'var(--state-healthy)',
};

const SOURCE_GLYPH: Record<string, string> = {
  event: '◆',
  file: '📄',
  db: '🗄',
  log: '📋',
  alert: '⚠',
  process: '⚙',
};

// EventTimeline — vertical SVG-rail timeline. Each row is one event from a
// joined source (events / file_accesses / db_queries). Caller passes the
// already-fetched + sorted events; this component is dumb-presentational.
export default function EventTimeline({ events }: Props) {
  if (!events || events.length === 0) {
    return <div className="event-timeline-empty">No correlated activity in this window.</div>;
  }
  return (
    <ol className="event-timeline">
      {events.map((e, i) => (
        <li key={i} className="event-timeline-row" data-source={e.source}>
          <span
            className="event-timeline-glyph"
            style={{ color: SOURCE_COLOR[e.source] ?? 'var(--state-unknown)' }}
          >
            {SOURCE_GLYPH[e.source] ?? '•'}
          </span>
          <span className="event-timeline-ts">{formatTs(e.ts)}</span>
          <span className="event-timeline-summary">{summarise(e)}</span>
        </li>
      ))}
    </ol>
  );
}

function summarise(e: ForensicEvent): string {
  switch (e.source) {
    case 'file':
      return `${e.op ?? 'access'} ${e.path ?? '<unknown>'} (${formatBytes(e.bytes)}) by ${e.process_name ?? 'pid ' + (e.pid ?? '?')}`;
    case 'db':
      return `${e.query_text ?? '<query>'} → ${e.rows_affected ?? 0} rows in ${e.exec_time_ms ?? 0}ms`;
    case 'alert':
      return `${e.severity ?? 'alert'}: ${e.message ?? e.event_type}`;
    case 'log':
      return e.message ?? e.event_type;
    default:
      return `${e.event_type}${e.message ? ` — ${e.message}` : ''}`;
  }
}

function formatTs(iso: string): string {
  try {
    const d = new Date(iso);
    return d.toISOString().slice(11, 23);
  } catch {
    return iso;
  }
}

function formatBytes(n?: number): string {
  if (!n) return '0 B';
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
}
