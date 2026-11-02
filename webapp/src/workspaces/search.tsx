// Hybrid search: the pinned symbols endpoint with a q + type filter, cursor
// pagination (Load more driven by next_cursor), each result showing its
// commit-stamp and evidence reference. Low-confidence and empty states are
// surfaced plainly — never a fabricated result.
import { useEffect, useMemo, useState } from 'react';
import { api } from '@/lib/api';
import { useApp } from '@/lib/appContext';
import { useQuery } from '@/lib/useQuery';
import { useRoute, navigate, symbolPath } from '@/lib/router';
import {
  symbolName,
  symbolKind,
  symbolFile,
  symbolLine,
  symbolCommit,
  symbolConfidence,
  type Paged,
  type SymbolSearchResult,
} from '@/lib/types';
import { confidenceLabel } from '@/lib/format';
import { EmptyBlock, ErrorBlock, LoadingBlock, NoticeBlock } from '@/components/states';
import { EvidenceRow } from '@/components/EvidenceReference';
import { IconSearch } from '@/components/icons';
import { ScreenHead, Section } from './screen';

const PAGE = 30;

export function SearchWorkspace() {
  const app = useApp();
  const repo = app.repository;
  const route = useRoute();

  // Pre-filters can arrive from the architecture explorer (a module label via
  // the "search/q=<label>" segment) or a deep link. Kept editable.
  const [q, setQ] = useState(() => route.params.q ?? route.params._1 ?? '');
  const [type, setType] = useState(() => route.params.type ?? '');
  const [results, setResults] = useState<SymbolSearchResult[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loadToken, setLoadToken] = useState(0);

  const hasRepo = Boolean(repo);
  const queryKey = useMemo(
    () => (hasRepo && (q || type) ? `search:${repo!.id}:${q}:${type}` : 'search:empty'),
    [hasRepo, repo, q, type],
  );

  // First page — runs on query change, returns items + the next_cursor.
  const first = useQuery<Paged<SymbolSearchResult>>(
    (signal) =>
      repo ? api.symbolSearch(repo.id, { q, type, limit: PAGE, cursor: null }, signal) : Promise.resolve({ items: [] }),
    queryKey,
    { enabled: hasRepo && Boolean(q || type) },
  );

  // Subsequent pages — gated on the "Load more" click so we never auto-page.
  const more = useQuery<Paged<SymbolSearchResult>>(
    (signal) =>
      repo && nextCursor
        ? api.symbolSearch(repo.id, { q, type, limit: PAGE, cursor: nextCursor }, signal)
        : Promise.resolve({ items: [] }),
    `more:${repo?.id}:${q}:${type}:${loadToken}`,
    { enabled: hasRepo && loadToken > 0 && Boolean(nextCursor) },
  );

  // Reset + seed pagination whenever the query identity changes.
  useEffect(() => {
    setResults([]);
    setNextCursor(null);
    setLoadToken(0);
  }, [queryKey]);

  useEffect(() => {
    if (!first.data) return;
    setResults(first.data.items);
    setNextCursor(first.data.next_cursor ?? null);
  }, [first.data]);

  useEffect(() => {
    if (!more.data) return;
    setResults((prev) => [...prev, ...more.data.items]);
    setNextCursor(more.data.next_cursor ?? null);
  }, [more.data]);

  const active = hasRepo && Boolean(q || type);
  const canLoadMore = hasRepo && loadToken === 0 ? Boolean(nextCursor) : Boolean(nextCursor && !more.loading);
  const lowCount = results.filter((r) => confidenceLabel(symbolConfidence(r))?.low).length;

  const searchNow = () => {
    // Force the first-page query to re-run even when q/type look unchanged.
    setLoadToken(0);
    setNextCursor(null);
    first.reload();
  };

  return (
    <>
      <ScreenHead
        index="03 · Search"
        title="Hybrid symbol search"
        job="Search by symbol, path or keyword. Each result is stamped with the commit the index recorded it at and shows its evidence reference — exact, verbatim."
      />

      <Section title="Query" hint="q + type filter, cursor-paginated">
        <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap', alignItems: 'flex-end' }}>
          <label style={{ display: 'flex', flexDirection: 'column', gap: 6, flex: '1 1 320px' }}>
            <span className="label-caps">Query</span>
            <div className="search-input">
              <span className="search-ico"><IconSearch width={15} height={15} /></span>
              <input
                className="input"
                value={q}
                onChange={(e) => setQ(e.target.value)}
                placeholder="symbol name, file path, keyword…"
                aria-label="Search query"
              />
            </div>
          </label>
          <label style={{ display: 'flex', flexDirection: 'column', gap: 6, flex: '0 1 180px' }}>
            <span className="label-caps">Type filter</span>
            <input
              className="input"
              value={type}
              onChange={(e) => setType(e.target.value)}
              placeholder="e.g. function, class…"
              aria-label="Symbol type filter"
            />
          </label>
          <button className="btn btn-primary" disabled={!hasRepo} onClick={searchNow} style={{ height: 40 }}>
            Search
          </button>
        </div>
        {!repo && (
          <div style={{ marginTop: 10 }}>
            <EmptyBlock
              title="No repository selected"
              hint="Pick a repository in the top bar to run searches against it. Searches always target the selected repository's index."
            />
          </div>
        )}
      </Section>

      <Section title="Results" hint={active ? `${results.length} shown${lowCount ? ` · ${lowCount} low-confidence` : ''}` : ''}>
        {first.error ? (
          <ErrorBlock title="Search failed" message={first.error} onRetry={first.reload} />
        ) : !active ? (
          <EmptyBlock
            title="No query yet"
            hint="Enter a query and/or type filter to search the selected repository. Nothing is shown until you ask for something — no fabricated results."
          />
        ) : first.loading ? (
          <LoadingBlock label="Searching" />
        ) : results.length === 0 ? (
          <EmptyBlock
            icon={<IconSearch />}
            title="No matching symbols"
            hint={`The service returned zero results for ${q ? `"${q}"` : 'the filter'}. An empty result set is reported honestly; it does not mean the symbol does not exist anywhere — only that this index did not record a match.`}
          />
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
            {lowCount > 0 && (
              <NoticeBlock tone="warning" title={`${lowCount} result${lowCount === 1 ? '' : 's'} are low-confidence`}>
                These matches carry low-confidence flags from the index. Treat their evidence as provisional and verify against the definition.
              </NoticeBlock>
            )}
            <div style={{ overflowX: 'auto' }}>
              <table className="table">
                <thead>
                  <tr>
                    <th>Symbol</th>
                    <th>Kind</th>
                    <th>Evidence (file · line · commit)</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {results.map((r, i) => (
                    <ResultRow key={`${r.id}:${i}`} r={r} onInspect={() => navigate(symbolPath(r.id))} />
                  ))}
                </tbody>
              </table>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
              <button
                className="btn"
                disabled={!canLoadMore || more.loading}
                onClick={() => setLoadToken((t) => t + 1)}
              >
                {more.loading ? 'Loading…' : 'Load more'}
              </button>
              {nextCursor && !more.loading && (
                <span className="text-muted" style={{ fontSize: 0.75 }}>More results available</span>
              )}
              {!nextCursor && results.length > 0 && (
                <span className="text-muted" style={{ fontSize: 0.75 }}>All results shown</span>
              )}
            </div>
          </div>
        )}
      </Section>
    </>
  );
}

function ResultRow({ r, onInspect }: { r: SymbolSearchResult; onInspect: () => void }) {
  const name = symbolName(r);
  const kind = symbolKind(r);
  const file = symbolFile(r);
  const line = symbolLine(r);
  const commit = symbolCommit(r);
  return (
    <tr>
      <td>
        <button
          className="btn btn-ghost"
          style={{ padding: 0, border: 'none', background: 'none', color: 'var(--text-primary)', fontSize: 0.8125, textAlign: 'left', height: 'auto' }}
          onClick={onInspect}
          aria-label={`Inspect ${name}`}
        >
          <code style={{ fontSize: 0.8125 }}>{name}</code>
        </button>
      </td>
      <td>{kind ? <span className="chip">{kind}</span> : <span className="text-muted">—</span>}</td>
      <td style={{ maxWidth: 420 }}>
        <EvidenceRow file={file} line={line ?? undefined} commit={commit} confidence={symbolConfidence(r)} />
      </td>
      <td style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>
        <button className="btn btn-ghost" style={{ padding: '4px 10px' }} onClick={onInspect}>Inspect</button>
      </td>
    </tr>
  );
}
