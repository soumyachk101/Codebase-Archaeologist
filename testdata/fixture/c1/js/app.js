import { add, mul } from "./util.js";
// external: not in the index, stays an unresolved reference (never invented)
import { format } from "some-external-lib";

export function total(x, y, factor) {
  const s = add(x, y);
  const p = mul(s, factor);
  return format(p);
}
