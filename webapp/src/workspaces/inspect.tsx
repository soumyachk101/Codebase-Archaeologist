// Symbol inspector: definition evidence for a symbol exactly as the service
// returned it (file, line range, commit — a copyable path, never a
// fabricated permalink), the reverse-dependency list with each dependant's
// source fact, and a link into the git-history timeline for that symbol.
import { useMemo } from 'react';
import { api } from '@/lib/api';
import { useApp } from '@/lib/appContext';
import { useQuery } from '@/lib/useQuery';
import { useRoute, navigate } from '@/lib/router';
import {
  detailEndLine,
  detailFile,
  detailStartLine,
  symbolName,
  type Dependant,
  type SymbolDetail,
} from '@/lib/types';
import { shortSha, confidenceLabel } from '@/lib/format';
import { CopyButton, EmptyBlock, ErrorBlock, LoadingBlock, NoticeBlock } from '@/components/states';
import { EvidenceRef } from '@/components/EvidenceReference';
import { IconInspect } from '@/components/icons';
import { ScreenHead, Section } from './screen';
import { symbolSnapshots, detailCopyRef } from './shared';

export function InspectorWorkspace() {
  const route = useRoute();
  const symbolId = route.params.inspect ?? route.params._0;

  if (!symbolId) return <PickSymbol />;
  return <Inspect id={symbolId} key={symbolId} />;
}

function PickSymbol() {
  return (
    <>
      <ScreenHead index="02 · Inspect" title="Symbol inspector" />
      <EmptyBlock
        icon={<IconInspect />}
        title="Pick a symbol to inspect"
        hint="Open a symbol from the Search workspace — it carries the exact id this screen needs. There is no symbol selected right now, so there is nothing to show yet."
      />
    </>
  );
}

function Inspect({ id }: { id: string }) {
  const app = useApp();
  const detail = useQuery<SymbolDetail>(
    (signal) => api.symbol(id, signal),
    `symbol:${id}`,
    { enabled: Boolean(id) },
  );
  const deps = useQuery<import('@/lib/types').Paged<Dependant>>(
    (signal) => api.dependants(id, signal),
    `dependants:${id}`,
    { enabled: Boolean(id) },
  );

  const d = detail.data;
  const name = d ? symbolName(d) : id;

  const snapshots = useMemo(() => symbolSnapshots(d), [d]);
  const hasSnapshots = snapshots.length > 0;

  return (
    <>
      <ScreenHead
        index="02 · Inspect"
        title={`Symbol · ${name}`}
        job="Definition evidence, reverse dependencies and the commit timeline for one symbol — every value is exactly what the index recorded."
      />

      {app.repository && <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 12 }}>
        <RepoNote repoId={app.repository.id} />
      </div>}

      {detail.error ? (
        <ErrorBlock title="Could not load this symbol" message={detail.error} onRetry={detail.reload} />
      ) : (
        <Section title="Definition evidence" hint={d ? shortSha(d.commit ?? d.commitSha) : ''}>
          {detail.loading ? (
            <LoadingBlock label="Loading definition" compact />
          ) : !d ? (
            <EmptyBlock
              title="No definition recorded"
              hint="The service answered but did not return a definition for this symbol id. It may be unknown to the index under that identifier."
            />
          ) : (
            <Definition d={d} />
          )}
        </Section>
      )}

      <Section title="Reverse dependencies" hint={deps.data ? `${deps.data.items.length} recorded` : 'who depends on this'}>
        {deps.loading ? (
          <LoadingBlock label="Loading dependants" compact />
        ) : deps.error ? (
          <ErrorBlock title="Could not load dependants" message={deps.error} onRetry={deps.reload} />
        ) : !deps.data || deps.data.items.length === 0 ? (
          <EmptyBlock
            title="No dependants recorded"
            hint="The index found no reverse dependencies for this symbol. An empty list is reported honestly — it is not a claim that nothing depends on it."
          />
        ) : (
          <DependantsList items={deps.data.items} targetId={id} />
        )}
      </Section>

      <Section title="Commit timeline" hint={hasSnapshots ? `${snapshots.length} commit${snapshots.length === 1 ? '' : 's'} stamped on this symbol` : 'no successive commits recorded'}>
        {hasSnapshots ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <ol style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 8 }}>
              {snapshots.map((sha) => (
                <li key={sha} style={{ display: 'flex', alignItems: 'center', gap: 10, fontFamily: 'var(--font-mono)', fontSize: 0.75, color: 'var(--text-secondary)' }}>
                  <span className="status-dot" data-state="ready" style={{ width: 7, height: 7 }} />
                  {shortSha(sha)}
                </li>
              ))}
            </ol>
            <button className="btn btn-ghost" onClick={() => openHistory(id, snapshots[snapshots.length - 1])}>
              Open this symbol in the git-history timeline →
            </button>
          </div>
        ) : (
          <NoticeBlock title="No commit-stamps recorded">
            The service did not record a list of successive commits for this symbol, so the timeline has no snapshots to evolve through. You can still open the History workspace and pick the symbol by name.
          </NoticeBlock>
        )}
      </Section>
    </>
  );
}

