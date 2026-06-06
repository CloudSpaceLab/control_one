export type LiveEventsMode = 'sse' | 'polling' | 'off';

const rawMode = String(import.meta.env.VITE_LIVE_EVENTS_MODE ?? 'sse')
  .trim()
  .toLowerCase();

export const liveEventsMode: LiveEventsMode =
  rawMode === 'polling' || rawMode === 'off' || rawMode === 'sse'
    ? rawMode
    : 'sse';

export function liveEventsUseSSE(): boolean {
  return liveEventsMode === 'sse';
}
