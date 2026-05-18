import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { classifyValue, ENTITY_TYPE_LABELS, entityRoute } from '../lib/entity';
import type { EntityType } from './kit';
import './CommandPalette.css';

interface Command {
  id: string;
  label: string;
  hint?: string;
  group: string;
  to?: string;
  action?: () => void;
  keywords?: string[];
}

const COMMANDS: Command[] = [
  { id: 'go.dashboard', group: 'Go to', label: 'Control Room', hint: 'Fleet status', to: '/' },
  { id: 'go.alerts', group: 'Go to', label: 'Alerts', hint: 'Open findings', to: '/alerts' },
  { id: 'go.investigate', group: 'Go to', label: 'Search & lifecycle', hint: 'Pivot on evidence', to: '/investigate', keywords: ['search', 'lookup', 'pivot'] },
  { id: 'go.nodes', group: 'Go to', label: 'Servers', hint: 'Node health', to: '/nodes', keywords: ['nodes', 'fleet', 'hosts'] },
  { id: 'go.network', group: 'Go to', label: 'Network & exposure', hint: 'Ports, IP behavior, blocks', to: '/security/network', keywords: ['connections', 'ports', 'ip', 'firewall', 'webserver'] },
  { id: 'go.patch', group: 'Go to', label: 'Patch posture', hint: 'Patch risk', to: '/infrastructure/patch', keywords: ['patches', 'updates', 'maintenance'] },
  { id: 'go.compliance', group: 'Go to', label: 'Compliance', hint: 'Control status', to: '/compliance' },
  { id: 'go.access', group: 'Go to', label: 'Access', hint: 'JIT requests', to: '/access' },
  { id: 'go.audit', group: 'Go to', label: 'Audit Log', hint: 'Change trail', to: '/audit' },
];

// CommandPalette is the Cmd+K (Ctrl+K on Windows/Linux) launcher. It searches
// across navigation targets and surfaces the most-used jumps without making
// the user navigate the side nav. Keyboard-only by design — Esc closes,
// Up/Down move, Enter activates, Tab cycles groups.
export function CommandPalette(): JSX.Element | null {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const navigate = useNavigate();

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const isToggle = (e.key === 'k' || e.key === 'K') && (e.metaKey || e.ctrlKey);
      if (isToggle) {
        e.preventDefault();
        setOpen((v) => !v);
        return;
      }
      if (open && e.key === 'Escape') {
        setOpen(false);
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [open]);

  useEffect(() => {
    if (open) {
      setQuery('');
      setActive(0);
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [open]);

  const matches = useMemo(() => {
    const raw = query.trim();
    const q = raw.toLowerCase();

    const dynamic: Command[] = [];
    if (raw.length > 1) {
      const det = classifyValue(raw);
      if (det.type !== 'unknown') {
        dynamic.push({
          id: 'inv.entity',
          group: 'Investigate',
          label: `Investigate ${ENTITY_TYPE_LABELS[det.type as EntityType]}: ${raw}`,
          hint: 'Open lifecycle',
          to: entityRoute(det.type as EntityType, raw),
        });
      }
      dynamic.push({
        id: 'inv.search',
        group: 'Investigate',
        label: `Full search "${raw}"`,
        hint: 'Faceted results',
        to: `/search?q=${encodeURIComponent(raw)}`,
      });
    }

    if (!q) return [...dynamic, ...COMMANDS];
    const filtered = COMMANDS.filter((c) => {
      const hay = [c.label, c.hint ?? '', c.group, ...(c.keywords ?? [])].join(' ').toLowerCase();
      return hay.includes(q);
    });
    return [...dynamic, ...filtered];
  }, [query]);

  const grouped = useMemo(() => {
    const m = new Map<string, Command[]>();
    matches.forEach((c) => {
      const list = m.get(c.group) ?? [];
      list.push(c);
      m.set(c.group, list);
    });
    return Array.from(m.entries());
  }, [matches]);

  if (!open) return null;

  const run = (c: Command) => {
    if (c.to) navigate(c.to);
    if (c.action) c.action();
    setOpen(false);
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setActive((i) => Math.min(i + 1, matches.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActive((i) => Math.max(i - 1, 0));
    } else if (e.key === 'Enter' && matches[active]) {
      e.preventDefault();
      run(matches[active]);
    }
  };

  let cursor = 0;
  return (
    <div className="co-palette__backdrop" role="dialog" aria-modal="true" aria-label="Command palette" onClick={() => setOpen(false)}>
      <div className="co-palette" onClick={(e) => e.stopPropagation()}>
        <input
          ref={inputRef}
          className="co-palette__input"
          placeholder="Search evidence or open a core page (Esc to close)"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setActive(0);
          }}
          onKeyDown={onKeyDown}
          aria-label="Command search"
        />
        <div className="co-palette__list" role="listbox">
          {grouped.length === 0 ? (
            <div className="co-palette__empty">No matches.</div>
          ) : (
            grouped.map(([group, items]) => (
              <div key={group} className="co-palette__group">
                <div className="co-palette__group-label">{group}</div>
                {items.map((c) => {
                  const idx = cursor++;
                  const isActive = idx === active;
                  return (
                    <button
                      key={c.id}
                      type="button"
                      role="option"
                      aria-selected={isActive}
                      className={`co-palette__item${isActive ? ' co-palette__item--active' : ''}`}
                      onMouseEnter={() => setActive(idx)}
                      onClick={() => run(c)}
                    >
                      <span>{c.label}</span>
                      {c.hint ? <small>{c.hint}</small> : null}
                    </button>
                  );
                })}
              </div>
            ))
          )}
        </div>
        <div className="co-palette__footer">
          <kbd>↑</kbd><kbd>↓</kbd> navigate · <kbd>Enter</kbd> open · <kbd>Esc</kbd> close · <kbd>⌘K</kbd> toggle
        </div>
      </div>
    </div>
  );
}
