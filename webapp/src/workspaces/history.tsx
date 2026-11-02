// Git-history timeline: pick a symbol (or file) and drive the commit list
// from the commit-stamps the search results and symbol detail already carry
// — no invented endpoint. For a selected commit, fetch /commits/{sha}/
// timeline and render the files + symbols changed at that commit as a
// commit-aware evolution view, with the symbol's snapshot carousel.
//
// Routing note: the hash router is segment-based, so targets travel as
// path segments (`#/history/symbol=foo`), never as `?`-query strings.
import { useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { api } from '@/lib/api';
import { useApp } from '@/lib/appContext';
import { useQuery } from '@/lib/useQuery';
import { useRoute, navigate, symbolPath } from '@/lib/router';
import {
  timelineFiles,
  timelineSymbols,
  type CommitTimeline,
  type Paged,
  type SymbolDetail,
  type SymbolSearchResult,
  type TimelineSymbol,
} from '@/lib/types';
import { shortSha } from '@/lib/format';
import { EmptyBlock, ErrorBlock, LoadingBlock, NoticeBlock } from '@/components/states';
import { IconClock } from '@/components/icons';
import { SnapshotCarousel } from '@/components/SnapshotCarousel';
import { ScreenHead, Section } from './screen';

export function HistoryWorkspace() {
  const route = useRoute();
  const target = route.params.symbol ?? '';
  return <HistoryInner key={route.raw} target={target} initialSha={route.params.sha ?? null} />;
}

function HistoryInner({ target, initialSha }: { target: string; initialSha: string | null }) {
  const app = useApp();
  const repo = app.repository;

  // The commit-stamps that drive the list: the target's recorded commits
  // (from the symbol detail) plus the commit-stamps its search results carry.
  const detail = useQuery<SymbolDetail>(
    (signal) => api.symbol(target, signal),
    `hist-symbol:${target}`,
    { enabled: Boolean(target) },
  );
  const searches = useQuery<Paged<SymbolSearchResult>>(
    (signal) =>
      repo
        ? api.symbolSearch(repo.id, { q: target, limit: 50, cursor: null }, signal)
        : Promise.resolve({ items: [] }),
    `hist-search:${repo?.id}:${target}`,
    { enabled: Boolean(repo && target) },
  );

  const commitStamps = useMemo(() => {
    const fromDetail = detail.data?.commits
      ?.map((c) => (typeof c === 'string' ? c : c.sha ?? c.commit))
      .filter((s): s is string => Boolean(s)) ?? [];
    const fromResults = (searches.data?.items ?? [])
      .map((r) => r.commit ?? r.commitSha)
      .filter((s): s is string => Boolean(s));
    const seen = new Set<string>();
    const out: string[] = [];
    [...fromDetail, ...fromResults].forEach((s) => {
      if (!seen.has(s)) {
        seen.add(s);
        out.push(s);
      }
    });
    return out;
  }, [detail.data, searches.data]);

  // A selected commit to fetch the timeline for. Default to the most recent
  // recorded stamp; the user can pick any of them.
  const [selectedSha, setSelectedSha] = useState<string | null>(initialSha);
  const effectiveSha =
    selectedSha && commitStamps.includes(selectedSha)
      ? selectedSha
      : commitStamps.length
        ? commitStamps[commitStamps.length - 1]
        : null;

  const timeline = useQuery<CommitTimeline>(
    (signal) => (effectiveSha ? api.commitTimeline(effectiveSha, signal) : Promise.resolve(null as unknown as CommitTimeline)),
    `timeline:${effectiveSha ?? 'none'}`,
    { enabled: Boolean(effectiveSha) },
  );

  const tl = timeline.data;
  const files = useMemo(() => timelineFiles(tl), [tl]);
  const syms = useMemo(() => timelineSymbols(tl), [tl]);

  const targetLabel = target || 'untitled target';

  const [targetDraft, setTargetDraft] = useState(target);
  useEffect(() => setTargetDraft(target), [target]);
  const commitTarget = () => {
    const v = targetDraft.trim();
    if (v) navigate(`history/symbol=${encodeURIComponent(v)}`);
    else navigate('history');
  };

  const snapshotItems = commitStamps.map((sha) => (
    <div
      key={sha}
      className="surface"
      style={{ padding: 16, height: '100%', display: 'flex', flexDirection: 'column', gap: 8 }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <span className="status-dot" data-state="ready" style={{ width: 7, height: 7 }} />
        <code style={{ fontSize: 0.875, color: 'var(--text-primary)' }}>{shortSha(sha)}</code>
      </div>
      <button
        className="btn btn-ghost"
        style={{ alignSelf: 'flex-start', height: 28 }}
        onClick={() => setSelectedSha(sha)}
      >
        View commit timeline
      </button>
      <div className="text-muted" style={{ fontSize: 0.75 }}>
        Commit-snapshot of {targetLabel}. Selecting it fetches the files and symbols that changed at this commit.
      </div>
    </div>
  ));
  const snapshotLabels = commitStamps.map((sha) => `commit ${shortSha(sha)}`);

  return (
    <>
      <ScreenHead
        index="04 · History"
        title="Git-history timeline"
        job="Pick a symbol or file; the commit list is driven by the commit-stamps the index already carries — never by an endpoint the contract lacks. Select a commit to see exactly what changed."
      />

      <Section title="Target" hint={repo ? `repository · ${repo.id}` : 'no repository selected'}>
        <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap', alignItems: 'flex-end' }}>
          <label style={{ display: 'flex', flexDirection: 'column', gap: 6, flex: '1 1 280px' }}>
            <span className="label-caps">Symbol or file</span>
            <input
              className="input"
              value={targetDraft}
              onChange={(e) => setTargetDraft(e.target.value)}
              onBlur={commitTarget}
              onKeyDown={(e) => {
                if (e.key === 'Enter') commitTarget();
              }}
              placeholder="a symbol id or name, or a file path"
              aria-label="Symbol or file to track through history"
            />
          </label>
        </div>
        {!target ? (
          <div style={{ marginTop: 10 }}>
            <EmptyBlock
              icon={<IconClock />}
              title="Nothing to track yet"
              hint="Enter a symbol or file to pull its commit history, or open one from the Inspect workspace — it links in here. Until then there is no history to show."
            />
          </div>
        ) : null}
      </Section>

      {target && (
        <Section
          title="Recorded commits"
          hint={commitStamps.length ? `${commitStamps.length} commit-stamps on this target` : 'none recorded yet'}
        >
          {detail.error ? (
            <ErrorBlock title="Could not read the target's commit stamps" message={detail.error} onRetry={detail.reload} />
          ) : !commitStamps.length ? (
            <NoticeBlock title="No commit-stamps recorded for this target">
              The index returned no successive commits for “{target}”, and no search result carried a commit-stamp
              either. The timeline therefore has nothing to evolve through — this is the honest empty state, not a
              promise of history.
            </NoticeBlock>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
              <ol style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 6 }}>
                {commitStamps.map((sha) => (
                  <li key={sha}>
                    <button
                      className="btn"
                      style={{ justifyContent: 'flex-start', opacity: sha === effectiveSha ? 1 : 0.7 }}
                      aria-pressed={sha === effectiveSha}
                      onClick={() => setSelectedSha(sha)}
                    >
                      <span className="mono-sha">{shortSha(sha)}</span>
                      {sha === effectiveSha && <span className="chip" data-tone="info">viewing</span>}
                    </button>
                  </li>
                ))}
              </ol>

              {commitStamps.length > 1 && (
                <div>
                  <span className="label-caps">Snapshot evolution — flip between successive commits</span>
                  <div style={{ marginTop: 8 }}>
                    <SnapshotCarousel
                      items={snapshotItems}
                      labels={snapshotLabels}
                      itemWidth={320}
                      onIndexChange={(i) => setSelectedSha(commitStamps[i])}
                    />
                  </div>
                </div>
              )}
            </div>
          )}
        </Section>
      )}

      {effectiveSha && (
        <Section title={`Commit ${shortSha(effectiveSha)} — what changed`} hint="files + symbols recorded at this commit">
          {timeline.loading ? (
            <LoadingBlock label="Loading commit timeline" compact />
          ) : timeline.error ? (
            <ErrorBlock title="Could not load the commit timeline" message={timeline.error} onRetry={timeline.reload} />
          ) : !tl ? (
            <EmptyBlock title="No timeline recorded" hint="The service did not return a timeline for this commit." />
          ) : files.length === 0 && syms.length === 0 ? (
            <NoticeBlock title="No file or symbol changes recorded at this commit">
              The timeline endpoint answered but reported nothing changed here. That may be accurate, or the index
              may simply have no data for this commit — the UI shows only what it was given.
            </NoticeBlock>
          ) : (
            <div style={{ display: 'grid', gap: 16, gridTemplateColumns: 'minmax(0,1fr) minmax(0,1fr)' }}>
              <TimelineList
                title="Files changed"
                items={files.map((f) => (
                  <TimelineItem key={f} label={f} mono />
                ))}
                empty="No files recorded"
              />
              <TimelineList
                title="Symbols changed"
                items={syms.map((s) => (
                  <TimelineSymbolItem key={s.label} s={s} onInspect={(id) => id && navigate(symbolPath(id))} />
                ))}
                empty="No symbols recorded"
              />
            </div>
          )}
        </Section>
      )}
    </>
  );
}

