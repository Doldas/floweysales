/**
 * Pure geometry and presentation helpers shared by every graph surface (Flow
 * Editor, Functions, Triggers, execution snapshots). No Angular imports so they
 * can be tested with `node --test`.
 */

/** Canonical node card size in canvas units. Every graph surface uses it. */
export const NODE_WIDTH = 125;
export const NODE_HEIGHT = 70;

export type DiagramTone = 'ok' | 'fail' | 'run' | 'skip' | 'none';
export type DiagramEdgeKind = 'data' | 'exec' | 'event';

export interface DiagramPort { name: string; type?: string }

export interface DiagramNode {
  id: string;
  x: number;
  y: number;
  /** Semantic kind; selects the accent color (`data-type`). */
  kind: string;
  /** Upper-case caption such as "ƒ FUNCTION". */
  tag: string;
  label: string;
  /** Optional second line (status, payload type). */
  sub?: string;
  tone?: DiagramTone;
  inputs?: DiagramPort[];
  outputs?: DiagramPort[];
  /** Adds to the generated accessible name (for example a run status). */
  ariaLabel?: string;
  /** Replaces the generated accessible name; the Flow Editor names a node "Select <label>". */
  accessibleName?: string;
  /**
   * One connection port in the middle of the input (or output) side, for nodes that are wired
   * without typed pins. The text is the port's accessible name. Typed pins in `inputs` and
   * `outputs` replace it.
   */
  inPort?: string;
  outPort?: string;
  /** Class of the second text line, for example a live run badge. */
  subClass?: string;
  /** The second line already states the state, so the tone label is not added to it. */
  plainSub?: boolean;
}

export interface DiagramEdge { from: string; to: string; fromPort?: string; toPort?: string; kind?: DiagramEdgeKind }

export interface ViewBox { x: number; y: number; width: number; height: number }

/** Vertical offset of port `index` of `count` on a node card. */
export function portOffset(index: number, count: number, height = NODE_HEIGHT): number {
  return Math.round(height * (index + 1) / (count + 1));
}

function portY(ports: DiagramPort[] | undefined, name: string | undefined): number {
  if (!ports?.length) return NODE_HEIGHT / 2;
  const index = ports.findIndex(p => p.name === name);
  // A wire that names no port of this node ends in the middle of the card, like a wire to a plain port.
  return index < 0 ? NODE_HEIGHT / 2 : portOffset(index, ports.length);
}

export const PORT_HIT_RADIUS = 14;

/** A connection point of a node: one side, and a typed pin when it has one. */
export interface DiagramPortRef { nodeId: string; side: 'in' | 'out'; pin?: string; type?: string }
export interface DiagramPortPoint extends DiagramPortRef { x: number; y: number }

/**
 * Every connection point on the canvas with its position. A node's typed pins replace its
 * plain port; a node with neither has no connection point on that side.
 */
export function diagramPorts(nodes: readonly DiagramNode[]): DiagramPortPoint[] {
  const points: DiagramPortPoint[] = [];
  for (const n of nodes) {
    if (n.inputs?.length) n.inputs.forEach((p, i) => points.push({ nodeId: n.id, side: 'in', pin: p.name, type: p.type, x: n.x, y: n.y + portOffset(i, n.inputs!.length) }));
    else if (n.inPort) points.push({ nodeId: n.id, side: 'in', x: n.x, y: n.y + NODE_HEIGHT / 2 });
    if (n.outputs?.length) n.outputs.forEach((p, i) => points.push({ nodeId: n.id, side: 'out', pin: p.name, type: p.type, x: n.x + NODE_WIDTH, y: n.y + portOffset(i, n.outputs!.length) }));
    else if (n.outPort) points.push({ nodeId: n.id, side: 'out', x: n.x + NODE_WIDTH, y: n.y + NODE_HEIGHT / 2 });
  }
  return points;
}

/** The closest connection point of the given side within the hit radius, if any. */
export function nearestDiagramPort(points: readonly DiagramPortPoint[], x: number, y: number, side: 'in' | 'out', radius = PORT_HIT_RADIUS): DiagramPortPoint | null {
  let best: DiagramPortPoint | null = null;
  let bestDistance = radius;
  for (const p of points) {
    if (p.side !== side) continue;
    const d = Math.hypot(p.x - x, p.y - y);
    if (d <= bestDistance) { best = p; bestDistance = d; }
  }
  return best;
}

