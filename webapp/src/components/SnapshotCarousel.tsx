// Horizontal carousel for a symbol's commit-snapshot evolution. This is the
// one place a carousel is used: flipping between successive commits of one
// symbol, driven by the git-history workspace's data.
//
// Interaction spec (premium carousel): pointer-capture drag 1:1 with the
// grab offset honoured; release velocity handed to a critically-damped
// spring from motion.ts; momentum projection picks the landing snap point;
// the spring is interruptible and direction-reversible mid-flight; a partial
// next-item affordance; keyboard arrows and accessible prev/next buttons;
// reduced-motion jumps without the flick; no autoplay, ever.
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from 'react';
import {
  projectMomentum,
  springParams,
  stepSpring,
  useRafFrame,
  useReducedMotion,
  type SpringState,
} from '@/lib/motion';
import { IconChevronLeft, IconChevronRight } from './icons';

export interface SnapshotCarouselProps {
  /** One entry per commit snapshot of the symbol. */
  items: ReactNode[];
  /** Accessible name for each slide. */
  labels: string[];
  /** Slide width in px — consistent item widths per spec. */
  itemWidth?: number;
  itemGap?: number;
  /** How much of the next item peeks in (the partial affordance). */
  peek?: number;
  onIndexChange?: (i: number) => void;
  initialIndex?: number;
}

