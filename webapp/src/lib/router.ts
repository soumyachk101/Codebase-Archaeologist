// Minimal hash router. Hash routing keeps the app deployable from any static
// host with zero server config, and deep links (e.g. a symbol inspected from
// search) survive reloads.
import { useCallback, useEffect, useState } from 'react';

export interface Route {
  /** e.g. 'architect', 'search', 'history', 'explainer', 'inspect' */
  name: string;
  /** Segments after the workspace name. */
  params: Record<string, string>;
  raw: string;
}

export function parseHash(hash: string): Route {
  const clean = hash.replace(/^#\/?/, '');
  const [name = '', ...rest] = clean.split('/');
  const params: Record<string, string> = {};
  rest.forEach((seg, i) => {
    const [k, v] = seg.split('=');
    params[k] = decodeURIComponent(v ?? '');
    params[`_${i}`] = decodeURIComponent(seg);
  });
  return { name, params, raw: clean };
}

function currentHash(): string {
  return window.location.hash || '#/';
}

export function useRoute(): Route {
  const [route, setRoute] = useState<Route>(() => parseHash(currentHash()));
  useEffect(() => {
    const onChange = () => setRoute(parseHash(currentHash()));
    window.addEventListener('hashchange', onChange);
    return () => window.removeEventListener('hashchange', onChange);
  }, []);
  return route;
}

export function navigate(path: string): void {
  const target = path.startsWith('#') ? path : `#/${path.replace(/^\/+/, '')}`;
  if (window.location.hash === target) return;
  window.location.hash = target;
}

/** Build a querystring fragment for a symbol inspect target. */
export function symbolPath(symbolId: string, extra?: Record<string, string>): string {
  const qs = extra ? `?${Object.entries(extra).map(([k, v]) => `${k}=${encodeURIComponent(v)}`).join('&')}` : '';
  return `inspect/${encodeURIComponent(symbolId)}${qs}`;
}

/** Build the explain target path. */
export function explainPath(target: string, repoId?: string): string {
  const repo = repoId ? `?repo=${encodeURIComponent(repoId)}` : '';
  return `explainer/${encodeURIComponent(target)}${repo}`;
}

export function useNavigate(): () => void {
  const [_, setTick] = useState(0);
  return useCallback(() => setTick((t) => t + 1), []);
}
