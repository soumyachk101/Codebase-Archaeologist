// shapes.ts — exported geometry surface (TypeScript).
export interface Shape {
  area(): number;
}

// circleArea is the single exported function draw.ts calls across the module.
export function circleArea(r: number): number {
  return 3.14159 * r * r;
}
