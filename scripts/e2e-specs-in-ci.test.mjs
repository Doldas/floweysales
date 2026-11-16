// Every Playwright spec in frontend/e2e must run in CI, or be listed in scripts/e2e-not-in-ci.json.
// CI names the specs it runs one step at a time in .github/workflows/ci.yml, so a spec that is not
// added there is never run: it passes on a developer's machine and protects nothing.
//
// scripts/e2e-not-in-ci.json is the list of specs that are not run yet. It is a debt, not a
// permission: a spec leaves the list when CI runs it, a new spec is never put on it without a reason,
// and the list may not name a spec that no longer exists or that CI already runs.
//
// Run: node --test scripts/e2e-specs-in-ci.test.mjs
import test from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

const root = new URL('..', import.meta.url).pathname;

/** The specs of frontend/e2e, as the e2e/<name>.spec.ts paths CI uses. */
export function e2eSpecs() {
  return readdirSync(join(root, 'frontend/e2e')).filter(f => f.endsWith('.spec.ts')).map(f => `e2e/${f}`).sort();
}

/** The e2e specs a workflow names. */
export function e2eSpecsInWorkflow(text) {
  // Not the e2e inside another name, such as live-e2e/, which is another suite.
  return new Set(text.match(/(?<![A-Za-z0-9_-])e2e\/[A-Za-z0-9_.-]+\.spec\.ts/g) ?? []);
}

const named = () => e2eSpecsInWorkflow(readFileSync(join(root, '.github/workflows/ci.yml'), 'utf8'));
const notYet = () => JSON.parse(readFileSync(join(root, 'scripts/e2e-not-in-ci.json'), 'utf8')).specs;

test('the helper finds what a workflow names', () => {
  assert.deepEqual([...e2eSpecsInWorkflow('run: npx playwright test e2e/a.spec.ts e2e/b-c.spec.ts frontend/e2e/d.spec.ts && echo e2e/not-a-spec.ts live-e2e/x.spec.ts')].sort(), ['e2e/a.spec.ts', 'e2e/b-c.spec.ts', 'e2e/d.spec.ts']);
  assert.ok(e2eSpecs().includes('e2e/flow-diagrams.spec.ts'));
});

test('every Playwright spec is run by CI or is on the list of specs not run yet', () => {
  const inCi = named();
  const listed = new Set(notYet());
  assert.ok(e2eSpecs().length > 30, 'the search found too few specs');
  const forgotten = e2eSpecs().filter(s => !inCi.has(s) && !listed.has(s));
  assert.deepEqual(forgotten, [], `add these to a Playwright step of .github/workflows/ci.yml:\n  ${forgotten.join('\n  ')}`);
});

test('the list of specs not run yet is honest: no spec that is gone, none that CI runs, no duplicates', () => {
  const inCi = named();
  const existing = new Set(e2eSpecs());
  const list = notYet();
  assert.equal(new Set(list).size, list.length, 'a spec is listed twice');
  assert.deepEqual(list.filter(s => !existing.has(s)), [], 'remove these from scripts/e2e-not-in-ci.json: the specs no longer exist');
  assert.deepEqual(list.filter(s => inCi.has(s)), [], 'remove these from scripts/e2e-not-in-ci.json: CI runs them now');
  assert.deepEqual(list, [...list].sort(), 'keep the list sorted');
});

test('CI does not name a spec that no longer exists', () => {
  const existing = new Set(e2eSpecs());
  assert.deepEqual([...named()].filter(s => !existing.has(s)), [], 'remove these from .github/workflows/ci.yml');
});
