// Explanation panel: /explain for a selected symbol or module. The answer is
// shown with every EvidenceReference verbatim; coverage makes it visually
// unambiguous which parts are measured indexed facts and which are the
// derived/summary layer. Abstain shows the reason plainly; partial marks
// the gap.
import { useEffect, useMemo, useState } from 'react';
import { api } from '@/lib/api';
import { useApp } from '@/lib/appContext';
import { useQuery } from '@/lib/useQuery';
import { useRoute, navigate } from '@/lib/router';
import {
  explainCoverage,
  type ExplainResult,
  type EvidenceReference,
} from '@/lib/types';
import { EmptyBlock, ErrorBlock, LoadingBlock, NoticeBlock } from '@/components/states';
import { EvidenceList } from '@/components/EvidenceReference';
import { IconExplain } from '@/components/icons';
import { ScreenHead, Section } from './screen';

export function ExplainerWorkspace() {
  const app = useApp();
  const repo = app.repository;
  const route = useRoute();

  const target = route.params.target ?? '';
  const [targetDraft, setTargetDraft] = useState(target);

  // Keep the draft field in sync with the route.
  useEffect(() => setTargetDraft(target), [target]);

  // The explain call targets the selected repository; if the deep link
  // carried an explicit repo segment, honour it (the contract takes repository=…).
  const repoParam = route.params.repo;
  const effectiveRepo = repoParam && repo ? repoParam : repo?.id ?? null;

  const result = useQuery<ExplainResult>(
    (signal) =>
      effectiveRepo && target
        ? api.explain(effectiveRepo, target, signal)
        : Promise.reject(new Error('No target to explain')),
    `explain:${effectiveRepo ?? 'none'}:${target}`,
    { enabled: Boolean(effectiveRepo && target) },
  );

  const data = result.data;
  const coverage: 'full' | 'partial' | 'abstain' | 'unknown' = data ? explainCoverage(data.coverage) : 'unknown';
  const evidence = useMemo<EvidenceReference[]>(() => data?.evidence ?? [], [data]);
  const confidence = data?.confidence;

  const setTarget = (value: string) => {
    const v = value.trim();
    if (!v) {
      navigate('explainer');
      return;
    }
    // The hash router is segment-based: travel the target (and an explicit
    // repo, when deep-linking across repos) as `key=value` path segments.
    const segs = [`target=${encodeURIComponent(v)}`];
    if (repoParam && repoParam !== repo?.id) segs.push(`repo=${encodeURIComponent(repoParam)}`);
    navigate(`explainer/${segs.join('/')}`);
  };

  const missing = coverage === 'abstain' || (coverage === 'partial' && evidence.length === 0);

  return (
    <>
      <ScreenHead
        index="05 · Explain"
        title="Explanation panel"
        job="An evidence-linked explanation for a symbol or module. Every claim is tied to verbatim evidence; when the service abstains, the reason and the missing evidence are shown plainly."
      />

      <Section title="Target" hint={repo ? `repository · ${repo.id}` : 'no repository selected'}>
        <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap', alignItems: 'flex-end' }}>
          <label style={{ display: 'flex', flexDirection: 'column', gap: 6, flex: '1 1 300px' }}>
            <span className="label-caps">Symbol or module</span>
            <input
              className="input"
              value={targetDraft}
              onChange={(e) => setTargetDraft(e.target.value)}
              onBlur={() => setTarget(targetDraft)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') setTarget(targetDraft);
              }}
              placeholder="symbol id, symbol name, or module id"
              aria-label="Symbol or module to explain"
            />
          </label>
          <button className="btn btn-primary" style={{ height: 40 }} disabled={!effectiveRepo || !targetDraft.trim()} onClick={() => setTarget(targetDraft)}>
            Explain
          </button>
        </div>
        {!target ? (
          <div style={{ marginTop: 10 }}>
            <EmptyBlock
              icon={<IconExplain />}
              title="Nothing to explain yet"
              hint="Pick a symbol from the Inspect workspace or a module from the Architect workspace — they link here — or type a target above."
            />
          </div>
        ) : null}
      </Section>

      {target && (
        <Section
          title="Explanation"
          hint={coverage !== 'unknown' ? `coverage · ${coverage}` : 'derived summary layer — not measured fact'}
        >
          {result.error ? (
            <ErrorBlock
              title="Could not explain this target"
              message={result.error}
              onRetry={result.reload}
            />
          ) : !data ? (
            result.loading ? (
              <LoadingBlock label="Composing an evidence-linked explanation" />
            ) : (
              <EmptyBlock
                title="No explanation available"
                hint="The service did not return an explanation for this target. Rather than guess, the panel stays empty and says so."
              />
            )
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
              {/* Coverage strip — makes the fact/summary split unambiguous. */}
              <CoverageStrip coverage={coverage} abstainedReason={data.abstained_reason} />

              {coverage === 'abstain' ? (
                <NoticeBlock
                  tone="warning"
                  title="The service abstained — not enough evidence"
                >
                  {data.abstained_reason || 'No abstained_reason was supplied with the response.'}
                  {missing && (
                    <>
                      <span style={{ marginTop: 4 }}>
                        No evidence references were returned with this answer, so there is nothing verbatim to show.
                      </span>
                    </>
                  )}
                </NoticeBlock>
              ) : (
                <>
                  <div
                    style={{
                      border: '1px solid var(--border-subtle)',
                      borderRadius: 'var(--radius-md)',
                      background: 'var(--surface-2)',
                      padding: 16,
                      fontSize: 0.9375,
                      color: 'var(--text-primary)',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
                      <span className="label-caps">Answer — derived/summary layer</span>
                      {confidence != null && <span className="chip">confidence · {String(confidence)}</span>}
                    </div>
                    <p style={{ whiteSpace: 'pre-wrap' }}>{data.answer}</p>
                  </div>

                  <div style={{ borderTop: '1px solid var(--border-subtle)', paddingTop: 14 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
                      <span className="label-caps">Evidence — measured indexed facts</span>
                      {evidence.length === 0 && <span className="text-muted" style={{ fontSize: 0.75 }}>none returned</span>}
                    </div>
                    {evidence.length ? (
                      <EvidenceList items={evidence} title="" />
                    ) : (
                      <NoticeBlock tone="warning" title="Answer returned with no evidence">
                        This explanation arrived without any evidence references, so its claims cannot be verified against the index.
                        Treat it as an unverified summary.
                      </NoticeBlock>
                    )}
                  </div>

                  {coverage === 'partial' && (
                    <NoticeBlock
                      tone="warning"
                      title="Partial coverage — some evidence is missing"
                    >
                      {data.abstained_reason
                        ? data.abstained_reason
                        : 'The service flagged this answer as partial. The evidence above is what it has; the gap it could not fill is not shown here because the contract does not supply it verbatim.'}
                    </NoticeBlock>
                  )}
                </>
              )}
            </div>
          )}
        </Section>
      )}
    </>
  );
}

function CoverageStrip({
  coverage,
  abstainedReason,
}: {
  coverage: 'full' | 'partial' | 'abstain' | 'unknown';
  abstainedReason?: string;
}) {
  const meta: Record<typeof coverage, { tone: 'success' | 'warning' | 'danger'; label: string; sub: string }> = {
    full: {
      tone: 'success',
      label: 'Full coverage',
      sub: 'The claims below are backed by the measured indexed facts shown in the evidence pane.',
    },
    partial: {
      tone: 'warning',
      label: 'Partial coverage',
      sub: 'Some of this explanation is a derived summary the index could not fully back. The gap is marked below.',
    },
    abstain: {
      tone: 'danger',
      label: 'Abstained',
      sub: abstainedReason || 'The service declined to answer because its evidence was insufficient. The reason is shown below.',
    },
    unknown: {
      tone: 'warning',
      label: 'Coverage not reported',
      sub: 'The service did not report a coverage level for this answer; treat it as an unmeasured summary until it does.',
    },
  };
  const m = meta[coverage];
  return (
    <div
      role="note"
      style={{
        display: 'flex',
        gap: 10,
        padding: '12px 14px',
        borderRadius: 'var(--radius-md)',
        border: `1px solid color-mix(in srgb, var(--${toneToVar(m.tone)}) 35%, transparent)`,
        background: `var(--${toneToVar(m.tone)}-soft)`,
        fontSize: 0.8125,
      }}
    >
      <span
        className="status-dot"
        data-state={m.tone === 'success' ? 'ready' : m.tone === 'warning' ? 'indexing' : 'error'}
        style={{ marginTop: 3 }}
      />
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <strong style={{ color: `var(--${toneToVar(m.tone)})`, fontWeight: 600 }}>{m.label}</strong>
        <span style={{ color: 'var(--text-secondary)' }}>{m.sub}</span>
      </div>
    </div>
  );
}

function toneToVar(tone: 'success' | 'warning' | 'danger') {
  return { success: 'success', warning: 'warning', danger: 'danger' }[tone];
}
