// App-wide context: selected repository, health/index status, and the API base
// URL. Kept small on purpose — feature state lives in the features.
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react';
import type { ReactNode } from 'react';
import { api, ApiRequestError } from './api';
import { getApiBase, setApiBase } from './env';
import { useQuery } from './useQuery';
import {
  normalizeIndexStatus,
  type Health,
  type IndexStatus,
  type Repository,
} from './types';

const REPO_STORAGE_KEY = 'archaeologist.repository';

export interface StatusView {
  loading: boolean;
  error: string | null;
  status: IndexStatus | null;
  kind: 'ready' | 'indexing' | 'error' | 'unknown';
  label: string;
  reachable: boolean;
}

interface AppContextValue {
  repositories: Repository[];
  reposLoading: boolean;
  reposError: string | null;
  reloadRepositories: () => void;
  repository: Repository | null;
  selectRepository: (id: string | null) => void;
  status: StatusView;
  reloadStatus: () => void;
  apiBase: string;
  setApiBaseAndReload: (value: string) => void;
}

const AppContext = createContext<AppContextValue | null>(null);

export function AppProvider({ children }: { children: ReactNode }): ReactNode {
  const [repos, setRepos] = useState<Repository[]>([]);
  const [reposLoading, setReposLoading] = useState(true);
  const [reposError, setReposError] = useState<string | null>(null);

  const [repositoryId, setRepositoryId] = useState<string | null>(() => {
    try {
      return window.localStorage.getItem(REPO_STORAGE_KEY);
    } catch {
      return null;
    }
  });

  const [apiBase, setApiBaseState] = useState<string>(() => getApiBase());

  // ---- /api/health ----------------------------------------------------------
  const health = useQuery<Health>(
    (signal) => api.health(signal),
    'health',
  );

  // ---- /api/index/status ----------------------------------------------------
  const indexStatus = useQuery<IndexStatus>(
    (signal) => api.indexStatus(signal),
    'indexStatus',
  );

  const status: StatusView = useMemo(() => {
    const doc = indexStatus.data;
    const normalized = normalizeIndexStatus(doc);
    const reachable = !health.error && !indexStatus.error;
    let kind: StatusView['kind'] = 'unknown';
    let label = 'no status';
    if (indexStatus.error || health.error) {
      kind = 'unknown';
      label = indexStatus.error || health.error ? 'unreachable' : 'no status';
    } else if (doc) {
      kind = normalized.kind;
      label = normalized.label;
    }
    return {
      loading: indexStatus.loading,
      error: indexStatus.error || health.error,
      status: doc,
      kind,
      label,
      reachable,
    };
  }, [indexStatus, health]);

  const reloadStatus = useCallback(() => {
    indexStatus.reload();
  }, [indexStatus.reload]);

  // ---- repositories ---------------------------------------------------------
  useEffect(() => {
    let cancelled = false;
    const controller = new AbortController();
    setReposLoading(true);
    setReposError(null);
    api
      .repositories(controller.signal)
      .then((page) => {
        if (cancelled) return;
        setRepos(page.items ?? []);
        setReposLoading(false);
      })
      .catch((e: unknown) => {
        if (cancelled) return;
        setRepos([]);
        setReposLoading(false);
        setReposError(e instanceof ApiRequestError ? e.message : String(e));
      });
    return () => {
      cancelled = true;
      controller.abort();
    };
  }, [apiBase]);

  const reloadRepositories = useCallback(() => {
    // Bump apiBase state even if unchanged to re-run the effect above.
    setApiBaseState(getApiBase());
  }, []);

  const selectRepository = useCallback(
    (id: string | null) => {
      setRepositoryId(id);
      try {
        if (id) window.localStorage.setItem(REPO_STORAGE_KEY, id);
        else window.localStorage.removeItem(REPO_STORAGE_KEY);
      } catch {
        /* non-fatal */
      }
      // Scroll workspaces to top on repository switch.
      window.scrollTo(0, 0);
    },
    [],
  );

  const repository = useMemo(
    () => (repositoryId ? (repos.find((r) => r.id === repositoryId) ?? null) : null),
    [repositoryId, repos],
  );

  const setApiBaseAndReload = useCallback((value: string) => {
    setApiBase(value);
    setApiBaseState(value.trim() || getApiBase());
    // Re-run repositories + status loads with the new base.
  }, []);

  // Poll index status while it reports indexing (bounded cadence).
  const polling = status.kind === 'indexing';
  useEffect(() => {
    if (!polling) return;
    const id = window.setInterval(() => indexStatus.reload(), 5000);
    return () => window.clearInterval(id);
  }, [polling, indexStatus.reload]);

  const value = useMemo<AppContextValue>(
    () => ({
      repositories: repos,
      reposLoading,
      reposError,
      reloadRepositories,
      repository,
      selectRepository,
      status,
      reloadStatus,
      apiBase,
      setApiBaseAndReload,
    }),
    [
      repos,
      reposLoading,
      reposError,
      reloadRepositories,
      repository,
      selectRepository,
      status,
      reloadStatus,
      apiBase,
      setApiBaseAndReload,
    ],
  );

  return <AppContext.Provider value={value}>{children}</AppContext.Provider>;
}

export function useApp(): AppContextValue {
  const ctx = useContext(AppContext);
  if (!ctx) throw new Error('useApp must be used inside <AppProvider>');
  return ctx;
}
