/**
 * Pure helpers for drawing wires on the Flow Studio canvas by dragging from an
 * output port to an input port. Free of Angular so they run under node --test.
 */

/** Same shape and spacing as the declared pins in function-pins.ts (imports with extensions break the Angular build). */
export interface Pin { name: string; type: string }
const pinOffset = (index: number, count: number, height: number) => Math.round(height * (index + 1) / (count + 1));

export const NODE_WIDTH = 125;
export const NODE_HEIGHT = 70;
export const PORT_HIT_RADIUS = 14;

export interface WireNode { id: string; type: string; x: number; y: number }
export interface WireEdge { from: string; to: string; from_port?: string; to_port?: string }
export interface PortRef { nodeId: string; side: 'in' | 'out'; pin?: string }
export interface PortPoint extends PortRef { x: number; y: number; type?: string }
export type PinsOf = (node: WireNode) => { inputs: Pin[]; outputs: Pin[] };

/** Every connectable port on the canvas with its position. Typed pins replace the default port. */
export function portPoints(nodes: readonly WireNode[], pinsOf: PinsOf): PortPoint[] {
  const points: PortPoint[] = [];
  for (const n of nodes) {
    const pins = pinsOf(n);
    if (n.type !== 'trigger') {
      if (pins.inputs.length) pins.inputs.forEach((p, i) => points.push({ nodeId: n.id, side: 'in', pin: p.name, type: p.type, x: n.x, y: n.y + pinOffset(i, pins.inputs.length, NODE_HEIGHT) }));
      else points.push({ nodeId: n.id, side: 'in', x: n.x, y: n.y + NODE_HEIGHT / 2 });
    }
    if (n.type !== 'output') {
      if (pins.outputs.length) pins.outputs.forEach((p, i) => points.push({ nodeId: n.id, side: 'out', pin: p.name, type: p.type, x: n.x + NODE_WIDTH, y: n.y + pinOffset(i, pins.outputs.length, NODE_HEIGHT) }));
      else points.push({ nodeId: n.id, side: 'out', x: n.x + NODE_WIDTH, y: n.y + NODE_HEIGHT / 2 });
    }
  }
  return points;
}

/** The closest port of the given side within the hit radius, if any. */
export function nearestPort(points: readonly PortPoint[], x: number, y: number, side: 'in' | 'out', radius = PORT_HIT_RADIUS): PortPoint | null {
  let best: PortPoint | null = null, bestDistance = radius;
  for (const p of points) {
    if (p.side !== side) continue;
    const d = Math.hypot(p.x - x, p.y - y);
    if (d <= bestDistance) { best = p; bestDistance = d; }
  }
  return best;
}

export type WireResult = { edges: WireEdge[]; edge: WireEdge } | { problem: string };

/**
 * Turns a finished drag into the edge list. Pin-to-pin wires must agree on
 * type and a pin takes one source; plain wires reject duplicates.
 */
export function wireFromDrag(edges: readonly WireEdge[], source: PortPoint, target: PortPoint): WireResult {
  if (source.side !== 'out' || target.side !== 'in') return { problem: 'Drag from an output port to an input port.' };
  if (source.nodeId === target.nodeId) return { problem: 'A node cannot connect to itself.' };
  if (source.type && target.type && source.type !== target.type) return { problem: `Cannot connect ${source.type} output "${source.pin}" to ${target.type} input "${target.pin}".` };
  const edge: WireEdge = { from: source.nodeId, to: target.nodeId };
  if (source.pin) edge.from_port = 'out:' + source.pin;
  if (target.pin) edge.to_port = 'in:' + target.pin;
  const same = (a: WireEdge, b: WireEdge) => a.from === b.from && a.to === b.to && (a.from_port || '') === (b.from_port || '') && (a.to_port || '') === (b.to_port || '');
  if (edges.some(e => same(e, edge))) return { problem: 'These ports are already connected.' };
  const rest = target.pin
    // A typed pin takes one source and cannot be mixed with an untyped input.
    ? edges.filter(e => !(e.to === target.nodeId && (e.to_port === 'in:' + target.pin || !e.to_port || e.to_port === 'input')))
    : edges.filter(e => !(e.to === target.nodeId && e.to_port?.startsWith('in:')));
  return { edges: [...rest, edge], edge };
}

/** Stable key of an edge for rendering; several pin wires may join the same two nodes. */
export function edgeKey(e: WireEdge): string {
  return `${e.from}:${e.from_port || ''}>${e.to}:${e.to_port || ''}`;
}

/** Cubic path between two points, matching the committed wires. */
export function wirePath(x1: number, y1: number, x2: number, y2: number): string {
  const d = Math.max(45, Math.abs(x2 - x1) * 0.45);
  return `M ${x1} ${y1} C ${x1 + d} ${y1}, ${x2 - d} ${y2}, ${x2} ${y2}`;
}