function TimelineList({ title, items, empty }: { title: string; items: ReactNode[]; empty: string }) {
  return (
    <div>
      <span className="label-caps">{title}</span>
      {items.length ? (
        <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 4 }}>{items}</div>
      ) : (
        <p className="text-muted" style={{ marginTop: 8, fontSize: 0.8125 }}>{empty}</p>
      )}
    </div>
  );
}

function TimelineItem({ label, mono }: { label: string; mono?: boolean }) {
  return (
    <div
      style={{
        fontFamily: mono ? 'var(--font-mono)' : undefined,
        fontSize: 0.8125,
        color: 'var(--text-secondary)',
        padding: '6px 10px',
        background: 'var(--surface-2)',
        border: '1px solid var(--border-subtle)',
        borderRadius: 'var(--radius-sm)',
        wordBreak: 'break-all',
      }}
    >
      {label}
    </div>
  );
}

function TimelineSymbolItem({ s, onInspect }: { s: TimelineSymbol; onInspect: (id: string) => void }) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 8,
        padding: '6px 10px',
        background: 'var(--surface-2)',
        border: '1px solid var(--border-subtle)',
        borderRadius: 'var(--radius-sm)',
      }}
    >
      <code style={{ fontSize: 0.8125, color: 'var(--text-primary)', flex: 1, wordBreak: 'break-all' }}>{s.label}</code>
      {s.kind && <span className="chip">{s.kind}</span>}
      {s.file && <span className="text-muted" style={{ fontFamily: 'var(--font-mono)', fontSize: 0.7 }} title={s.file}>{s.file}</span>}
      {s.line != null && <span className="mono-sha">L{s.line}</span>}
      {s.commit && <span className="mono-sha">{shortSha(s.commit)}</span>}
      <button
        className="btn btn-ghost"
        style={{ height: 24, padding: '0 8px', fontSize: 0.7 }}
        onClick={() => onInspect(s.label)}
        aria-label={`Inspect ${s.label}`}
      >
        Inspect
      </button>
    </div>
  );
}
