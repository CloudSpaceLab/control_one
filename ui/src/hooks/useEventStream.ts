import { useEffect, useRef } from 'react';
import { useApiClient } from './useApiClient';

export interface StreamedEvent {
  topic: string;
  tenant_id: string;
  node_id?: string;
  payload?: unknown;
  timestamp?: string;
}

// useEventStream subscribes to the control-plane SSE stream for the given
// tenant. The handler is invoked for each event. Empty tenantId disables the
// subscription (useful while the tenant picker is still loading).
export function useEventStream(
  tenantId: string | undefined,
  topics: string[],
  onEvent: (ev: StreamedEvent) => void,
): void {
  const client = useApiClient();
  const handlerRef = useRef(onEvent);
  handlerRef.current = onEvent;

  useEffect(() => {
    if (!tenantId || !client?.streamEvents) return undefined;
    const cancel = client.streamEvents(
      { tenantId, topics },
      (ev) => handlerRef.current(ev as StreamedEvent),
    );
    return () => cancel();
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client, tenantId, topics.join(',')]);
}
