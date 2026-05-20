export function isFeatureFlagEnabled(flag: string): boolean {
  if (typeof window === 'undefined') return false;
  const flags = (window as Window & { __C1_FLAGS__?: Record<string, boolean> }).__C1_FLAGS__;
  return flags?.[flag] === true;
}
