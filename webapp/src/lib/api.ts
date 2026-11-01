// Thin client for the pinned Archaeologist REST contract.
//
// Only the endpoints below exist; nothing else is called. Every method takes
// an AbortSignal so callers can interrupt in-flight requests (stale-result
// guards, navigation, unmount).
import { getApiBase } from './env';
import type {
  ApiErrorBody,
  CommitTimeline,
  Dependant,
  ExplainResult,
  Health,
  IndexStatus,
  Module,
  ModuleGraph,
  Paged,
  Repository,
  SymbolDetail,
  SymbolSearchResult,
} from './types';

export interface SearchOptions {
  q?: string;
  type?: string;
  limit?: number;
  cursor?: string | null;
}

export class ApiRequestError extends Error {
  readonly status: number;
  readonly code?: string;
  readonly body: unknown;

  constructor(message: string, status: number, body: unknown) {
    super(message);
    this.name = 'ApiRequestError';
    this.status = status;
    this.body = body;
    const err = (body as ApiErrorBody)?.error;
    if (err?.code) this.code = err.code;
  }
}

/** True when the base URL and this browser's origin differ (CORS territory). */
export function isCrossOrigin(): boolean {
  const base = getApiBase();
  try {
    return new URL(base).origin !== window.location.origin;
  } catch {
    return true;
  }
}

async function request<T>(path: string, init?: { signal?: AbortSignal }): Promise<T> {
  const base = getApiBase().replace(/\/+$/, '');
  let res: Response;
  try {
    res = await fetch(base + path, {
      signal: init?.signal,
      headers: { accept: 'application/json' },
    });
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') throw e;
    // Network-level failure (service down, DNS, CORS preflight, mixed content).
    const detail = e instanceof Error ? e.message : String(e);
    throw new ApiRequestError(
      `Could not reach ${base} (${detail}). If the service is running elsewhere, set the API base in the top bar.`,
      0,
      null,
    );
  }

  const text = await res.text();
  let body: unknown = null;
  if (text) {
    try {
      body = JSON.parse(text);
    } catch {
      body = text;
    }
  }

  if (!res.ok) {
    const msg =
      (body as ApiErrorBody)?.error?.message ||
      (body as Record<string, string>)?.error ||
      `Request failed with HTTP ${res.status}`;
    throw new ApiRequestError(msg, res.status, body);
  }
  return body as T;
}

export const api = {
  /** GET /api/health */
  health(signal?: AbortSignal): Promise<Health> {
    return request<Health>('/api/health', { signal });
  },

  /** GET /api/index/status */
  indexStatus(signal?: AbortSignal): Promise<IndexStatus> {
    return request<IndexStatus>('/api/index/status', { signal });
  },

  /** GET /api/repositories */
  repositories(signal?: AbortSignal): Promise<Paged<Repository>> {
    return request<Paged<Repository>>('/api/repositories', { signal });
  },

  /** GET /api/repositories/{id}/modules */
  modules(repositoryId: string, signal?: AbortSignal): Promise<ModuleGraph> {
    return request<ModuleGraph>(`/api/repositories/${encodeURIComponent(repositoryId)}/modules`, { signal });
  },

  /** GET /api/repositories/{id}/symbols?q=&type=&limit=&cursor= */
  symbolSearch(repositoryId: string, opts: SearchOptions, signal?: AbortSignal): Promise<Paged<SymbolSearchResult>> {
    const p = new URLSearchParams();
    if (opts.q) p.set('q', opts.q);
    if (opts.type) p.set('type', opts.type);
    if (opts.limit != null) p.set('limit', String(opts.limit));
    if (opts.cursor) p.set('cursor', opts.cursor);
    const qs = p.toString();
    return request<Paged<SymbolSearchResult>>(
      `/api/repositories/${encodeURIComponent(repositoryId)}/symbols${qs ? `?${qs}` : ''}`,
      { signal },
    );
  },

  /** GET /api/symbols/{id} */
  symbol(id: string, signal?: AbortSignal): Promise<SymbolDetail> {
    return request<SymbolDetail>(`/api/symbols/${encodeURIComponent(id)}`, { signal });
  },

  /** GET /api/symbols/{id}/dependants */
  dependants(symbolId: string, signal?: AbortSignal): Promise<Paged<Dependant>> {
    return request<Paged<Dependant>>(`/api/symbols/${encodeURIComponent(symbolId)}/dependants`, { signal });
  },

  /** GET /api/commits/{sha}/timeline */
  commitTimeline(sha: string, signal?: AbortSignal): Promise<CommitTimeline> {
    return request<CommitTimeline>(`/api/commits/${encodeURIComponent(sha)}/timeline`, { signal });
  },

  /** GET /api/explain?repository={id}&target={symbol-or-module} */
  explain(repositoryId: string, target: string, signal?: AbortSignal): Promise<ExplainResult> {
    const p = new URLSearchParams({ repository: repositoryId, target });
    return request<ExplainResult>(`/api/explain?${p.toString()}`, { signal });
  },
};

export type { Module };