export function SnapshotCarousel({
  items,
  labels,
  itemWidth = 320,
  itemGap = 16,
  peek = 64,
  onIndexChange,
  initialIndex = 0,
}: SnapshotCarouselProps) {
  const n = items.length;
  const reduced = useReducedMotion();
  const step = itemWidth + itemGap;

  const clampIndex = useCallback((i: number) => Math.max(0, Math.min(Math.max(0, n - 1), i)), [n]);
  const targetFor = useCallback((i: number) => -i * step, [step]);

  const [index, setIndex] = useState(() => clampIndex(initialIndex));
  const [offset, setOffset] = useState(() => targetFor(initialIndex));
  const [dragging, setDragging] = useState(false);
  const [settling, setSettling] = useState(false);

  const spring = useRef<SpringState>({
    x: targetFor(initialIndex),
    v: 0,
    target: targetFor(initialIndex),
  });
  // Synchronous drag state — React state is too slow to track pointer events.
  const drag = useRef({
    active: false,
    pointerId: -1,
    startX: 0,
    startOffset: 0,
    liveOffset: 0,
    lastX: 0,
    lastT: 0,
    velocity: 0,
  });
  const trackRef = useRef<HTMLDivElement>(null);

  const settleTo = useCallback(
    (i: number, viaSpring: boolean) => {
      const landing = clampIndex(i);
      setIndex(landing);
      onIndexChange?.(landing);
      if (!viaSpring || reduced) {
        // Reduced motion (or an explicit non-animated jump): go straight.
        const t = targetFor(landing);
        spring.current = { x: t, v: 0, target: t };
        setOffset(t);
        setSettling(false);
      } else {
        spring.current.target = targetFor(landing);
        spring.current.v = 0;
        setSettling(true);
      }
    },
    [clampIndex, targetFor, reduced, onIndexChange],
  );

  // ---- spring settle loop (only runs while settling && !dragging) --------
  const onFrame = useCallback((dt: number) => {
    const s = spring.current;
    const settled = stepSpring(s, dt, springParams());
    setOffset(s.x);
    if (settled) setSettling(false);
  }, []);
  useRafFrame(onFrame, settling && !dragging && !reduced);

  // ---- pointer: down (React), move (React), up/cancel (native, captured) --
  const onPointerDown = useCallback(
    (e: ReactPointerEvent<HTMLDivElement>) => {
      if (n <= 1) return;
      const el = trackRef.current;
      if (!el) return;
      el.setPointerCapture(e.pointerId);
      const d = drag.current;
      d.active = true;
      d.pointerId = e.pointerId;
      d.startX = e.clientX;
      d.startOffset = offset;
      d.liveOffset = offset;
      d.lastX = e.clientX;
      d.lastT = performance.now();
      d.velocity = 0;
      // Start the spring from the live on-screen value, not a stale target.
      spring.current.x = offset;
      spring.current.v = 0;
      spring.current.target = offset;
      setSettling(false);
      setDragging(true);
    },
    [n, offset],
  );

  const onPointerMove = useCallback(
    (e: ReactPointerEvent<HTMLDivElement>) => {
      const d = drag.current;
      if (!d.active || e.pointerId !== d.pointerId) return;
      const dx = e.clientX - d.startX;
      // Clamp with a little rubber-band past the ends.
      const min = targetFor(n - 1);
      const max = targetFor(0);
      let next = d.startOffset + dx;
      if (next > max) next = max + (next - max) * 0.25;
      if (next < min) next = min + (next - min) * 0.25;
      d.liveOffset = next;
      setOffset(next);
      const t = performance.now();
      const dt = t - d.lastT;
      if (dt > 0) {
        d.velocity = (e.clientX - d.lastX) / dt; // px/ms, sign = drag direction
        d.lastX = e.clientX;
        d.lastT = t;
      }
    },
    [n, targetFor],
  );

  useEffect(() => {
    if (!dragging) return;
    const el = trackRef.current;
    if (!el) return;
    const onUp = (e: PointerEvent) => {
      const d = drag.current;
      if (!d.active || e.pointerId !== d.pointerId) return;
      d.active = false;
      if (el.hasPointerCapture(e.pointerId)) el.releasePointerCapture(e.pointerId);
      setDragging(false);
      // Momentum projection picks the landing snap point from the release
      // velocity — never just the release position — and the release velocity
      // is handed to the spring (critically damped, no overshoot by default).
      const proj = projectMomentum(d.liveOffset, d.velocity);
      const landing = clampIndex(Math.round(-proj / step));
      // Resume the spring from the live on-screen value (not a stale target)
      // and hand the release velocity to it — critically damped, interruptible.
      spring.current.x = d.liveOffset;
      spring.current.target = targetFor(landing);
      spring.current.v = d.velocity * 1000; // px/ms → px/s
      setIndex(landing);
      onIndexChange?.(landing);
      if (!reduced) setSettling(true);
      else {
        const t = targetFor(landing);
        spring.current = { x: t, v: 0, target: t };
        setOffset(t);
      }
    };
    el.addEventListener('pointerup', onUp);
    el.addEventListener('pointercancel', onUp);
    return () => {
      el.removeEventListener('pointerup', onUp);
      el.removeEventListener('pointercancel', onUp);
    };
  }, [dragging, clampIndex, step, targetFor, onIndexChange, reduced]);

  // ---- keyboard + accessible controls ------------------------------------
  const go = useCallback((delta: number) => settleTo(index + delta, true), [index, settleTo]);

  const onKeyDown = useCallback(
    (e: ReactKeyboardEvent<HTMLDivElement>) => {
      if (e.key === 'ArrowLeft') {
        e.preventDefault();
        go(-1);
      } else if (e.key === 'ArrowRight') {
        e.preventDefault();
        go(1);
      } else if (e.key === 'Home') {
        e.preventDefault();
        settleTo(0, true);
      } else if (e.key === 'End') {
        e.preventDefault();
        settleTo(n - 1, true);
      }
    },
    [go, settleTo, n],
  );

  const canDrag = n > 1;
  const trackWidth = itemWidth + peek;

  return (
    <div
      style={{ position: 'relative', userSelect: 'none', touchAction: 'pan-y' }}
      role="region"
      aria-roledescription="carousel"
      aria-label={`Commit snapshots, showing ${index + 1} of ${n}`}
    >
      <div
        ref={trackRef}
        role="group"
        aria-roledescription="carousel slide"
        aria-label={`Snapshot ${index + 1} of ${n}: ${labels[index] ?? ''}`}
        tabIndex={canDrag ? 0 : undefined}
        onKeyDown={onKeyDown}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        style={{
          overflow: 'hidden',
          width: trackWidth,
          maxWidth: '100%',
          cursor: canDrag ? (dragging ? 'grabbing' : 'grab') : 'default',
          borderRadius: 'var(--radius-md)',
        }}
      >
        <div
          style={{
            display: 'flex',
            gap: itemGap,
            transform: `translate3d(${offset}px, 0, 0)`,
            willChange: 'transform',
          }}
        >
          {items.map((it, i) => (
            <div
              key={i}
              style={{
                flex: '0 0 auto',
                width: itemWidth,
                outline: 'none',
                borderRadius: 'var(--radius-md)',
              }}
              aria-hidden={i !== index || undefined}
            >
              {it}
            </div>
          ))}
        </div>
      </div>

      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          marginTop: 10,
          justifyContent: 'space-between',
        }}
      >
        <button
          className="icon-btn"
          disabled={index === 0}
          aria-label="Previous commit snapshot"
          onClick={() => go(-1)}
          style={{ width: 34, height: 34 }}
        >
          <IconChevronLeft width={16} height={16} />
        </button>
        <div style={{ display: 'flex', gap: 5, flexWrap: 'wrap' }} aria-hidden>
          {labels.map((l, i) => (
            <button
              key={i}
              className="icon-btn"
              aria-label={`Go to snapshot ${i + 1}: ${l}`}
              aria-current={i === index ? 'true' : undefined}
              onClick={() => settleTo(i, true)}
              style={{
                width: 10,
                height: 10,
                borderRadius: '50%',
                border: '1px solid var(--border-strong)',
                background: i === index ? 'var(--accent)' : 'transparent',
              }}
            />
          ))}
        </div>
        <button
          className="icon-btn"
          disabled={index === n - 1}
          aria-label="Next commit snapshot"
          onClick={() => go(1)}
          style={{ width: 34, height: 34 }}
        >
          <IconChevronRight width={16} height={16} />
        </button>
      </div>
    </div>
  );
}
