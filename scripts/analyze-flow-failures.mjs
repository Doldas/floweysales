// Optional, paid, post-test AI triage. Only aggregated, allowlisted counts
// enter the prompt. Never send live flow names, prompts, errors, node outputs,
// credentials, symbols or test-account identities to a model.
import { readFileSync, writeFileSync } from 'node:fs';

const path = 'frontend/live-report/flow-failure-summary.json';
const summary = JSON.parse(readFileSync(path, 'utf8'));
const categories = [
  'permission_or_credentials', 'provider_rate_limit', 'provider_unavailable',
  'missing_market_data', 'invalid_flow', 'output_contract', 'timeout',
  'failed_node', 'unknown',
];
if (summary.schema !== 1 || !categories.every(k => Number.isInteger(summary.categories?.[k]) && summary.categories[k] >= 0)) {
  throw new Error('Invalid sanitized failure summary; refusing AI call');
}
const payload = {
  inspected_flows: Number(summary.inspected_flows),
  inspected_runs: Number(summary.inspected_runs),
  failed_runs: Number(summary.failed_runs),
  categories: Object.fromEntries(categories.map(k => [k, summary.categories[k]])),
};
if (Object.values(payload).slice(0, 3).some(n => !Number.isInteger(n) || n < 0)) throw new Error('Invalid summary counts');
if (!payload.failed_runs) {
  console.log('No recent failed runs: AI triage not needed.');
  process.exit(0);
}
const key = process.env.LIVE_FLOW_AI_KEY;
const model = process.env.LIVE_FLOW_AI_MODEL;
if (!key || !model) throw new Error('Optional AI triage needs protected LIVE_FLOW_AI_KEY and LIVE_FLOW_AI_MODEL');
const request = {
  model,
  temperature: 0,
  max_tokens: 500,
  messages: [
    { role: 'system', content: 'You are a software reliability analyst. Only aggregated failure category counts are available. Give brief hypotheses, a prioritized investigation checklist and what additional evidence is needed. Do not claim root causes are confirmed. Never invent per-flow facts, legal licensing conclusions or personal data.' },
    { role: 'user', content: JSON.stringify(payload) },
  ],
};
const controller = new AbortController();
const timer = setTimeout(() => controller.abort(), 20_000);
let response;
try {
  response = await fetch('https://api.openai.com/v1/chat/completions', {
    method: 'POST',
    headers: { Authorization: `Bearer ${key}`, 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
    signal: controller.signal,
  });
} finally { clearTimeout(timer); }
if (!response.ok()) throw new Error(`Optional AI triage returned HTTP ${response.status}`);
const json = await response.json();
const analysis = String(json.choices?.[0]?.message?.content ?? '').slice(0, 5000);
if (!analysis) throw new Error('Optional AI triage returned no content');
writeFileSync('frontend/live-report/flow-failure-ai-analysis.md',
  '# AI-assisted flow failure triage\n\nHypotheses only; no raw live errors, user data or provider identities were supplied.\n\n' + analysis + '\n', { mode: 0o600 });
console.log('AI triage report generated from sanitized category counts.');
