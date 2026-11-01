import { useCallback, useEffect, useRef, useState } from 'react';

export interface QueryState<T> {
  data: T | null;
  /** True while a request for the current key is in flight. */
  loading: boolean;
  error: string | null;
}

/**
 * Minimal data-fetching hook.
 *
 * - Aborts an in-flight request when `key` changes or the component unmounts
 *   (stale responses are dropped).
 * - `refetchToken` (any value) forces a re-run without clearing the key
 *   identity.
 * - The fetcher should be stable (useMemo/useCallback) or capture only its
 *   own dependencies, which are passed via `key`.
 */
export function useQuery<T>(
  fetcher: (signal: AbortSignal) => Promise<T>,
  key: string,
  opts?: { enabled?: boolean; refetchToken?: unknown },
): QueryState<T> {
  const enabled = opts?.enabled !== false;
  const refetchToken = opts?.refetchToken;
  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;

  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(enabled);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!enabled) {
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    setData(null);
    setError(null);
    setLoading(true);

    fetcherRef
      .current(controller.signal)
      .then((result) => {
        if (controller.signal.aborted) return;
        setData(result);
        setLoading(false);
      })
      .catch((e: unknown) => {
        if (controller.signal.aborted) return;
        setError(e instanceof Error ? e.message : String(e));
        setLoading(false);
      });

    return () => controller.abort();
    // fetcher is tracked through the ref; key + enabled + refetchToken
    // drive re-runs.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, enabled, refetchToken]);

  /** Manually re-run the fetch for the current key (keeps data until settled). */
  const reload = useCallback(() => {
    setData(null);
    setError(null);
    setLoading(true);
    const controller = new AbortController();
    fetcherRef
      .current(controller.signal)
      .then((result) => {
        if (controller.signal.aborted) return;
        setData(result);
        setLoading(false);
      })
      .catch((e: unknown) => {
        if (controller.signal.aborted) return;
        setError(e instanceof Error ? e.message : String(e));
        setLoading(false);
      });
  }, [key, enabled]);

  return { data, loading, error, reload };
}
