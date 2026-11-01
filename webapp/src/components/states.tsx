// Shared, honest state components. Every workspace renders one of these
// instead of a fake stat, so users are never shown fabricated data.
import { useState, type ReactNode } from 'react';
import { copyText } from '@/lib/format';
import { IconCheck, IconCopy, IconError, IconInfo, IconWarning } from './icons';

export function LoadingBlock({ label = 'Loading', compact = false }: { label?: string; compact?: boolean }) {
  return (
    <div
      role="status"
      aria-live="polite"
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 10,
        color: 'var(--text-muted)',
        fontSize: compact ? 0.8125 : 0.875,
        padding: compact ? '8px 4px' : '20px 4px',
      }}
    >
      <span className="spinner" aria-hidden />
      <span>{label}…</span>
    </div>
  );
}

export function EmptyBlock({
  title,
  hint,
  icon,
  children,
}: {
  title: string;
  hint?: string;
  icon?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <div
      aria-live="polite"
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 6,
        padding: '24px 16px',
        border: '1px dashed var(--border-strong)',
        borderRadius: 'var(--radius-md)',
        background: 'var(--accent-surface)',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, color: 'var(--text-secondary)' }}>
        {icon ?? <IconInfo style={{ color: 'var(--text-muted)' }} />}
        <strong style={{ fontWeight: 600 }}>{title}</strong>
      </div>
      {hint && <p style={{ color: 'var(--text-muted)', fontSize: 0.8125, maxWidth: 520 }}>{hint}</p>}
      {children}
    </div>
  );
}

export function ErrorBlock({
  title = 'Something went wrong',
  message,
  onRetry,
}: {
  title?: string;
  message?: string | null;
  onRetry?: () => void;
}) {
  return (
    <div
      role="alert"
      style={{
        display: 'flex',
        gap: 12,
        padding: '16px',
        border: '1px solid color-mix(in srgb, var(--danger) 35%, transparent)',
        background: 'var(--danger-soft)',
        borderRadius: 'var(--radius-md)',
      }}
    >
      <IconError style={{ color: 'var(--danger)', flex: 'none', marginTop: 2 }} />
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        <strong style={{ fontWeight: 600 }}>{title}</strong>
        {message && <p style={{ color: 'var(--text-secondary)', fontSize: 0.8125, maxWidth: 560 }}>{message}</p>}
        {onRetry && (
          <button className="btn btn-ghost" style={{ alignSelf: 'flex-start' }} onClick={onRetry}>
            Try again
          </button>
        )}
      </div>
    </div>
  );
}

/** Neutral notice used for partial/low-confidence and abstain states. */
export function NoticeBlock({
  tone = 'info',
  title,
  children,
}: {
  tone?: 'info' | 'warning';
  title?: string;
  children?: ReactNode;
}) {
  const icon =
    tone === 'warning' ? (
      <IconWarning style={{ color: 'var(--warning)', flex: 'none', marginTop: 2 }} />
    ) : (
      <IconInfo style={{ color: 'var(--info)', flex: 'none', marginTop: 2 }} />
    );
  return (
    <div
      role="note"
      style={{
        display: 'flex',
        gap: 10,
        padding: '12px 14px',
        border: `1px solid color-mix(in srgb, ${tone === 'warning' ? 'var(--warning)' : 'var(--info)'} 35%, transparent)`,
        background: tone === 'warning' ? 'var(--warning-soft)' : 'var(--info-soft)',
        borderRadius: 'var(--radius-md)',
        fontSize: 0.8125,
      }}
    >
      {icon}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4, color: 'var(--text-secondary)' }}>
        {title && <strong style={{ color: 'var(--text-primary)', fontWeight: 600 }}>{title}</strong>}
        {children}
      </div>
    </div>
  );
}

/** A copy-to-clipboard control with transient "copied" feedback. */
export function CopyButton({ value, label = 'Copy' }: { value: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      className="icon-btn"
      style={{ width: 'auto', padding: '0 8px', gap: 6, fontSize: 0.75, height: 24 }}
      onClick={async () => {
        const ok = await copyText(value);
        if (ok) {
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1400);
        }
      }}
      aria-label={label}
      title={label}
    >
      {copied ? <IconCheck style={{ color: 'var(--success)', width: 13, height: 13 }} /> : <IconCopy style={{ width: 13, height: 13 }} />}
      <span style={{ color: copied ? 'var(--success)' : 'inherit' }}>{copied ? 'copied' : label}</span>
    </button>
  );
}
