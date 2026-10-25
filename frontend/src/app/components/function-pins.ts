/**
 * Pure helpers for Blueprint functions with declared typed pins (graph
 * version 2) and for binding those pins in the flow editor. Kept free of
 * Angular dependencies so they can be tested without a DOM or TestBed.
 */

export type PinType = 'text' | 'number' | 'boolean';
export interface Pin { name: string; type: PinType }
export type V2NodeType = 'input' | 'output' | 'constant' | 'operation' | 'branch';
export interface V2Node { id: string; type: V2NodeType; operation?: string; value_type?: PinType; value?: string; x: number; y: number }
export type WireKind = 'data' | 'exec';
export interface V2Edge { kind: WireKind; from: string; from_port: string; to: string; to_port: string }
export interface V2Graph { version: 2; inputs: Pin[]; outputs: Pin[]; nodes: V2Node[]; edges: V2Edge[] }
export interface Port { port: string; kind: WireKind; type?: PinType }

export const MAX_PINS = 8;
const PIN_NAME = /^[a-zA-Z][a-zA-Z0-9_]{0,47}$/;

/** True for graphs with declared typed pins (Blueprint graph version 2). */
export function isTypedFunctionGraph(graph: unknown): graph is V2Graph {
  return !!graph && typeof graph === 'object' && (graph as { version?: number }).version === 2;
}

/** A valid starting graph: one number input wired straight to one number output. */
export function newTypedFunctionGraph(): V2Graph {
  return {
    version: 2,
    inputs: [{ name: 'value', type: 'number' }],
    outputs: [{ name: 'result', type: 'number' }],
    nodes: [{ id: 'start', type: 'input', x: 30, y: 120 }, { id: 'finish', type: 'output', x: 640, y: 120 }],
    edges: [
      { kind: 'exec', from: 'start', from_port: 'next', to: 'finish', to_port: 'in' },
      { kind: 'data', from: 'start', from_port: 'value', to: 'finish', to_port: 'result' },
    ],
  };
}

/** Problems with a declared pin list, worded for the editor. */
export function pinProblems(pins: readonly Pin[], side: 'input' | 'output'): string[] {
  const problems: string[] = [];
  if (pins.length < 1 || pins.length > MAX_PINS) problems.push(`Declare 1–${MAX_PINS} ${side} pins.`);
  const seen = new Set<string>();
  for (const pin of pins) {
    if (!PIN_NAME.test(pin.name)) problems.push(`${side} pin "${pin.name}" must start with a letter and use letters, digits or _.`);
    else if (seen.has(pin.name)) problems.push(`${side} pin "${pin.name}" is declared twice.`);
    seen.add(pin.name);
  }
  return problems;
}

function operationInputs(operation: string | undefined): string[] {
  switch (operation) {
    case 'not': case 'to_text': return ['a'];
    case 'if': return ['condition', 'true', 'false'];
    default: return ['a', 'b'];
  }
}

/** Ports a node offers to outgoing wires. */
export function outputPorts(node: V2Node, graph: V2Graph): Port[] {
  switch (node.type) {
    case 'input': return [...graph.inputs.map(p => ({ port: p.name, kind: 'data' as const, type: p.type })), { port: 'next', kind: 'exec' }];
    case 'constant': return [{ port: 'value', kind: 'data', type: node.value_type }];
    case 'operation': return [{ port: 'value', kind: 'data' }, { port: 'next', kind: 'exec' }];
    case 'branch': return [{ port: 'true', kind: 'exec' }, { port: 'false', kind: 'exec' }];
    default: return [];
  }
}

/** Ports a node accepts incoming wires on. */
export function inputPorts(node: V2Node, graph: V2Graph): Port[] {
  switch (node.type) {
    case 'output': return [...graph.outputs.map(p => ({ port: p.name, kind: 'data' as const, type: p.type })), { port: 'in', kind: 'exec' }];
    case 'operation': return [...operationInputs(node.operation).map(port => ({ port, kind: 'data' as const })), { port: 'in', kind: 'exec' }];
    case 'branch': return [{ port: 'condition', kind: 'data', type: 'boolean' }, { port: 'in', kind: 'exec' }];
    default: return [];
  }
}

/**
 * Adds a wire, replacing any wire already feeding the same input port
 * (each input takes one source). Wires between mismatched kinds or to the
 * node itself are refused by returning the edges unchanged.
 */
export function connectWire(edges: readonly V2Edge[], wire: V2Edge, graph: V2Graph): V2Edge[] {
  const from = graph.nodes.find(n => n.id === wire.from);
  const to = graph.nodes.find(n => n.id === wire.to);
  if (!from || !to || from.id === to.id) return [...edges];
  const out = outputPorts(from, graph).find(p => p.port === wire.from_port);
  const into = inputPorts(to, graph).find(p => p.port === wire.to_port);
  if (!out || !into || out.kind !== into.kind || out.kind !== wire.kind) return [...edges];
  return [...edges.filter(e => !(e.kind === wire.kind && e.to === wire.to && e.to_port === wire.to_port)), wire];
}

