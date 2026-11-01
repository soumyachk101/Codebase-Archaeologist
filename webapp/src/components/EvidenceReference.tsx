// Provenance display: renders an EvidenceReference exactly as the service
// returned it (file, line range, symbol, commit). No fabricated permalinks —
// users copy the path themselves.
import { memo } from 'react';
import { copyText, confidenceLabel, shortSha } from '@/lib/format';
import {
  evidenceCommit,
  evidenceEndLine,
  evidenceFile,
  evidenceLine,
  evidenceSymbol,
  type EvidenceReference,
} from '@/lib/types';
import { CopyButton } from './states';

function lineRange(e: EvidenceReference): string {
  const s = evidenceLine(e);
  const en = evidenceEndLine(e);
  if (s == null) return '';
  return en != null && en !== s ? `L${s}–L${en}` : `L${s}`;
}

export const EvidenceRef = memo(function EvidenceRef({
  evidence,
  dense = false,
}: {
  evidence: EvidenceReference;
  dense?: boolean;
}) {
  const file = evidenceFile(evidence);
  const lines = lineRange(evidence);
  const sym = evidenceSymbol(evidence);
  const commit = evidenceCommit(evidence);
  const snippet = evidence.snippet ?? evidence.quote;
  const conf = confidenceLabel(evidence.confidence);

  const copyPath = file ? `${file}${lines ? `:${lines.replace('L', '')}` : ''}` : '';

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: dense ? 4 : 8,
        fontFamily: 'var(--font-mono)',
        fontSize: dense ? 0.75 : 0.8125,
        color: 'var(--text-secondary)',
        fontVariantNumeric: 'tabular-nums',
      }}
    >
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, alignItems: 'center' }}>
        {file ? (
          <>
            <span style={{ color: 'var(--text-primary)' }}>{file}</span>
            {lines && <span style={{ color: 'var(--text-muted)' }}>{lines}</span>}
          </>
        ) : (
          <span style={{ color: 'var(--text-muted)' }}>file not recorded</span>
        )}
        {commit && <span className="chip" data-tone="info">{shortSha(commit)}</span>}
        {conf && conf.low && <span className="chip" data-tone="warning">low conf · {conf.text}</span>}
        {file && <CopyButton value={copyPath} label="copy path" />}
      </div>
      {sym && <div style={{ color: 'var(--text-muted)' }}>{sym}</div>}
      {snippet && (
        <pre
          style={{
            margin: 0,
            padding: dense ? 6 : 8,
            background: 'var(--surface-2)',
            border: '1px solid var(--border-subtle)',
            borderRadius: 'var(--radius-sm)',
            overflow: 'auto',
            fontSize: dense ? 0.75 : 0.8125,
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-word',
            color: 'var(--text-primary)',
          }}
        >
          {snippet}
        </pre>
      )}
    </div>
  );
});

/** Stacked list of evidence references — the provenance panel. */
export function EvidenceList({
  items,
  dense = false,
  title,
}: {
  items: EvidenceReference[];
  dense?: boolean;
  title?: string;
}) {
  if (!items.length) {
    return <div style={{ color: 'var(--text-muted)', fontSize: 0.8125 }}>No evidence recorded.</div>;
  }
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      {title && <span className="label-caps">{title}</span>}
      {items.map((e, i) => (
        <div
          key={i}
          style={{
            border: '1px solid var(--border-subtle)',
            borderRadius: 'var(--radius-sm)',
            padding: dense ? 8 : 10,
            background: 'var(--surface-2)',
          }}
        >
          <EvidenceRef evidence={e} dense={dense} />
        </div>
      ))}
    </div>
  );
}

/** A row used in dense tables (search results, dependants). */
export function EvidenceRow({
  file,
  line,
  commit,
  symbol,
  confidence,
}: {
  file?: string;
  line?: string | number;
  commit?: string;
  symbol?: string;
  confidence?: number | string;
}) {
  const conf = confidenceLabel(confidence);
  const path = file ? `${file}${line != null && line !== '' ? `:${line}` : ''}` : '';
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
      {file ? (
        <code style={{ fontSize: 0.75, color: 'var(--text-primary)' }}>{file}</code>
      ) : (
        <span style={{ color: 'var(--text-muted)', fontSize: 0.8125 }}>—</span>
      )}
      {line != null && line !== '' && <span className="mono-sha">{line}</span>}
      {commit && <span className="mono-sha">{shortSha(commit)}</span>}
      {symbol && <span className="mono-sha" style={{ color: 'var(--text-muted)' }}>{symbol}</span>}
      {conf && conf.low && <span className="chip" data-tone="warning">low · {conf.text}</span>}
      {path && <CopyButton value={path} label="copy" />}
    </div>
  );
}
