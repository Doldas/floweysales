#!/usr/bin/env node
// Read-only audit of the GitHub settings required by foundation issue #1.
// It does not change repository settings and does not bypass CI policy.
import { pathToFileURL } from 'node:url';

const EXPECTED_CHECKS = ['policy', 'frontend', 'go'];

export function auditRuleset(rulesets, requiredChecks = EXPECTED_CHECKS) {
  const errors = [];
  const active = rulesets.filter(r => r.enforcement === 'active' &&
    r.target === 'branch' &&
    r.conditions?.ref_name?.include?.includes('~DEFAULT_BRANCH'));
  if (!active.length) errors.push('No active ruleset targets the default branch.');

  const rules = active.flatMap(r => r.rules || []);
  for (const type of ['pull_request', 'deletion', 'non_fast_forward']) {
    if (!rules.some(rule => rule.type === type)) {
      errors.push('Default branch is missing rule: ' + type);
    }
  }
  const checks = new Set(rules.filter(r => r.type === 'required_status_checks')
    .flatMap(r => r.parameters?.required_status_checks || [])
    .map(c => c.context));
  for (const check of requiredChecks) {
    if (!checks.has(check)) errors.push('Required status check is not enforced: ' + check);
  }
  if (active.some(r => (r.bypass_actors || []).some(actor => actor.bypass_mode === 'always'))) {
    errors.push('At least one actor has an unconditional always-bypass on the default branch.');
  }
  return { ok: errors.length === 0, errors };
}

export function auditPagesDeployment(runs) {
  const pages = runs.filter(r => r.name === 'Documentation Pages')
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime());
  if (!pages.length) return { ok: false, errors: ['No Documentation Pages deployment runs found.'], latest: null };
  const latest = pages[0];
  if (latest.status !== 'completed' || latest.conclusion !== 'success') {
    return { ok: false, errors: ['Latest Documentation Pages run is not successful.'], latest: latest.id };
  }
  return { ok: true, errors: [], latest: latest.id };
}

export async function auditRepository(fetchJSON, repository = 'Doldas/floweysales') {
  const base = 'https://api.github.com/repos/' + repository;
  const [summary, workflowRuns] = await Promise.all([
    fetchJSON(base + '/rulesets'),
    fetchJSON(base + '/actions/runs?per_page=100')
  ]);
  const activeRulesets = summary.filter(r => r.enforcement === 'active' && r.target === 'branch');
  const details = await Promise.all(activeRulesets.map(r => fetchJSON(base + '/rulesets/' + r.id)));
  return { ruleset: auditRuleset(details), pages: auditPagesDeployment(workflowRuns.workflow_runs || []) };
}

async function main() {
  const repo = process.env.GITHUB_REPOSITORY || 'Doldas/floweysales';
  const token = process.env.GITHUB_TOKEN;
  const result = await auditRepository(async url => {
    const response = await fetch(url, {
      headers: {
        Accept: 'application/vnd.github+json',
        'X-GitHub-Api-Version': '2022-11-28',
        ...(token ? { Authorization: 'Bearer ' + token } : {})
      }
    });
    if (!response.ok) throw new Error('GitHub API HTTP ' + response.status + ' for ' + new URL(url).pathname);
    return response.json();
  }, repo);
  console.log('Repository foundation gate audit: ' + repo);
  for (const [key, data] of Object.entries(result)) {
    console.log(key + ': ' + (data.ok ? 'PASS' : 'FAIL'));
    for (const error of data.errors) console.log('  - ' + error);
  }
  console.log('GitHub Projects v2 board: MANUAL VERIFICATION REQUIRED (not assessed by this read-only audit).');
  if (!result.ruleset.ok || !result.pages.ok) process.exitCode = 1;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch(e => { console.error('Audit unavailable: ' + e.message); process.exitCode = 2; });
}
