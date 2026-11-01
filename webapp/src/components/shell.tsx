// Application shell: sticky top bar (repository selector, index-status
// indicator, workspace tabs, theme toggle, API base URL control) plus honest
// connection banners. When the service is down or the base URL is wrong the
// user is told why and how to fix it — never shown a silent dead UI.
import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { useApp } from '@/lib/appContext';
import { DEFAULT_API_BASE } from '@/lib/env';
import { navigate, type Route } from '@/lib/router';
import { repoLabel } from '@/lib/types';
import { ErrorBlock, NoticeBlock } from './states';
import { IconClock, IconExplain, IconGraph, IconInspect, IconSearch, Mark } from './icons';

type IconComp = (props: { width?: number; height?: number }) => ReactNode;

const TABS: Array<{ name: string; label: string; icon: IconComp }> = [
  { name: 'architect', label: 'Architect', icon: IconGraph },
  { name: 'inspect', label: 'Inspect', icon: IconInspect },
  { name: 'search', label: 'Search', icon: IconSearch },
  { name: 'history', label: 'History', icon: IconClock },
  { name: 'explainer', label: 'Explain', icon: IconExplain },
];

export function Shell({ route, children }: { route: Route; children: ReactNode }) {
  const app = useApp();

  // ---- theme (dark default, designed light theme) --------------------------
  const [theme, setTheme] = useState<'dark' | 'light'>(() => {
    try {
      return window.localStorage.getItem('archaeologist.theme') === 'light' ? 'light' : 'dark';
    } catch {
      return 'dark';
    }
  });
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    try {
      window.localStorage.setItem('archaeologist.theme', theme);
    } catch {
      /* storage unavailable — theme still applies for this session */
    }
  }, [theme]);

  // ---- API base control (wired to env.ts: localStorage override layer) -----
  const [apiDraft, setApiDraft] = useState(app.apiBase);
  useEffect(() => {
    setApiDraft(app.apiBase);
  }, [app.apiBase]);
  const commitApiBase = () => app.setApiBaseAndReload(apiDraft);

  // ---- connection state -----------------------------------------------------
  const offline = Boolean(app.reposError || app.status.error);
  const dot: string = offline
    ? 'error'
    : app.status.kind === 'indexing'
      ? 'indexing'
      : app.status.kind === 'ready'
        ? 'ready'
        : app.status.kind === 'error'
          ? 'error'
          : '';
  const dotLabel = offline
    ? 'offline'
    : app.status.loading
      ? 'checking…'
      : app.status.kind === 'ready'
        ? 'index ready'
        : app.status.kind === 'indexing'
          ? 'indexing'
          : app.status.kind === 'error'
            ? 'index error'
            : app.status.label || 'no status';

  const banners: ReactNode[] = [];
  if (offline) {
    banners.push(
      <ErrorBlock
        key="offline"
        title="Service unreachable"
        message={`${app.reposError ?? app.status.error} API base in use: ${app.apiBase}. If the Go service runs elsewhere, point the top bar at it. Until it answers, every workspace below shows no data — nothing is fabricated.`}
        onRetry={() => {
          app.reloadRepositories();
          app.reloadStatus();
        }}
      />,
    );
  }
  if (!offline && app.status.kind === 'error' && app.status.status) {
    const detail = [app.status.status.message, ...(app.status.status.errors ?? [])]
      .filter(Boolean)
      .join(' ');
    banners.push(
      <NoticeBlock key="index-error" tone="warning" title={`Index status: ${app.status.label}`}>
        {detail || 'The service reported an index error; re-index or check the service logs.'}
      </NoticeBlock>,
    );
  }
  if (!offline && !app.reposLoading && app.repositories.length === 0) {
    banners.push(
      <NoticeBlock key="no-repos" title="No repositories indexed">
        The service is reachable but reports no repositories. Index one with the CLI
        (the Go pipeline is being completed in parallel) — until then the workspaces
        have no data to show.
      </NoticeBlock>,
    );
  }

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="app-frame topbar-inner" style={{ flexWrap: 'wrap', rowGap: 10 }}>
          <a className="topbar-brand" href="#/">
            <span className="mark">
              <Mark />
            </span>
            <span>Codebase Archaeologist</span>
          </a>

          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <label className="label-caps" htmlFor="repo-select">
              Repository
            </label>
            <select
              id="repo-select"
              className="select"
              style={{ width: 210, height: 30, padding: '0 10px' }}
              value={app.repository?.id ?? ''}
              onChange={(e) => app.selectRepository(e.target.value || null)}
              disabled={app.reposLoading && app.repositories.length === 0}
              aria-busy={app.reposLoading}
              title="Every workspace operates on this repository"
            >
              {app.repositories.length === 0 ? (
                <option value="">
                  {app.reposLoading
                    ? 'loading repositories…'
                    : offline
                      ? 'no repositories (service unreachable)'
                      : 'no repositories indexed'}
                </option>
              ) : (
                <>
                  <option value="">no repository selected</option>
                  {app.repositories.map((r) => (
                    <option key={r.id} value={r.id}>
                      {repoLabel(r)}
                    </option>
                  ))}
                </>
              )}
            </select>
          </div>

          <nav className="nav-tabs" aria-label="Workspaces">
            {TABS.map((t) => {
              const Ico = t.icon;
              const active = route.name === t.name;
              return (
                <button
                  key={t.name}
                  className="nav-tab"
                  aria-current={active ? 'page' : undefined}
                  onClick={() => navigate(t.name)}
                >
                  <span style={{ display: 'inline-flex', marginRight: 6, verticalAlign: '-2px' }}>
                    <Ico width={14} height={14} />
                  </span>
                  {t.label}
                </button>
              );
            })}
          </nav>

          <div className="topbar-status" style={{ flexWrap: 'wrap', rowGap: 8 }}>
            <span
              className="status-item"
              title={offline ? (app.reposError ?? app.status.error ?? 'unreachable') : app.status.label}
            >
              <span className="status-dot" data-state={dot} />
              {dotLabel}
            </span>
            <button
              className="btn btn-ghost"
              style={{ height: 30, padding: '0 10px' }}
              onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
              aria-label={`Switch to ${theme === 'dark' ? 'light' : 'dark'} theme`}
            >
              {theme === 'dark' ? 'Light' : 'Dark'}
            </button>
            <label className="label-caps" htmlFor="api-base">
              API
            </label>
            <input
              id="api-base"
              className="input"
              style={{ width: 190, height: 30, padding: '0 10px' }}
              value={apiDraft}
              onChange={(e) => setApiDraft(e.target.value)}
              onBlur={commitApiBase}
              onKeyDown={(e) => {
                if (e.key === 'Enter') e.currentTarget.blur();
              }}
              placeholder={DEFAULT_API_BASE}
              aria-label="API base URL — committed on Enter/blur, stored in localStorage, overrides the build-time env"
            />
          </div>
        </div>
      </header>

      {banners.length > 0 && (
        <div className="app-frame" style={{ paddingTop: 12, display: 'flex', flexDirection: 'column', gap: 10 }}>
          {banners}
        </div>
      )}

      <main className="app-frame">{children}</main>
    </div>
  );
}
