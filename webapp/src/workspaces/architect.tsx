// Architecture explorer: the module graph for the selected repository,
// rendered as an SVG layout (deterministic circular — stable across filter
// changes, as the spec requires) with a table fallback for dense graphs or
// reduced detail. Clicking a module focuses its cross-module edges, and
// "Search module" jumps into the symbol search pre-filtered to it.
import { useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { api } from '@/lib/api';
import { useApp } from '@/lib/appContext';
import { useQuery } from '@/lib/useQuery';
import {
  moduleLabel,
  moduleSymbolCount,
  normalizeEdges,
  repoLabel,
  type Module,
  type ModuleEdge,
  type ModuleGraph,
} from '@/lib/types';
import { navigate } from '@/lib/router';
import { EmptyBlock, ErrorBlock, LoadingBlock, NoticeBlock } from '@/components/states';
import { IconGraph, IconTable } from '@/components/icons';
import { ScreenHead, RequireRepo, Section } from './screen';

const DENSE_THRESHOLD = 14;

interface Pos {
  x: number;
  y: number;
}

export function ArchitectureWorkspace() {
  const app = useApp();
  const repo = app.repository;
  if (!repo) return <ArchitectGate />;
  return <Architect repoId={repo.id} repoName={repoLabel(repo)} key={repo.id} />;
}

function ArchitectGate() {
  return (
    <RequireRepo index="01">
      <ScreenHead index="01 · Architect" title="Architecture explorer" />
    </RequireRepo>
  );
}

function Architect({ repoId, repoName }: { repoId: string; repoName: string }) {
  const graph = useQuery<ModuleGraph>(
    (signal) => api.modules(repoId, signal),
    `modules:${repoId}`,
    { enabled: Boolean(repoId) },
  );
  const [focusId, setFocusId] = useState<string | null>(null);
  const [view, setView] = useState<'auto' | 'graph' | 'table'>('auto');

  const modules = graph.data?.items ?? [];
  const edges = useMemo(() => normalizeEdges(graph.data ?? null), [graph.data]);

  const dense = modules.length > DENSE_THRESHOLD;
  const showTable = (view === 'table') || (view === 'auto' && dense);
  const showGraph = !showTable;

  const focusedModule = focusId ? modules.find((m) => m.id === focusId) : null;
  const focusEdges = focusId
    ? edges.filter((e) => e.source === focusId || e.target === focusId)
    : edges;

  return (
    <>
      <ScreenHead
        index="01 · Architect"
        title="Architecture explorer"
        job={`Module graph for ${repoName}: modules and cross-module edges exactly as the index records them. Click a module to focus its edges; the table view is the accessible fallback.`}
      />

      {graph.error ? (
        <ErrorBlock
          title="Could not load the module graph"
          message={graph.error}
          onRetry={graph.reload}
        />
      ) : (
        <>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
            <div role="group" aria-label="Graph view" style={{ display: 'flex', gap: 4 }}>
              <ViewBtn active={view === 'graph'} onClick={() => setView('graph')} label="Graph">
                <IconGraph width={13} height={13} /> Graph
              </ViewBtn>
              <ViewBtn active={view === 'table'} onClick={() => setView('table')} label="Table">
                <IconTable width={13} height={13} /> Table
              </ViewBtn>
            </div>
            {dense && view === 'auto' && (
              <span className="text-muted" style={{ fontSize: 0.75 }}>
                {modules.length} modules — dense graph, table shown for readability.
              </span>
            )}
          </div>

          {graph.loading && <LoadingBlock label="Loading module graph" />}

          {!graph.loading && modules.length === 0 && (
            <EmptyBlock
              icon={<IconGraph />}
              title="No modules recorded"
              hint="The service answered, but it reports no modules for this repository. If the index run was partial or the service is still being finished, there is nothing to draw — this is an honest empty state, not a placeholder graph."
            />
          )}

          {!graph.loading && modules.length > 0 && showGraph && (
            <ModuleGraphSvg modules={modules} edges={focusEdges} focusId={focusId} onPick={setFocusId} />
          )}

          {!graph.loading && modules.length > 0 && showTable && (
            <ModuleTable modules={modules} edges={focusEdges} focusId={focusId} onPick={setFocusId} />
          )}

          {!graph.loading && modules.length > 0 && (
            <Section
              title="Focused module"
              hint={focusedModule ? moduleLabel(focusedModule) : 'click a module'}
            >
              {!focusedModule ? (
                <span className="text-muted" style={{ fontSize: 0.8125 }}>
                  No module focused. Focus one to see its cross-module edges and search its symbols.
                </span>
              ) : (
                <FocusedModule module={focusedModule} edges={focusEdges} />
              )}
            </Section>
          )}
        </>
      )}
    </>
  );
}

function ViewBtn({
  active,
  onClick,
  label,
  children,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  children: ReactNode;
}) {
  return (
    <button
      className={`btn ${active ? 'btn-primary' : 'btn-ghost'}`}
      style={{ height: 30, padding: '0 10px', fontSize: 0.75 }}
      aria-pressed={active}
      onClick={onClick}
    >
      {children}
      <span className="sr-only">{label}</span>
    </button>
  );
}

/* ---- deterministic circular layout ---------------------------------- */

function layout(modules: Module[], width = 760, height = 520, ring = 200): Pos[] {
  const n = modules.length;
  const cx = width / 2;
  const cy = height / 2;
  return modules.map((_, i) => {
    const angle = (i / Math.max(1, n)) * Math.PI * 2 - Math.PI / 2;
    const r = n <= 1 ? 0 : ring;
    return { x: cx + r * Math.cos(angle), y: cy + r * Math.sin(angle) };
  });
}

function ModuleGraphSvg({
  modules,
  edges,
  focusId,
  onPick,
}: {
  modules: Module[];
  edges: ModuleEdge[];
  focusId: string | null;
  onPick: (id: string) => void;
}) {
  const positions = useMemo(() => layout(modules), [modules]);
  const posOf = new Map<string, Pos>();
  modules.forEach((m, i) => posOf.set(m.id, positions[i]));
  const dimmed = (id: string) =>
    focusId != null && id !== focusId && !edges.some((e) => (e.source === focusId && e.target === id) || (e.target === focusId && e.source === id));

  return (
    <div style={{ overflow: 'auto', border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-md)', background: 'var(--surface-1)' }}>
      <svg
        viewBox="0 0 760 520"
        role="img"
        aria-label={`Module graph: ${modules.length} modules, ${edges.length} cross-module edges`}
        style={{ width: '100%', minWidth: 560, display: 'block', height: 'auto' }}
      >
        {edges.map((e, i) => {
          const a = posOf.get(e.source);
          const b = posOf.get(e.target);
          if (!a || !b) return null;
          const dim = focusId != null && e.source !== focusId && e.target !== focusId;
          return (
            <line
              key={i}
              x1={a.x}
              y1={a.y}
              x2={b.x}
              y2={b.y}
              stroke="var(--accent-2)"
              strokeOpacity={dim ? 0.15 : 0.55}
              strokeWidth={Math.min(3, 0.75 + (e.count ?? 1) * 0.25)}
            />
          );
        })}
        {modules.map((m, i) => {
          const p = positions[i];
          const r = Math.min(34, 18 + 2 * Math.log10(1 + (moduleSymbolCount(m) ?? 0)));
          const dim = dimmed(m.id);
          const isFocus = m.id === focusId;
          return (
            <g
              key={m.id}
              tabIndex={0}
              role="button"
              aria-label={`module ${moduleLabel(m)}${isFocus ? ' (focused)' : ''}`}
              style={{ cursor: 'pointer', outline: 'none' }}
              opacity={dim ? 0.35 : 1}
              onClick={() => onPick(m.id)}
              onKeyDown={(ev) => {
                if (ev.key === 'Enter' || ev.key === ' ') {
                  ev.preventDefault();
                  onPick(m.id);
                }
              }}
              onFocus={() => onPick(m.id)}
            >
              <circle
                cx={p.x}
                cy={p.y}
                r={r}
                fill="var(--surface-2)"
                stroke={isFocus ? 'var(--accent)' : 'var(--border-strong)'}
                strokeWidth={isFocus ? 2 : 1}
              />
              <text
                x={p.x}
                y={p.y + r + 12}
                textAnchor="middle"
                fill="var(--text-secondary)"
                fontSize="0.6875rem"
                fontFamily="var(--font-mono)"
                style={{ textTransform: 'uppercase', letterSpacing: 'var(--letter-caps)' }}
              >
                {truncateLabel(moduleLabel(m))}
              </text>
            </g>
          );
        })}
      </svg>
    </div>
  );
}

function truncateLabel(s: string, n = 16): string {
  return s.length > n ? s.slice(0, n - 1) + '…' : s;
}

/* ---- table fallback -------------------------------------------------- */

function ModuleTable({
  modules,
  edges,
  focusId,
  onPick,
}: {
  modules: Module[];
  edges: ModuleEdge[];
  focusId: string | null;
  onPick: (id: string) => void;
}) {
  const edgeText = (id: string) =>
    edges
      .filter((e) => e.source === id || e.target === id)
      .map((e) => {
        const other = e.source === id ? e.target : e.source;
        const dir = e.source === id ? '→' : '←';
        return `${dir} ${other}${e.relation ? ` (${e.relation.toLowerCase()})` : ''}${e.count ? ` ×${e.count}` : ''}`;
      })
      .join('  ');
  return (
    <div style={{ overflowX: 'auto' }}>
      <table className="table">
        <thead>
          <tr>
            <th>Module</th>
            <th>Symbols</th>
            <th>Files</th>
            <th>Cross-module edges</th>
          </tr>
        </thead>
        <tbody>
          {modules.map((m) => (
            <tr
              key={m.id}
              style={{ cursor: 'pointer', opacity: focusId && focusId !== m.id ? 0.55 : 1 }}
              onClick={() => onPick(m.id)}
            >
              <td>
                <button
                  className="btn btn-ghost"
                  style={{ padding: 0, border: 'none', background: 'none', color: 'var(--text-primary)', fontSize: 0.8125, textAlign: 'left', height: 'auto' }}
                  onClick={(e) => {
                    e.stopPropagation();
                    onPick(m.id);
                  }}
                >
                  {moduleLabel(m)}
                </button>
                {m.path && <div className="text-muted" style={{ fontSize: 0.7, fontFamily: 'var(--font-mono)' }}>{m.path}</div>}
              </td>
              <td>{moduleSymbolCount(m) ?? '—'}</td>
              <td>{m.fileCount ?? m.file_count ?? '—'}</td>
              <td style={{ maxWidth: 420, fontFamily: 'var(--font-mono)', fontSize: 0.75, color: 'var(--text-secondary)' }}>
                {edgeText(m.id) || '—'}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/* ---- focused module -------------------------------------------------- */

function FocusedModule({ module, edges }: { module: Module; edges: ModuleEdge[] }) {
  const app = useApp();
  const repo = app.repository;
  const symbolCount = moduleSymbolCount(module);
  const out = edges.filter((e) => e.source === module.id);
  const in_ = edges.filter((e) => e.target === module.id);
  const rel = (list: ModuleEdge[]) =>
    list.length
      ? [
          ...list.map((e) =>
            `${e.source === module.id ? 'out' : 'in'} · ${e.source === module.id ? e.target : e.source}${e.relation ? ` (${e.relation.toLowerCase()})` : ''}${e.count ? ` ×${e.count}` : ''}`,
          ),
        ]
      : [];
  const rels = [...rel(out), ...rel(in_)];

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
        <h3 style={{ fontSize: '1rem' }}>{moduleLabel(module)}</h3>
        {module.description && <span className="text-secondary" style={{ fontSize: 0.8125 }}>{module.description}</span>}
      </div>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        <span className="chip">{symbolCount != null ? `${symbolCount} symbols` : 'symbols not recorded'}</span>
        {module.language && <span className="chip">{module.language}</span>}
        {module.path && <span className="chip">{module.path}</span>}
      </div>
      {rels.length > 0 ? (
        <ul style={{ margin: 0, paddingLeft: 18, color: 'var(--text-secondary)', fontSize: 0.8125, display: 'flex', flexDirection: 'column', gap: 4 }}>
          {rels.map((r, i) => (
            <li key={i} className="text-mono" style={{ fontSize: 0.75 }}>{r}</li>
          ))}
        </ul>
      ) : (
        <NoticeBlock title="No cross-module edges recorded">
          The index reports no inbound or outbound cross-module edges for this module. That is what the data says — not a promise the module is isolated.
        </NoticeBlock>
      )}
      {repo && (
        <div>
          <button
            className="btn btn-primary"
            onClick={() => navigate(`search/q=${encodeURIComponent(moduleLabel(module))}`)}
          >
            Search this module’s symbols
          </button>
        </div>
      )}
    </div>
  );
}
