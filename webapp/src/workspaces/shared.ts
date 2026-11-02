// Small helpers shared by the workspaces.
import {
  detailCommits,
  detailEndLine,
  detailFile,
  detailStartLine,
  symbolCommit,
  type SymbolDetail,
  type SymbolSearchResult,
} from '@/lib/types';
import { shortSha } from '@/lib/format';

/** The commit stamps a symbol detail record already carries, newest last. */
export function symbolSnapshots(d: SymbolDetail | null): string[] {
  const list = d ? detailCommits(d) : [];
  return list.length ? list : [];
}

/** Stable snapshot list: merge detail commits + the definition's own commit. */
export function symbolSnapshotsWithDefinition(d: SymbolDetail | null): string[] {
  const own = d ? (detailCommitSafe(d) ?? undefined) : undefined;
  const list = d ? detailCommits(d) : [];
  const out: string[] = [...list];
  if (own && !out.includes(own)) out.push(own);
  return out;
}

function detailCommitSafe(d: SymbolDetail): string | null | undefined {
  return (d.commit ?? d.commitSha) as string | null | undefined;
}

/** A commit stamp taken from a search result. */
export function resultCommit(r: SymbolSearchResult): string | null {
  const c = symbolCommit(r);
  return c ? c : null;
}

/** A copyable `path:Lstart[-Lend]` value — a verbatim reference, no permalink. */
export function copyableRef(file: string | undefined, start: number | undefined, end?: number | undefined): string {
  if (!file) return '';
  if (start == null) return file;
  return end != null && end !== start ? `${file}:${start}-${end}` : `${file}:${start}`;
}

export { shortSha };

/** Build a copyable definition reference from a symbol detail. */
export function detailCopyRef(d: SymbolDetail): string {
  return copyableRef(detailFile(d), detailStartLine(d), detailEndLine(d));
}
