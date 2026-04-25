import { useEffect, useRef } from 'react';
import { useApiClient } from './useApiClient';
// useEventStream subscribes to the control-plane SSE stream for the given
// tenant. The handler is invoked for each event. Empty tenantId disables the
// subscription (useful while the tenant picker is still loading).
export function useEventStream(tenantId, topics, onEvent) {
    const client = useApiClient();
    const handlerRef = useRef(onEvent);
    handlerRef.current = onEvent;
    useEffect(() => {
        if (!tenantId)
            return undefined;
        const cancel = client.streamEvents({ tenantId, topics }, (ev) => handlerRef.current(ev));
        return () => cancel();
    }, [client, tenantId, topics.join(',')]);
}