/** Renames a declared pin and the wires that use it. */
export function renamePin(graph: V2Graph, side: 'input' | 'output', from: string, to: string): V2Graph {
  const pins = (side === 'input' ? graph.inputs : graph.outputs).map(p => p.name === from ? { ...p, name: to } : p);
  const edges = graph.edges.map(e => {
    if (side === 'input' && e.kind === 'data' && e.from_port === from && graph.nodes.find(n => n.id === e.from)?.type === 'input') return { ...e, from_port: to };
    if (side === 'output' && e.kind === 'data' && e.to_port === from && graph.nodes.find(n => n.id === e.to)?.type === 'output') return { ...e, to_port: to };
    return e;
  });
  return side === 'input' ? { ...graph, inputs: pins, edges } : { ...graph, outputs: pins, edges };
}

/** Removes a declared pin and the wires that use it. */
export function removePin(graph: V2Graph, side: 'input' | 'output', name: string): V2Graph {
  const edges = graph.edges.filter(e => {
    if (side === 'input') return !(e.kind === 'data' && e.from_port === name && graph.nodes.find(n => n.id === e.from)?.type === 'input');
    return !(e.kind === 'data' && e.to_port === name && graph.nodes.find(n => n.id === e.to)?.type === 'output');
  });
  return side === 'input'
    ? { ...graph, inputs: graph.inputs.filter(p => p.name !== name), edges }
    : { ...graph, outputs: graph.outputs.filter(p => p.name !== name), edges };
}

// --- Flow editor: binding a function node's pins ---

/** A flow edge as the flow editor stores it. */
export interface FlowWire { from: string; to: string; from_port?: string; to_port?: string }

/** Declared pins of a function definition; legacy functions have none. */
export function declaredPins(graph: unknown): { inputs: Pin[]; outputs: Pin[] } {
  return isTypedFunctionGraph(graph) ? { inputs: graph.inputs, outputs: graph.outputs } : { inputs: [], outputs: [] };
}

/** Upstream node bound to each input pin of a function node. */
export function pinBindings(edges: readonly FlowWire[], nodeId: string): Record<string, string> {
  const bound: Record<string, string> = {};
  for (const e of edges) {
    if (e.to === nodeId && e.to_port?.startsWith('in:')) bound[e.to_port.slice(3)] = e.from;
  }
  return bound;
}

/**
 * Binds one input pin of a function node to an upstream node, replacing an
 * earlier binding of that pin and any untyped input into the node. An empty
 * source unbinds the pin.
 */
export function bindInputPin(edges: readonly FlowWire[], nodeId: string, pin: string, source: string): FlowWire[] {
  const kept = edges.filter(e => !(e.to === nodeId && (e.to_port === 'in:' + pin || !e.to_port || e.to_port === 'input')));
  return source && source !== nodeId ? [...kept, { from: source, to: nodeId, to_port: 'in:' + pin }] : kept;
}

/** Routes one output pin of a function node to a downstream node. */
export function routeOutputPin(edges: readonly FlowWire[], nodeId: string, pin: string, target: string): FlowWire[] {
  if (!target || target === nodeId) return [...edges];
  const kept = edges.filter(e => !(e.from === nodeId && e.to === target));
  return [...kept, { from: nodeId, from_port: 'out:' + pin, to: target }];
}

/** Vertical offset of pin i of n on a node of the given height. */
export function pinOffset(index: number, count: number, height: number): number {
  return Math.round(height * (index + 1) / (count + 1));
}

/**
 * Checks one flow edge that uses typed pins against the pins the function
 * node declares. Returns a message for the editor's graph validation, or
 * null when the edge is fine.
 */
export function pinEdgeProblem(edge: FlowWire, fromType: string, toType: string, fromPins: Pin[], toPins: Pin[]): string | null {
  if (edge.to_port?.startsWith('in:')) {
    const pin = edge.to_port.slice(3);
    if (toType !== 'function') return 'Input pins exist only on function nodes.';
    if (!toPins.length) return 'This function has no declared input pins; give it typed inputs in the function library.';
    if (!toPins.some(p => p.name === pin)) return `The function declares no input pin "${pin}".`;
  }
  if (edge.from_port?.startsWith('out:')) {
    const pin = edge.from_port.slice(4);
    if (fromType !== 'function') return 'Output pins exist only on function nodes.';
    if (!fromPins.length) return 'This function has no declared output pins; give it typed outputs in the function library.';
    if (!fromPins.some(p => p.name === pin)) return `The function declares no output pin "${pin}".`;
  }
  return null;
}

/** Declared input pins of a function node that no edge binds yet. */
export function unboundInputPins(edges: readonly FlowWire[], nodeId: string, inputs: readonly Pin[]): Pin[] {
  const bound = pinBindings(edges, nodeId);
  return inputs.filter(p => !(p.name in bound));
}

/** True when a node mixes typed input pins with an untyped input. */
export function mixesPinAndPlainInputs(edges: readonly FlowWire[], nodeId: string): boolean {
  const incoming = edges.filter(e => e.to === nodeId);
  return incoming.some(e => e.to_port?.startsWith('in:')) && incoming.some(e => !e.to_port?.startsWith('in:'));
}
