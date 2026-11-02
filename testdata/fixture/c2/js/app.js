import { add, mul } from "./util.js";
// external: not in the index, stays an unresolved reference (never invented)
import { format } from "some-external-lib";

export function total(x, y, factor) {
  const s = add(x, y);
  const p = mul(s, factor);
  return format(p);
}

// scale is the only addition in commit 2 — the incremental test depends on
// this being the single changed file.
export function scale(v, n) {
  return mul(v, n);
}
