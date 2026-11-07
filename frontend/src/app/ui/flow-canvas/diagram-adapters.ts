/**
 * Adapters from each surface's own graph model to the shared diagram model.
 * They are read-only projections: they never change the persisted graph, node
 * ids, ports or coordinates (see docs/flow-diagram-system.md). Free of Angular
 * so they run under `node --test`.
 */
import { toneForStatus, type DiagramEdge, type DiagramNode, type DiagramPort } from '#app/ui/flow-canvas/flow-canvas-geometry';

/** Caption of a Flow Editor node type; the Flow Editor and run snapshots share it. */
export function flowNodeTag(type: string, investorTag: (type: string) => string | undefined = () => undefined): string {
  switch (type) {
    case 'trigger': return '⏱ TRIGGER';
    case 'flow': return '↪ SUBFLOW';
    case 'constant': return '◆ VALUE';
    case 'function': return 'ƒ FUNCTION';
    case 'save_result': return '▣ SAVE';
    case 'load_result': return '▤ LOAD';
    case 'transform': return '⇄ TRANSFORM';
    case 'workspace_read': return '▤ READ FILE';
    case 'workspace_write': return '▣ WRITE FILE';
    case 'output': return '→ OUTPUT';
    case 'condition': return '◇ IF / ELSE';
    case 'merge': return '⇉ MERGE';
    case 'json_get': return '{} JSON FIELD';
    case 'emit_event': return '⇢ EMIT EVENT';
    default: return investorTag(type) || '✦ AGENT';
  }
}

/** Structural copies of the function-pins types, so this module needs no relative imports (it runs under `node --test`). */
export interface PortLike { port: string; kind: string; type?: string }
export interface V2NodeLike { id: string; type: string; operation?: string; value_type?: string; value?: string; x: number; y: number }
export interface V2GraphLike { version: 2; inputs: unknown[]; outputs: unknown[]; nodes: V2NodeLike[]; edges: { kind: 'data' | 'exec'; from: string; from_port: string; to: string; to_port: string }[] }
/** `inputPorts` / `outputPorts` from function-pins.ts. */
export interface PinPorts {
  inputPorts(node: V2NodeLike, graph: V2GraphLike): PortLike[];
  outputPorts(node: V2NodeLike, graph: V2GraphLike): PortLike[];
}

interface FlowLikeNode { id: string; type: string; label: string; x: number; y: number }
interface FlowLikeEdge { from: string; to: string }

/** Run snapshot: the flow's nodes, each toned by its recorded step status. */
export function runSnapshotDiagram(
  nodes: readonly FlowLikeNode[], edges: readonly FlowLikeEdge[], statusOf: (id: string) => string,
  tagOf: (type: string) => string = flowNodeTag,
): { nodes: DiagramNode[]; edges: DiagramEdge[] } {
  return {
    nodes: nodes.map(n => {
      const status = statusOf(n.id);
      return {
        id: n.id, x: n.x, y: n.y, kind: n.type, tag: tagOf(n.type), label: n.label,
        sub: status.toLowerCase(), tone: toneForStatus(status), ariaLabel: `${n.label} ${status}`,
      };
    }),
    edges: edges.map(e => ({ from: e.from, to: e.to })),
  };
}

interface V1Node { id: string; type: 'input' | 'output' | 'constant' | 'operation'; value_type?: string; value?: string; operation?: string; x: number; y: number }
interface V1Edge { from: string; to: string; to_port?: string }

const V1_KIND: Record<V1Node['type'], { kind: string; tag: string }> = {
  input: { kind: 'trigger', tag: '▶ INPUT' },
  output: { kind: 'output', tag: '→ OUTPUT' },
  constant: { kind: 'constant', tag: '◆ VALUE' },
  operation: { kind: 'function', tag: 'ƒ OPERATION' },
};

const EMPTY_V2: V2GraphLike = { version: 2, inputs: [], outputs: [], nodes: [], edges: [] };

/** Function with an untyped (graph version 1) node list. Keeps `to_port` defaults of the runtime ("a"). */
export function functionDiagram(nodes: readonly V1Node[], edges: readonly V1Edge[], ports: PinPorts): { nodes: DiagramNode[]; edges: DiagramEdge[] } {
  return {
    nodes: nodes.map(n => {
      const named = n.type === 'operation'
        ? ports.inputPorts({ id: n.id, type: 'operation', operation: n.operation, x: n.x, y: n.y }, EMPTY_V2).filter(p => p.kind === 'data').map(p => ({ name: p.port }))
        : undefined;
      return {
        id: n.id, x: n.x, y: n.y, ...V1_KIND[n.type], label: n.id,
        sub: n.operation || n.value_type || undefined,
        inputs: named,
      };
    }),
    edges: edges.map(e => ({ from: e.from, to: e.to, toPort: e.to_port || 'a', kind: 'data' as const })),
  };
}

const TYPED_KIND: Record<string, { kind: string; tag: string }> = {
  input: { kind: 'trigger', tag: '▶ INPUT' },
  output: { kind: 'output', tag: '→ OUTPUT' },
  constant: { kind: 'constant', tag: '◆ VALUE' },
  operation: { kind: 'function', tag: 'ƒ OPERATION' },
  branch: { kind: 'condition', tag: '◇ BRANCH' },
};

/** Function with declared typed pins (graph version 2): data and execution wires on named ports. */
export function typedFunctionDiagram(graph: V2GraphLike, ports: PinPorts): { nodes: DiagramNode[]; edges: DiagramEdge[] } {
  const toPort = (p: PortLike): DiagramPort => ({ name: p.port, type: p.type ?? (p.kind === 'exec' ? 'exec' : undefined) });
  return {
    nodes: graph.nodes.map(n => ({
      id: n.id, x: n.x, y: n.y, ...(TYPED_KIND[n.type] ?? TYPED_KIND['operation']), label: n.id,
      sub: n.operation || n.value_type || undefined,
      inputs: ports.inputPorts(n, graph).map(toPort),
      outputs: ports.outputPorts(n, graph).map(toPort),
    })),
    edges: graph.edges.map(e => ({ from: e.from, to: e.to, fromPort: e.from_port, toPort: e.to_port, kind: e.kind })),
  };
}

export interface TriggerLike { id: number; event_key: string; payload_type: string }

/**
 * Custom event triggers: one event node per trigger feeding a typed payload node.
 * Laid out in rows; positions are derived and never persisted.
 */
export function triggerDiagram(triggers: readonly TriggerLike[]): { nodes: DiagramNode[]; edges: DiagramEdge[] } {
  const nodes: DiagramNode[] = [];
  const edges: DiagramEdge[] = [];
  triggers.forEach((t, i) => {
    const y = 20 + i * 100;
    nodes.push({ id: `event:${t.id}`, x: 30, y, kind: 'trigger', tag: '⏱ EVENT', label: t.event_key, sub: 'custom trigger', outputs: [{ name: 'payload', type: t.payload_type }] });
    nodes.push({ id: `payload:${t.id}`, x: 260, y, kind: 'output', tag: '▤ PAYLOAD', label: t.payload_type, sub: 'typed value', inputs: [{ name: 'payload', type: t.payload_type }] });
    edges.push({ from: `event:${t.id}`, to: `payload:${t.id}`, kind: 'event' });
  });
  return { nodes, edges };
}
