// Shared scaffolding for the five workspaces: a screen header, a
// "a repository must be selected" gate, and a small label/section helper.
import type { ReactNode } from 'react';
import { useApp } from '@/lib/appContext';
import { EmptyBlock, NoticeBlock } from '@/components/states';

/** Page masthead for a workspace: index number, name and one-line job. */
export function ScreenHead({ index, title, job }: { index: string; title: string; job?: string }) {
  return (
    <header style={{ marginBottom: 20, display: 'flex', flexDirection: 'column', gap: 4 }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 10 }}>
        <span className="label-caps">{index}</span>
        <h1 style={{ fontSize: '1.375rem' }}>{title}</h1>
      </div>
      {job && <p className="text-secondary" style={{ fontSize: '0.875rem', maxWidth: 720 }}>{job}</p>}
    </header>
  );
}

/** Render children only when a repository is selected; otherwise an honest
 *  gate so a workspace never silently shows a different repo's data. */
export function RequireRepo({ index, children }: { index: string; children: ReactNode }) {
  const app = useApp();
  if (!app.repository) {
    return (
      <EmptyBlock
        title={`No repository selected (${index})`}
        hint="Pick a repository from the selector in the top bar. If none are listed, the service is reachable but has not indexed one yet — index a repository with the CLI first."
      />
    );
  }
  return <>{children}</>;
}

/** A section heading + rule inside a workspace. */
export function Section({ title, children, hint }: { title: string; children: ReactNode; hint?: string }) {
  return (
    <section className="panel screen-enter" style={{ marginTop: 20 }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, marginBottom: 12 }}>
        <span className="label-caps">{title}</span>
        {hint && <span className="text-muted" style={{ fontSize: 0.75 }}>{hint}</span>}
      </div>
      {children}
    </section>
  );
}

export { NoticeBlock };
