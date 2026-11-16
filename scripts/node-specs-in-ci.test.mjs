// Every frontend spec that runs with Node's test runner must be named in the "Unit tests" step of
// .github/workflows/ci.yml. That step lists its files by hand, so a spec that is not added there
// is never run in CI: it passes on a developer's machine and protects nothing.
//
// Run: node --test scripts/node-specs-in-ci.test.mjs
import test from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';

const root = new URL('..', import.meta.url).pathname;

/** Spec files under dir that import node:test; node_modules and build output are skipped. */
export function nodeSpecs(dir) {
  const found = [];
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === 'www' || name.startsWith('.')) continue;
    const path = join(dir, name);
    if (statSync(path).isDirectory()) found.push(...nodeSpecs(path));
    else if (name.endsWith('.spec.ts') && /from ['"]node:test['"]/.test(readFileSync(path, 'utf8'))) found.push(relative(root, path));
  }
  return found.sort();
}

/** The spec paths a workflow file names. */
export function specsInWorkflow(text) {
  return new Set(text.match(/frontend\/[A-Za-z0-9_./-]+\.spec\.ts/g) ?? []);
}

test('the helper finds what a workflow names and what a directory holds', () => {
  assert.deepEqual([...specsInWorkflow('run: node --test frontend/src/a.spec.ts frontend/live-e2e/b.spec.ts && echo x.spec.ts')].sort(),
    ['frontend/live-e2e/b.spec.ts', 'frontend/src/a.spec.ts']);
  assert.ok(nodeSpecs(join(root, 'frontend/src')).includes('frontend/src/app/ui/flow-canvas/graph-contract.spec.ts'));
});

test('every Node spec of the frontend is run by CI', () => {
  const named = specsInWorkflow(readFileSync(join(root, '.github/workflows/ci.yml'), 'utf8'));
  const specs = [...nodeSpecs(join(root, 'frontend/src')), ...nodeSpecs(join(root, 'frontend/live-e2e'))];
  assert.ok(specs.length > 50, `found only ${specs.length} specs; the search is broken`);
  const missing = specs.filter(s => !named.has(s));
  assert.deepEqual(missing, [], `add these to the Node unit test step of .github/workflows/ci.yml:\n  ${missing.join('\n  ')}`);
});

test('CI does not name a spec that no longer exists', () => {
  const named = [...specsInWorkflow(readFileSync(join(root, '.github/workflows/ci.yml'), 'utf8'))];
  const gone = named.filter(s => { try { return !statSync(join(root, s)).isFile(); } catch { return true; } });
  assert.deepEqual(gone, [], 'remove these from .github/workflows/ci.yml');
});
