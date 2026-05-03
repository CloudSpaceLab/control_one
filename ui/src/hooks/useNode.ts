import { useEffect, useState } from 'react';
import { useApiClient } from './useApiClient';
import type { Node, NodeHealthScore, TelemetryMetric } from '../lib/api';

export interface UseNodeResult {
  node: Node | null;
  health: NodeHealthScore | null;
  telemetry: TelemetryMetric[];
  loading: boolean;
  error: Error | null;
  reload: () => void;
}

export function useNode(nodeId: string | null | undefined): UseNodeResult {
  const api = useApiClient();
  const [node, setNode] = useState<Node | null>(null);
  const [health, setHealth] = useState<NodeHealthScore | null>(null);
  const [telemetry, setTelemetry] = useState<TelemetryMetric[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    if (!nodeId) return;
    let cancelled = false;
    setLoading(true);
    setError(null);

    Promise.all([
      api.getNode(nodeId),
      api.getNodeHealth(nodeId).catch(() => null),
      api.getNodeTelemetryMetrics(nodeId).catch(() => ({ data: [] as TelemetryMetric[] })),
    ])
      .then(([n, h, t]) => {
        if (cancelled) return;
        setNode(n);
        setHealth(h);
        setTelemetry(Array.isArray(t) ? t : t.data ?? []);
      })
      .catch((err) => {
        if (!cancelled) setError(err as Error);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [api, nodeId, tick]);

  return {
    node,
    health,
    telemetry,
    loading,
    error,
    reload: () => setTick((t) => t + 1),
  };
}
