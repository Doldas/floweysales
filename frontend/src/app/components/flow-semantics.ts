/**
 * Editor mirror of the server's flow semantics checks (flow_graph_semantics.go):
 * duplicate edges, inputs that can be active at the same time without a merge
 * node, typed pins fed from different branches, and pin type compatibility.
 * Free of Angular so it runs under node --test.
 */

export interface SemNode { id: string; type: string; label?: string; value_type?: string }
export interface SemEdge { from: string; to: string; from_port?: string; to_port?: string }
export interface SemPin { name: string; type: string }
export type SemPinsOf = (node: SemNode) => { inputs: SemPin[]; outputs: SemPin[] };
export interface SemIssue { message: string; nodeId?: string }

type Guard = Record<string, string>;
const conflicts = (a: Guard, b: Guard) => Object.keys(a).some(k => k in b && a[k] !== b[k]);
const sameGuard = (a: Guard, b: Guard) => Object.keys(a).length === Object.keys(b).length && Object.keys(a).every(k => a[k] === b[k]);
const isBranch = (e: SemEdge) => e.from_port === 'true' || e.from_port === 'false';

/** The value type a node feeds along an edge when it is known before running. */
export function sourceType(e: SemEdge, from: SemNode | undefined, pinsOf: SemPinsOf): string {
  if (!from) return '';
  if (e.from_port?.startsWith('out:')) return pinsOf(from).outputs.find(p => p.name === e.from_port!.slice(4))?.type || '';
  if (e.from_port && e.from_port !== 'value') return '';
  if (from.type === 'constant') return from.value_type || '';
  if (from.type === 'condition' && !e.from_port) return 'boolean';
  return '';
}

export function semanticIssues(version: number | undefined, nodes: readonly SemNode[], edges: readonly SemEdge[], pinsOf: SemPinsOf): SemIssue[] {
  const issues: SemIssue[] = [];
  const byId = new Map(nodes.map(n => [n.id, n]));
  const name = (id: string) => byId.get(id)?.label || id;
  const seen = new Set<string>();
  for (const e of edges) {
    const key = [e.from, e.from_port || '', e.to, e.to_port || ''].join('\u0000');
    if (seen.has(key)) issues.push({ message: `Connection ${name(e.from)} → ${name(e.to)} is duplicated.`, nodeId: e.to });
    seen.add(key);
  }
  if (version !== 2) return issues;

  for (const e of edges) {
    if (!e.to_port?.startsWith('in:')) continue;
    const pin = e.to_port.slice(3);
    const target = byId.get(e.to);
    const want = target ? pinsOf(target).inputs.find(p => p.name === pin)?.type || '' : '';
    const got = sourceType(e, byId.get(e.from), pinsOf);
    if (want && want !== 'text' && got && got !== want) issues.push({ message: `Input pin "${pin}" of ${name(e.to)} expects ${want} but ${name(e.from)} provides ${got}.`, nodeId: e.to });
  }

  // Topological order; cycles are reported by the structural checks.
  const incoming = new Map<string, SemEdge[]>(nodes.map(n => [n.id, []]));
  const indegree = new Map(nodes.map(n => [n.id, 0]));
  for (const e of edges) if (byId.has(e.from) && byId.has(e.to)) { incoming.get(e.to)!.push(e); indegree.set(e.to, indegree.get(e.to)! + 1); }
  const queue = nodes.filter(n => !indegree.get(n.id)).map(n => n.id), order: string[] = [];
  while (queue.length) {
    const id = queue.shift()!; order.push(id);
    for (const e of edges) if (e.from === id && indegree.has(e.to)) { indegree.set(e.to, indegree.get(e.to)! - 1); if (!indegree.get(e.to)) queue.push(e.to); }
  }
  if (order.length !== nodes.length) return issues;

  const guards = new Map<string, Guard>();
  for (const id of order) {
    const into = incoming.get(id)!;
    const edgeGuards = into.map(e => ({ ...guards.get(e.from), ...(byId.get(e.from)?.type === 'condition' && isBranch(e) ? { [e.from]: e.from_port! } : {}) }));
    const type = byId.get(id)!.type;
    if (type === 'merge') { /* collects whatever is active */ }
    else if (type === 'function' && into.some(e => e.to_port?.startsWith('in:'))) {
      const pins = new Set<string>();
      into.forEach(e => {
        const pin = e.to_port?.startsWith('in:') ? e.to_port.slice(3) : '';
        if (pin && pins.has(pin)) issues.push({ message: `Input pin "${pin}" of ${name(id)} has more than one source; use a Merge node.`, nodeId: id });
        pins.add(pin);
      });
      if (edgeGuards.some(g => !sameGuard(g, edgeGuards[0]))) issues.push({ message: `Input pins of ${name(id)} are fed from different IF/ELSE branches, so a pin would be missing when only one branch runs.`, nodeId: id });
    } else {
      outer: for (let i = 0; i < into.length; i++) for (let j = i + 1; j < into.length; j++) {
        if (!conflicts(edgeGuards[i], edgeGuards[j])) {
          issues.push({ message: `${name(id)} can receive input from ${name(into[i].from)} and ${name(into[j].from)} at the same time; add a Merge node.`, nodeId: id });
          break outer;
        }
      }
    }
    const guard: Guard = {};
    if (edgeGuards.length) for (const [k, v] of Object.entries(edgeGuards[0])) if (edgeGuards.every(g => g[k] === v)) guard[k] = v;
    guards.set(id, guard);
  }
  return issues;
}
