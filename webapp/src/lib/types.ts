// Pinned REST contract types for the Codebase Archaeologist service.
//
// Fields the contract promises are required; fields that a well-formed
// server may or may not include are optional and every renderer treats
// them as optional — the UI never fabricates values it was not given.

export interface Paged<T> {
  items: T[];
  next_cursor?: string | null;
}

/** Error envelope: { error: { code, message } }. */
export interface ApiErrorBody {
  error?: { code?: string; message?: string };
}

export interface Health {
  status?: string;
  version?: string;
  [key: string]: unknown;
}

export interface Repository {
  id: string;
  name?: string;
  url?: string;
  branch?: string;
  defaultBranch?: string;
  language?: string;
  languages?: string[];
  description?: string;
  updatedAt?: string;
  [key: string]: unknown;
}

/** GET /api/index/status — a free-form status document. */
export interface IndexStatus {
  status?: string;
  state?: string;
  repositoryId?: string;
  repository_id?: string;
  commit?: string;
  branch?: string;
  indexedAt?: string;
  updatedAt?: string;
  progress?: number;
  message?: string;
  version?: string;
  errors?: string[];
  [key: string]: unknown;
}

export interface Module {
  id: string;
  name?: string;
  path?: string;
  description?: string;
  language?: string;
  symbolCount?: number;
  symbol_count?: number;
  fileCount?: number;
  file_count?: number;
  [key: string]: unknown;
}

export interface ModuleEdge {
  source: string;
  target: string;
  relation?: string;
  count?: number;
}

/** GET /api/repositories/{id}/modules — module graph: modules + cross-module edges. */
export interface ModuleGraph extends Paged<Module> {
  edges?: unknown[];
  [key: string]: unknown;
}

/** Hybrid symbol/keyword search result. Each result carries a commit-stamp
 *  and an evidence reference. */
export interface SymbolSearchResult {
  id: string;
  name?: string;
  kind?: string;
  type?: string;
  language?: string;
  file?: string;
  path?: string;
  startLine?: number;
  line?: number;
  endLine?: number;
  commit?: string;
  commitSha?: string;
  confidence?: number | string;
  snippet?: string;
  evidence?: EvidenceReference;
  [key: string]: unknown;
}

/** GET /api/symbols/{id} — definition evidence: file, line range, commit. */
export interface SymbolDetail {
  id: string;
  name?: string;
  kind?: string;
  type?: string;
  language?: string;
  file?: string;
  path?: string;
  startLine?: number;
  line?: number;
  endLine?: number;
  commit?: string;
  commitSha?: string;
  /** Commit-stamps, when the index records successive versions of the symbol. */
  commits?: Array<string | { sha?: string; commit?: string }>;
  signature?: string;
  doc?: string;
  summary?: string;
  repositoryId?: string;
  repository?: string;
  evidence?: EvidenceReference;
  [key: string]: unknown;
}

/** One evidence reference: exact file, line range, symbol and commit, verbatim. */
export interface EvidenceReference {
  file?: string;
  path?: string;
  startLine?: number;
  line?: number;
  endLine?: number;
  symbol?: string;
  commit?: string;
  commitSha?: string;
  snippet?: string;
  quote?: string;
  confidence?: number | string;
  [key: string]: unknown;
}

/** A reverse dependency. Each carries the edge's source fact: where the
 *  importing/calling side lives (file + line). */
export interface Dependant {
  id?: string;
  symbol?: string;
  name?: string;
  kind?: string;
  /** Edge relation type, e.g. CALLS / IMPORTS / DEPENDS_ON. */
  relation?: string;
  edge?: string;
  type?: string;
  /** Source fact: file + line of the importing/calling side. */
  file?: string;
  path?: string;
  line?: number;
  startLine?: number;
  endLine?: number;
  commit?: string;
  commitSha?: string;
  confidence?: number | string;
  resolved?: boolean;
  note?: string;
  [key: string]: unknown;
}

/** GET /api/commits/{sha}/timeline — files + symbols changed at that commit. */
export interface CommitTimeline {
  sha?: string;
  commit?: string;
  message?: string;
  summary?: string;
  author?: string;
  date?: string;
  timestamp?: string;
  files?: unknown[];
  symbols?: unknown[];
  [key: string]: unknown;
}

/** GET /api/explain?repository=&target= */
export interface ExplainResult {
  answer: string;
  evidence?: EvidenceReference[];
  coverage?: 'full' | 'partial' | 'abstain' | string;
  abstained_reason?: string;
  confidence?: number | string;
  [key: string]: unknown;
}

/* ---------------------------------------------------------------- helpers */

const str = (v: unknown): string | undefined => (typeof v === 'string' && v.length > 0 ? v : undefined);
const num = (v: unknown): number | undefined => (typeof v === 'number' && Number.isFinite(v) ? v : undefined);

export function repoLabel(r: Repository): string {
  return str(r.name) ?? r.id;
}

export function moduleLabel(m: Module): string {
  return str(m.name) ?? m.id;
}

export function moduleSymbolCount(m: Module): number | undefined {
  return num(m.symbolCount ?? m.symbol_count);
}

export function symbolName(s: SymbolSearchResult): string {
  return str(s.name) ?? s.id;
}

