/** Short commit display: full SHA when >= 7 chars, else as-is. */
export function shortSha(sha: string | undefined | null, len = 8): string {
  if (!sha) return '';
  const s = sha.trim();
  return s.length > len ? s.slice(0, len) : s;
}

/** Monospace-safe truncation for paths in dense tables. */
export function truncateMiddle(s: string, max = 40): string {
  if (s.length <= max) return s;
  const half = Math.floor((max - 1) / 2);
  return `${s.slice(0, half)}…${s.slice(-half)}`;
}

export function formatDate(iso: string | undefined | null): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
}

export function formatDateTime(iso: string | undefined | null): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export function plural(n: number, one: string, many?: string): string {
  return `${n} ${n === 1 ? one : (many ?? `${one}s`)}`;
}

export function labelForEmpty(value: string | undefined | null): string {
  return value || '—';
}

/** Confidence label: low-confidence is surfaced explicitly, never hidden. */
export function confidenceLabel(c: number | string | undefined | null): { text: string; low: boolean } | null {
  if (c == null || c === '') return null;
  const n = typeof c === 'string' ? Number.parseFloat(c) : c;
  if (Number.isNaN(n)) {
    const text = String(c).toLowerCase();
    return { text: text, low: /low|unresolv|unknown|dynamic/.test(text) };
  }
  const low = n < 0.6;
  return { text: n >= 1 ? `${Math.round(n * 100)}%` : n.toFixed(2), low };
}

/** Copy to clipboard with a short "copied" feedback value toggle helper. */
export async function copyText(value: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(value);
    return true;
  } catch {
    // Fallback for non-secure contexts.
    try {
      const ta = document.createElement('textarea');
      ta.value = value;
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
      return true;
    } catch {
      return false;
    }
  }
}
