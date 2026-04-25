import { useEffect, useMemo, useRef, useState } from 'react';
import { Terminal } from 'xterm';
import { FitAddon } from 'xterm-addon-fit';
import 'xterm/css/xterm.css';
import { useApiClient } from '../hooks/useApiClient';
import { Badge } from './Badge';
import type { SessionEvent } from '../lib/api';
import './SessionReplay.css';

interface SessionReplayProps {
  sessionId: string;
  onClose?: () => void;
}

// SessionReplay loads the parsed session, streams the captured output back
// into an xterm.js instance to recreate the original terminal, and exposes
// commands as a searchable, click-to-jump timeline. Operators use this to
// audit privileged sessions without grepping raw recordings.
//
// Playback uses real-time gaps clamped to a reasonable range so a session
// idle for an hour does not freeze the player. Speed control lets reviewers
// fast-forward through long output bursts.
export function SessionReplay({ sessionId, onClose }: SessionReplayProps): JSX.Element {
  const client = useApiClient();
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const [events, setEvents] = useState<SessionEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState('');
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState(2);
  const [cursor, setCursor] = useState(0); // index into events for next emit
  const playbackRef = useRef<number | null>(null);

  // Initialise terminal once.
  useEffect(() => {
    if (!containerRef.current) return undefined;
    const term = new Terminal({
      convertEol: true,
      cursorBlink: false,
      disableStdin: true,
      fontFamily: 'ui-monospace, Menlo, Consolas, monospace',
      fontSize: 13,
      theme: {
        background: '#0a0a0d',
        foreground: '#e6e6ec',
      },
      scrollback: 5000,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(containerRef.current);
    fit.fit();
    termRef.current = term;
    fitRef.current = fit;

    const onResize = () => fit.fit();
    window.addEventListener('resize', onResize);
    return () => {
      window.removeEventListener('resize', onResize);
      term.dispose();
      termRef.current = null;
    };
  }, []);

  // Load events once.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      setLoading(true);
      try {
        const resp = await client.getSessionParsed(sessionId);
        if (!cancelled) {
          setEvents(resp.data ?? []);
          setError(null);
        }
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : 'load failed');
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [client, sessionId]);

  // Reset terminal when events change.
  useEffect(() => {
    termRef.current?.reset();
    setCursor(0);
  }, [events]);

  // Playback effect: emits the next event after a delay.
  useEffect(() => {
    if (!playing || cursor >= events.length || !termRef.current) {
      return undefined;
    }
    const ev = events[cursor];
    const next = events[cursor + 1];
    let delay = 0;
    if (next) {
      const a = Date.parse(ev.at);
      const b = Date.parse(next.at);
      if (!isNaN(a) && !isNaN(b)) {
        delay = Math.max(0, Math.min(2000, b - a)) / speed;
      }
    }
    emit(termRef.current, ev);
    playbackRef.current = window.setTimeout(() => setCursor((c) => c + 1), delay);
    return () => {
      if (playbackRef.current) window.clearTimeout(playbackRef.current);
    };
  }, [playing, cursor, events, speed]);

  // Stop on completion.
  useEffect(() => {
    if (cursor >= events.length && playing) {
      setPlaying(false);
    }
  }, [cursor, events.length, playing]);

  const commandIndex = useMemo(() => {
    const items: { idx: number; ev: SessionEvent }[] = [];
    events.forEach((ev, idx) => {
      if (ev.kind === 'command') items.push({ idx, ev });
    });
    return items;
  }, [events]);

  const filteredCommands = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return commandIndex;
    return commandIndex.filter((c) => (c.ev.command ?? '').toLowerCase().includes(q));
  }, [commandIndex, search]);

  const seekTo = (targetIdx: number) => {
    if (!termRef.current) return;
    setPlaying(false);
    termRef.current.reset();
    for (let i = 0; i <= targetIdx && i < events.length; i++) {
      emit(termRef.current, events[i]);
    }
    setCursor(targetIdx + 1);
  };

  const downloadTranscript = async () => {
    try {
      const text = await client.getSessionTranscript(sessionId);
      const blob = new Blob([text], { type: 'text/plain' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `session-${sessionId}.txt`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'download failed');
    }
  };

  return (
    <div className="session-replay">
      <header className="session-replay__header">
        <div>
          <p className="eyebrow">Session replay</p>
          <h3>Session {sessionId.slice(0, 8)}</h3>
          <p className="muted">
            {events.length} events · {commandIndex.length} commands
          </p>
        </div>
        <div className="session-replay__controls">
          <button
            type="button"
            className="primary-button"
            disabled={cursor >= events.length}
            onClick={() => setPlaying((p) => !p)}
          >
            {playing ? 'Pause' : 'Play'}
          </button>
          <button
            type="button"
            className="secondary-button"
            onClick={() => {
              setPlaying(false);
              termRef.current?.reset();
              setCursor(0);
            }}
          >
            Restart
          </button>
          <select value={speed} onChange={(e) => setSpeed(Number(e.target.value))} aria-label="Playback speed">
            <option value={1}>1×</option>
            <option value={2}>2×</option>
            <option value={4}>4×</option>
            <option value={8}>8×</option>
            <option value={1000}>Skip</option>
          </select>
          <button type="button" className="secondary-button" onClick={downloadTranscript}>
            Download transcript
          </button>
          {onClose ? (
            <button type="button" className="secondary-button" onClick={onClose} aria-label="Close replay">
              Close
            </button>
          ) : null}
        </div>
      </header>

      {error ? <p className="error-banner">{error}</p> : null}

      <div className="session-replay__body">
        <div className="session-replay__terminal" ref={containerRef} />

        <aside className="session-replay__sidebar">
          <input
            className="session-replay__search"
            placeholder="Find a command…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            aria-label="Search commands"
          />
          {loading ? (
            <p className="muted">Loading…</p>
          ) : filteredCommands.length === 0 ? (
            <p className="muted">No commands match.</p>
          ) : (
            <ol className="session-replay__commands">
              {filteredCommands.map((c) => (
                <li key={c.idx}>
                  <button type="button" onClick={() => seekTo(c.idx)} title="Jump to this command">
                    <Badge variant="info" size="sm">#{c.ev.sequence}</Badge>
                    <code>{c.ev.command}</code>
                    <small>{new Date(c.ev.at).toLocaleTimeString()}</small>
                  </button>
                </li>
              ))}
            </ol>
          )}
        </aside>
      </div>
    </div>
  );
}

function emit(term: Terminal, ev: SessionEvent) {
  switch (ev.kind) {
    case 'output':
      term.write(ev.payload);
      break;
    case 'input':
    case 'command':
      // Echo input in dim italic so reviewers can distinguish typed text
      // from server output.
      term.write(`\x1b[2m${ev.payload}\x1b[0m`);
      break;
    case 'resize':
      if (ev.cols && ev.rows) {
        term.resize(ev.cols, ev.rows);
      }
      break;
    default:
      break;
  }
}
