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
  /** Overrides the generated accessible name. */
  ariaLabel?: string;
}

export interface DiagramEdge { from: string; to: string; fromPort?: string; toPort?: string; kind?: DiagramEdgeKind }

export interface ViewBox { x: number; y: number; width: number; height: number }

/** Vertical offset of port `index` of `count` on a node card. */
export function portOffset(index: number, count: number, height = NODE_HEIGHT): number {
  return Math.round(height * (index + 1) / (count + 1));
}

function portY(ports: DiagramPort[] | undefined, name: string | undefined): number {
  if (!ports?.length) return NODE_HEIGHT / 2;
  const index = Math.max(0, ports.findIndex(p => p.name === name));
  return portOffset(index, ports.length);
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
