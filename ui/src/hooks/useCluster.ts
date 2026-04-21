import { useEffect, useState } from 'react';
import { Cluster, ClusterHealth, ClusterRolloutDetail } from '../lib/api';
import { useApiClient } from './useApiClient';
import { useApiErrorHandler } from './useApiErrorHandler';

interface ClusterDetailState {
  cluster: Cluster | null;
  health: ClusterHealth | null;
  rollout: ClusterRolloutDetail | null;
  loading: boolean;
  error: string | null;
}

interface UseClusterResult extends ClusterDetailState {
  reload: () => void;
}

const EMPTY_STATE: ClusterDetailState = {
  cluster: null,
  health: null,
  rollout: null,
  loading: true,
  error: null,
};

// useCluster loads the cluster detail, its latest rollout (if any), and the
// aggregate health view in parallel. The UI re-polls on reload() after
// PATCH / rollout actions.
export function useCluster(clusterId: string | undefined): UseClusterResult {
  const api = useApiClient();
  const handleError = useApiErrorHandler('Failed to load cluster');
  const [state, setState] = useState<ClusterDetailState>(EMPTY_STATE);
  const [reloadToken, setReloadToken] = useState(0);

  useEffect(() => {
    if (!clusterId) {
      setState({ ...EMPTY_STATE, loading: false });
      return;
    }
    let cancelled = false;
    setState((prev) => ({ ...prev, loading: true, error: null }));

    (async () => {
      try {
        const [cluster, health] = await Promise.all([
          api.getCluster(clusterId),
          api.getClusterHealth(clusterId),
        ]);
        let rollout: ClusterRolloutDetail | null = null;
        if (cluster.latest_rollout?.id) {
          try {
            rollout = await api.getClusterRollout(clusterId, cluster.latest_rollout.id);
          } catch {
            // Rollout fetch failure is non-fatal — just hide the progress bar.
            rollout = null;
          }
        }
        if (!cancelled) {
          setState({ cluster, health, rollout, loading: false, error: null });
        }
      } catch (error) {
        if (!cancelled) {
          setState({
            ...EMPTY_STATE,
            loading: false,
            error: handleError(error),
          });
        }
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [api, clusterId, reloadToken, handleError]);

  return {
    ...state,
    reload: () => setReloadToken((token) => token + 1),
  };
}
