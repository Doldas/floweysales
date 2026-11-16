/**
 * The rules for what a wire drawn on the Flow Studio canvas may connect. The shared canvas
 * (ui/flow-canvas) finds the two ports and reports the drag; this turns it into the new edge
 * list, or says why not. Free of Angular so it runs under node --test.
 */

export interface WireEdge { from: string; to: string; from_port?: string; to_port?: string }
export interface PortRef { nodeId: string; side: 'in' | 'out'; pin?: string }
export interface PortPoint extends PortRef { x: number; y: number; type?: string }

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
