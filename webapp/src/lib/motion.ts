// Motion utilities: a critically-damped spring integrator, momentum
// projection and media-query hooks. No external animation library — the
// spring maths is deterministic, interruptible and cheap at 60fps.
import { useEffect, useState } from 'react';

export interface SpringParams {
  stiffness: number;
  damping: number;
  mass: number;
}

export interface SpringState {
  x: number;
  v: number;
  target: number;
}

/** Critically damped (calm, no overshoot) spring by default. */
export function springParams(overrides?: Partial<SpringParams>): SpringParams {
  const stiffness = overrides?.stiffness ?? 170;
  const mass = overrides?.mass ?? 1;
  const damping = overrides?.damping ?? 2 * Math.sqrt(stiffness * mass);
  return { stiffness, damping, mass };
}

/**
 * Advance a spring by `dt` seconds, in place. Sub-steps for stability.
 * Returns true when settled.
 */
export function stepSpring(state: SpringState, dt: number, params: SpringParams): boolean {
  const { stiffness, damping, mass } = params;
  const maxStep = 1 / 120;
  let remaining = Math.min(dt, 0.1);
  while (remaining > 0) {
    const h = Math.min(maxStep, remaining);
    remaining -= h;
    const a = (-stiffness * (state.x - state.target) - damping * state.v) / mass;
    state.v += a * h;
    state.x += state.v * h;
  }
  const settled = Math.abs(state.v) < 0.002 && Math.abs(state.x - state.target) < 0.002;
  if (settled) {
    state.x = state.target;
    state.v = 0;
  }
  return settled;
}

/**
 * Momentum projection: estimate the landing point after a release with
 * velocity `v` (px/ms). Exponential decay over a 300ms window gives the
 * displacement; used to pick the snap target instead of snapping from the
 * release position.
 */
export function projectMomentum(from: number, vPxPerMs: number): number {
  const windowMs = 300;
  const decay = 8; // 1/s
  const displacement = (vPxPerMs * windowMs) * ((1 - Math.exp(-decay * (windowMs / 1000))) / (decay * (windowMs / 1000))) * 0.5;
  return from + displacement;
}

export function useReducedMotion(): boolean {
  const [reduced, setReduced] = useState(
    () => typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches,
  );
  useEffect(() => {
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)');
    const onChange = () => setReduced(mq.matches);
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  }, []);
  return reduced;
}

/** requestAnimationFrame loop that only runs while `active` is true. */
export function useRafFrame(onFrame: (dt: number) => void, active: boolean): void {
  useEffect(() => {
    if (!active) return;
    let id = 0;
    let alive = true;
    let last = performance.now();
    const loop = () => {
      if (!alive) return;
      const now = performance.now();
      onFrame((now - last) / 1000);
      last = now;
      id = requestAnimationFrame(loop);
    };
    id = requestAnimationFrame(loop);
    return () => {
      alive = false;
      cancelAnimationFrame(id);
    };
  }, [active, onFrame]);
}