/** A tiny "which repo is this from" reminder so a cross-repo symbol id is never
 *  silently read under the wrong repository. */
function RepoNote({ repoId }: { repoId: string }) {
  return <span className="chip">repository · {repoId}</span>;
}

function openHistory(symbolId: string, lastSha?: string) {
  // The hash router is segment-based (parseHash splits on '/'), so the target
  // travels as `key=value` path segments, never as a `?`-query string.
  const segs = [`symbol=${encodeURIComponent(symbolId)}`];
  if (lastSha) segs.push(`sha=${encodeURIComponent(lastSha)}`);
  navigate(`history/${segs.join('/')}`);
}

function Definition({ d }: { d: SymbolDetail }) {
  const file = detailFile(d);
  const s = detailStartLine(d);
  const en = detailEndLine(d);
  const commit = d.commit ?? d.commitSha;
  const ref = detailCopyRef(d);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
        <code style={{ fontSize: 0.9375, color: 'var(--text-primary)' }}>{file ?? 'file not recorded'}</code>
        {s != null && (
          <span className="text-muted" style={{ fontFamily: 'var(--font-mono)', fontSize: 0.75 }}>
            {en != null && en !== s ? `L${s}–L${en}` : `L${s}`}
          </span>
        )}
        {commit && <span className="chip" data-tone="info">{shortSha(commit)}</span>}
        {ref && <CopyButton value={ref} label="copy path + line" />}
      </div>

      {d.signature && (
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 0.8125, color: 'var(--text-secondary)', background: 'var(--surface-2)', border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-sm)', padding: '8px 10px', overflow: 'auto' }}>
          {d.signature}
        </div>
      )}
      {(d.doc ?? d.summary) && <p style={{ fontSize: 0.875, color: 'var(--text-secondary)' }}>{d.doc ?? d.summary}</p>}

      {d.evidence && (
        <div>
          <span className="label-caps">Evidence reference, verbatim</span>
          <div style={{ marginTop: 8 }}>
            <EvidenceRef evidence={d.evidence} />
          </div>
        </div>
      )}
    </div>
  );
}

function DependantsList({ items, targetId }: { items: Dependant[]; targetId: string }) {
  return (
    <div style={{ overflowX: 'auto' }}>
      <table className="table">
        <thead>
          <tr>
            <th>Dependant</th>
            <th>Relation</th>
            <th>Source fact (importing/calling side)</th>
            <th>Confidence</th>
          </tr>
        </thead>
        <tbody>
          {items.map((dep, i) => {
            const name = dep.name ?? dep.symbol ?? dep.id ?? '—';
            const sourceFile = dep.file ?? dep.path;
            const line = dep.startLine ?? dep.line;
            const fact = sourceFile ? `${sourceFile}${line != null ? `:${line}` : ''}` : 'source fact not recorded';
            return (
              <tr key={i}>
                <td>
                  <code style={{ fontSize: 0.8125, color: 'var(--text-primary)' }}>{name}</code>
                  {dep.kind && <div className="text-muted" style={{ fontSize: 0.7 }}>{dep.kind}</div>}
                </td>
                <td>
                  {dep.relation ?? dep.edge ?? dep.type ? (
                    <span className="chip">{String(dep.relation ?? dep.edge ?? dep.type).toLowerCase()}</span>
                  ) : (
                    <span className="text-muted">—</span>
                  )}
                </td>
                <td>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', maxWidth: 340 }}>
                    <code style={{ fontSize: 0.75, color: 'var(--text-secondary)', wordBreak: 'break-all' }}>{fact}</code>
                    <CopyButton value={fact} label="copy" />
                  </div>
                </td>
                <td>
                  {dep.confidence != null ? (
                    <Confidence c={dep.confidence} />
                  ) : dep.resolved === false ? (
                    <span className="chip" data-tone="warning">unresolved</span>
                  ) : (
                    <span className="text-muted">—</span>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {items.some((d) => d.resolved === false) && (
        <div style={{ marginTop: 10 }}>
          <NoticeBlock tone="warning" title="Some dependants are unresolved">
            The index could not resolve the dynamic reference for these edges — the source fact is shown as recorded, but the edge is not a confirmed dependency.
          </NoticeBlock>
        </div>
      )}
      <div style={{ marginTop: 12, display: 'flex', gap: 8 }}>
        <button className="btn btn-ghost" style={{ fontSize: 0.75 }} onClick={() => openHistory(targetId)}>
          Open in History →
        </button>
      </div>
    </div>
  );
}

function Confidence({ c }: { c: number | string }) {
  const conf = confidenceLabel(c);
  if (!conf) return null;
  return conf.low ? <span className="chip" data-tone="warning">low · {conf.text}</span> : <span className="chip">{conf.text}</span>;
}