/** A node card moved by a pointer drag: the distance in screen pixels scaled to the view box, in whole units. */
export function draggedPosition(origin: { x: number; y: number }, dxPx: number, dyPx: number, view: { width: number; height: number }, box: { width: number; height: number }): { x: number; y: number } {
  return { x: Math.round(origin.x + dxPx * view.width / box.width), y: Math.round(origin.y + dyPx * view.height / box.height) };
}

/** Cubic Bezier connector, the same curve the Flow Editor draws. */
export function connectorPath(x1: number, y1: number, x2: number, y2: number): string {
  const d = Math.max(45, Math.abs(x2 - x1) * 0.45);
  return `M ${x1} ${y1} C ${x1 + d} ${y1}, ${x2 - d} ${y2}, ${x2} ${y2}`;
}

/** Connector between two diagram nodes, honoring named ports. Empty when a node is missing. */
export function edgeBetween(nodes: readonly DiagramNode[], edge: DiagramEdge): string {
  const a = nodes.find(n => n.id === edge.from);
  const b = nodes.find(n => n.id === edge.to);
  if (!a || !b) return '';
  return connectorPath(a.x + NODE_WIDTH, a.y + portY(a.outputs, edge.fromPort), b.x, b.y + portY(b.inputs, edge.toPort));
}

/** View box that fits every node with padding; `aspect` is width/height. */
export function fitViewBox(nodes: readonly { x: number; y: number }[], aspect = 820 / 380, padding = 60, minWidth = 0): ViewBox {
  if (!nodes.length) return { x: 0, y: 0, width: 820, height: 380 };
  const left = Math.min(...nodes.map(n => n.x)) - padding;
  const top = Math.min(...nodes.map(n => n.y)) - padding;
  const right = Math.max(...nodes.map(n => n.x + NODE_WIDTH)) + padding;
  const bottom = Math.max(...nodes.map(n => n.y + NODE_HEIGHT)) + padding;
  const w = right - left;
  const h = bottom - top;
  const width = Math.max(w, h * aspect, minWidth);
  const height = width / aspect;
  return { x: left - (width - w) / 2, y: top - (height - h) / 2, width, height };
}

/** Zooms a view box around its center; factor below 1 zooms in. Width is clamped to [min,max]. */
export function zoomViewBox(view: ViewBox, factor: number, min = 200, max = 3400): ViewBox {
  const width = Math.max(min, Math.min(max, view.width * factor));
  const height = width * view.height / view.width;
  return { x: view.x + (view.width - width) / 2, y: view.y + (view.height - height) / 2, width, height };
}

export function viewBoxAttr(view: ViewBox): string {
  return `${view.x} ${view.y} ${view.width} ${view.height}`;
}

/** Maps a run-step status to the shared state tone. */
export function toneForStatus(status: string | undefined): DiagramTone {
  switch ((status || '').toUpperCase()) {
    case 'SUCCESS': return 'ok';
    case 'FAILED': case 'DEAD': return 'fail';
    case 'RUNNING': return 'run';
    case 'SKIPPED': return 'skip';
    default: return 'none';
  }
}

/** Text that states a tone, so state never depends on color alone. */
export function toneLabel(tone: DiagramTone | undefined): string {
  switch (tone) {
    case 'ok': return 'succeeded';
    case 'fail': return 'failed';
    case 'run': return 'running';
    case 'skip': return 'skipped';
    default: return '';
  }
}

/** Moves the selection to the nearest node in an arrow-key direction; keeps it when none lies that way. */
export function neighborInDirection(nodes: readonly DiagramNode[], fromId: string, dx: number, dy: number): string {
  const from = nodes.find(n => n.id === fromId);
  if (!from) return nodes[0]?.id ?? '';
  let best = fromId;
  let bestScore = Infinity;
  for (const n of nodes) {
    if (n.id === fromId) continue;
    const mx = n.x - from.x;
    const my = n.y - from.y;
    const along = mx * dx + my * dy;
    if (along <= 0) continue;
    const across = Math.abs(mx * dy - my * dx);
    const score = along + across * 2;
    if (score < bestScore) { bestScore = score; best = n.id; }
  }
  return best;
}
