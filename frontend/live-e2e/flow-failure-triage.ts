/**
 * Privacy-preserving failure classification. No error text, prompts, graph
 * payloads, node inputs or provider names are stored or sent to an AI.
 */
export const failureCategories = [
  'permission_or_credentials', 'provider_rate_limit', 'provider_unavailable',
  'missing_market_data', 'invalid_flow', 'output_contract',
  'timeout', 'failed_node', 'unknown',
] as const;
export type FailureCategory = typeof failureCategories[number];
export type FailureRecord = { flow: number; run: number; status: string; category: FailureCategory };
export type FailureSummary = {
  schema: 1;
  inspected_flows: number;
  inspected_runs: number;
  failed_runs: number;
  categories: Record<FailureCategory, number>;
  failures: FailureRecord[];
};
const safeStatuses = new Set(['FAILED', 'DEAD', 'CANCELLED', 'AWAITING_APPROVAL']);
export function classifyFailure(error: string | undefined, status: string, failedNode?: string): FailureCategory {
  const s = (error ?? '').slice(0, 4096).toLowerCase();
  if (/401|403|unauthori[sz]ed|forbidden|permission|invalid.api.key|missing.credentials|access.denied|license|licence/.test(s)) return 'permission_or_credentials';
  if (/429|rate.limit|quota|too.many.requests|retry.after/.test(s)) return 'provider_rate_limit';
  if (/502|503|504|connection.refused|dns|upstream|provider.unavailable|service.unavailable/.test(s)) return 'provider_unavailable';
  if (/no.data|missing.data|no.price|unavailable.symbol|symbol.not.found|no.history|missing.quarter/.test(s)) return 'missing_market_data';
  if (/invalid.graph|validation|unsupported.node|unconnected|invalid.edge|cycle.detected/.test(s)) return 'invalid_flow';
  if (/invalid.json|json.contract|schema|output.format/.test(s)) return 'output_contract';
  if (/timeout|timed.out|deadline.exceeded|context.deadline/.test(s)) return 'timeout';
  if (failedNode) return 'failed_node';
  return safeStatuses.has(status) ? 'unknown' : 'unknown';
}
export function summarizeFailures(flows: number, runs: number, failures: FailureRecord[]): FailureSummary {
  const categories = Object.fromEntries(failureCategories.map(c => [c, 0])) as Record<FailureCategory, number>;
  for (const failure of failures) categories[failure.category]++;
  return { schema: 1, inspected_flows: flows, inspected_runs: runs, failed_runs: failures.length, categories, failures };
}
export function safeAnalysisInput(s: FailureSummary): Pick<FailureSummary, 'schema' | 'inspected_flows' | 'inspected_runs' | 'failed_runs' | 'categories'> {
  return { schema: 1, inspected_flows: s.inspected_flows, inspected_runs: s.inspected_runs, failed_runs: s.failed_runs, categories: s.categories };
}
