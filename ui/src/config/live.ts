export type LiveEventsMode = 'sse' | 'polling' | 'off';

// Public demo builds favor polling because Cloudflare/HTTP3 can make long-lived
// fetch streams surface browser-visible QUIC noise. Private deployments can
// still opt into immediate SSE invalidation with VITE_LIVE_EVENTS_MODE=sse.
const rawMode = String(import.meta.env.VITE_LIVE_EVENTS_MODE ?? 'polling')
  .trim()
  .toLowerCase();

export const liveEventsMode: LiveEventsMode =
  rawMode === 'polling' || rawMode === 'off' || rawMode === 'sse'
    ? rawMode
    : 'sse';

export function liveEventsUseSSE(): boolean {
  return liveEventsMode === 'sse';
}