export function symbolKind(s: SymbolSearchResult): string | undefined {
  return str(s.kind ?? s.type);
}

export function symbolFile(s: SymbolSearchResult): string | undefined {
  return str(s.file ?? s.path);
}

export function symbolLine(s: SymbolSearchResult): number | undefined {
  return num(s.startLine ?? s.line);
}

export function symbolCommit(s: SymbolSearchResult): string | undefined {
  return str(s.commit ?? s.commitSha);
}

export function symbolConfidence(s: SymbolSearchResult): number | string | undefined {
  return s.confidence;
}

export function detailFile(d: SymbolDetail): string | undefined {
  return str(d.file ?? d.path);
}

export function detailStartLine(d: SymbolDetail): number | undefined {
  return num(d.startLine ?? d.line);
}

export function detailEndLine(d: SymbolDetail): number | undefined {
  return num(d.endLine);
}

export function detailCommit(d: SymbolDetail): string | undefined {
  return str(d.commit ?? d.commitSha);
}

/** Defensive read of the commit-stamp list on a symbol detail record. */
export function detailCommits(d: SymbolDetail | null): string[] {
  if (!d) return [];
  const out: string[] = [];
  const push = (v: unknown) => {
    const s = typeof v === 'string' ? v : v != null && typeof v === 'object' ? str((v as Record<string, unknown>).sha ?? (v as Record<string, unknown>).commit) : undefined;
    if (s) out.push(s);
  };
  if (Array.isArray(d.commits)) d.commits.forEach(push);
  return out;
}

export function evidenceFile(e: EvidenceReference): string | undefined {
  return str(e.file ?? e.path);
}

export function evidenceLine(e: EvidenceReference): number | undefined {
  return num(e.startLine ?? e.line);
}

export function evidenceEndLine(e: EvidenceReference): number | undefined {
  return num(e.endLine);
}

export function evidenceCommit(e: EvidenceReference): string | undefined {
  return str(e.commit ?? e.commitSha);
}

export function evidenceSymbol(e: EvidenceReference): string | undefined {
  return str(e.symbol);
}

/** Normalized state for the index-status document. */
export function normalizeIndexStatus(s: IndexStatus | null): { kind: 'ready' | 'indexing' | 'error' | 'unknown'; label: string } {
  const raw = str(s?.status ?? s?.state);
  if (!raw) return { kind: 'unknown', label: 'status unknown' };
  const l = raw.toLowerCase();
  if (/(ready|complete|completed|up-to-date|fresh|healthy|ok|idle)/.test(l)) return { kind: 'ready', label: raw };
  if (/(indexing|pending|running|in.progress|queued|building)/.test(l)) return { kind: 'indexing', label: raw };
  if (/(error|failed|stale|corrupt)/.test(l)) return { kind: 'error', label: raw };
  return { kind: 'unknown', label: raw };
}

export function explainCoverage(c: string | undefined): 'full' | 'partial' | 'abstain' | 'unknown' {
  switch (c?.toLowerCase()) {
    case 'full':
    case 'complete':
      return 'full';
    case 'partial':
    case 'low':
      return 'partial';
    case 'abstain':
    case 'abstained':
      return 'abstain';
    default:
      return 'unknown';
  }
}

/** Best-effort normalization of cross-module edges from /modules.
 *  Reads the common field spellings; drops anything without both ends. */
export function normalizeEdges(g: ModuleGraph | null): ModuleEdge[] {
  if (!g) return [];
  const raw: unknown = g.edges ?? g.relations ?? g.links;
  if (!Array.isArray(raw)) return [];
  const list = raw as Array<Record<string, unknown>>;
  return list
    .map((e) => ({
      source: str(e.source ?? e.from ?? e.sourceModule ?? e.sourceId),
      target: str(e.target ?? e.to ?? e.targetModule ?? e.targetId),
      relation: str(e.relation ?? e.kind ?? e.type ?? e.edge),
      count: num(e.count ?? e.weight),
    }))
    .filter((e): e is ModuleEdge => Boolean(e && e.source && e.target));
}

/** Normalize commit-timeline files to display labels. */
export function timelineFiles(tl: CommitTimeline | null): string[] {
  if (!tl || !Array.isArray(tl.files)) return [];
  return tl.files
    .map((f) => (typeof f === 'string' ? f : str((f as Record<string, unknown>).path ?? (f as Record<string, unknown>).file)))
    .filter((f): f is string => Boolean(f));
}

export interface TimelineSymbol {
  label: string;
  kind?: string;
  file?: string;
  line?: number;
  commit?: string;
}

export function timelineSymbols(tl: CommitTimeline | null): TimelineSymbol[] {
  if (!tl || !Array.isArray(tl.symbols)) return [];
  return tl.symbols
    .map((s) => {
      if (typeof s === 'string') return { label: s } as TimelineSymbol;
      const r = s as Record<string, unknown>;
      return {
        label: str(r.name ?? r.symbol) ?? str(r.id) ?? String(r),
        kind: str(r.kind ?? r.type),
        file: str(r.file ?? r.path),
        line: num(r.startLine ?? r.line),
        commit: str(r.commit ?? r.commitSha),
      } as TimelineSymbol;
    })
    .filter((s) => s.label !== 'undefined');
}
