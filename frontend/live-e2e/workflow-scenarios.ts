// Pure helpers of the live Flow Studio suite. No Playwright, network or
// filesystem access: they are unit-tested with `node --test` (see
// workflow-scenarios.spec.ts) so the safety rules can be checked without a
// deployed instance or credentials.

export type GraphNode = { id?: string; type: string; label?: string; tools?: string[]; flow_id?: number; function_id?: number };
export type GraphEdge = { from: string; to: string };
export type FlowGraph = { nodes?: GraphNode[]; edges?: GraphEdge[] };
export type FlowDef = {
  id: number;
  name: string;
  enabled: boolean;
  interval_minutes: number;
  event_key?: string;
  graph?: FlowGraph;
};
export type RunStep = { node: string; status: string };

export const fixturePrefix = '[E2E] ';
/** Hard ceiling regardless of what an operator asks for (cost bound). */
export const maxWorkflowCeiling = 6;
export const defaultMaxWorkflows = 2;

/**
 * Explicit allowlist: no writes, notifications, HTTP calls, nested flows,
 * plugin executables, schedule triggers or trading.
 */
export const allowedNodeTypes: ReadonlySet<string> = new Set([
  'trigger', 'constant', 'transform', 'json_get', 'condition', 'merge', 'output', 'agent',
]);

export type Scenario = 'chain' | 'branch' | 'merge' | 'agent-output' | 'deterministic';

/** True only for the literal strings "true" or "1". Anything else keeps a gate closed. */
export function optedIn(value: string | undefined): boolean {
  const v = (value ?? '').trim().toLowerCase();
  return v === 'true' || v === '1';
}

/** Bounded fixture count from LIVE_E2E_MAX_WORKFLOWS: 1..maxWorkflowCeiling, default 2. */
export function maxWorkflows(value: string | undefined): number {
  const n = Number.parseInt((value ?? '').trim(), 10);
  if (!Number.isFinite(n) || n < 1) return defaultMaxWorkflows;
  return Math.min(n, maxWorkflowCeiling);
}

/** Reasons why a flow must NOT be executed by the live suite. Empty means safe. */
export function fixtureProblems(flow: FlowDef): string[] {
  const problems: string[] = [];
  if (!flow.name.startsWith(fixturePrefix)) problems.push(`name must start with "${fixturePrefix}"`);
  if (flow.enabled) problems.push('fixture must be disabled (not an automation)');
  if (flow.interval_minutes !== 0) problems.push('fixture must have no schedule');
  if ((flow.event_key ?? '') !== '') problems.push('fixture must have no event trigger');
  const nodes = flow.graph?.nodes ?? [];
  if (nodes.length < 2) problems.push('fixture needs at least two nodes');
  for (const node of nodes) {
    if (!allowedNodeTypes.has(node.type)) problems.push(`node type "${node.type}" is not approved for live E2E`);
    if ((node.tools ?? []).length > 0) problems.push('agent nodes must not have tool grants');
  }
  return problems;
}

/** Fixtures sorted by name then id, restricted to the [E2E] prefix. */
export function pickFixtures(flows: FlowDef[]): FlowDef[] {
  return flows
    .filter(f => f.name.startsWith(fixturePrefix))
    .sort((a, b) => a.name.localeCompare(b.name) || a.id - b.id);
}

/** Classifies the structural scenario angles a graph covers. */
export function classifyScenarios(graph: FlowGraph | undefined): Scenario[] {
  const nodes = graph?.nodes ?? [];
  const edges = graph?.edges ?? [];
  const out = new Map<string, number>();
  for (const e of edges) out.set(e.from, (out.get(e.from) ?? 0) + 1);
  const types = new Set(nodes.map(n => n.type));
  const found: Scenario[] = [];
  const branches = types.has('condition') || [...out.values()].some(c => c > 1);
  if (branches) found.push('branch');
  if (types.has('merge')) found.push('merge');
  if (types.has('agent') && types.has('output')) found.push('agent-output');
  if (!types.has('agent')) found.push('deterministic');
  if (!branches && !types.has('merge')) found.push('chain');
  return found;
}

/**
 * Invariants of a SUCCESS run's step trace. They check structure and state,
 * never exact model wording. Returns human-readable problems (empty = ok).
 */
export function stepProblems(graph: FlowGraph | undefined, steps: RunStep[], scenarios: Scenario[] = classifyScenarios(graph)): string[] {
  const problems: string[] = [];
  const nodes = graph?.nodes ?? [];
  const ids = new Set(nodes.map(n => n.id).filter((x): x is string => Boolean(x)));
  if (steps.length === 0) return ['run recorded no node steps'];
  const executed = new Set<string>();
  for (const s of steps) {
    if (ids.size > 0 && !ids.has(s.node)) problems.push('trace contains a node that is not in the graph');
    if (s.status === 'FAILED' || s.status === 'DEAD') problems.push('a step of a SUCCESS run is failed');
    executed.add(s.node);
  }
  // The engine starts a run at the trigger without recording a step for it, so
  // only the nodes after it are expected in the trace (a recorded one is fine).
  const traced = nodes.filter(n => n.type !== 'trigger' && n.id);
  const outputs = nodes.filter(n => n.type === 'output' && n.id);
  const branching = scenarios.includes('branch');
  // With a branch, an output may legitimately sit on the untaken side.
  if (outputs.length > 0 && !branching && !outputs.some(o => executed.has(o.id as string))) {
    problems.push('no output node executed');
  }
  const ran = traced.filter(n => executed.has(n.id as string)).length;
  if (!branching && traced.length > 0 && ran < traced.length) {
    problems.push(`only ${ran} of ${traced.length} nodes executed in a non-branching graph`);
  }
  return problems;
}

/** Strips values that must never reach CI logs or reports. */
export function redact(text: string): string {
  return text
    .replace(/[\w.+-]+@[\w-]+(?:\.[\w-]+)+/g, '[email]')
    .replace(/\bBearer\s+[\w.~+/=-]+/gi, 'Bearer [token]')
    .replace(/\b(sk|pk|ghp|gho|xox[bap])[-_][\w-]{8,}/g, '[secret]')
    .replace(/\b[0-9a-f]{32,}\b/gi, '[hex]')
    .replace(/(password|passwd|secret|token|api[_-]?key)(["']?\s*[:=]\s*["']?)[^\s"',}]+/gi, '$1$2[redacted]');
}

/** Label safe for logs: id plus scenario tags, never the flow name or contents. */
export function fixtureLabel(flow: FlowDef): string {
  return `fixture ${flow.id} [${classifyScenarios(flow.graph).join(',')}]`;
}
