// Runs with Node's built-in test runner:
//   node --test src/app/core/workspace-readme.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { findReadme, parentFolder, readmePathFor } from './workspace-readme.ts';

const files = (...paths: string[]) => paths.map((path) => ({ path }));

test('only the README of the folder itself is used', () => {
  const all = files('/README.md', '/a/notes.md', '/a/b/README.md', '/c/README.md');
  assert.equal(findReadme(all, '/'), '/README.md');
  assert.equal(findReadme(all, '/a'), '', 'a parent README is not inherited');
  assert.equal(findReadme(all, '/a/b'), '/a/b/README.md');
  assert.equal(findReadme(all, '/c'), '/c/README.md', 'a sibling README is not used');
});

test('README.md wins over other letter cases, which are matched deterministically', () => {
  assert.equal(findReadme(files('/x/readme.md', '/x/README.md', '/x/Readme.md'), '/x'), '/x/README.md');
  assert.equal(findReadme(files('/x/readme.md', '/x/Readme.md'), '/x'), '/x/Readme.md');
  assert.equal(findReadme(files('/x/README.markdown', '/x/README.md.bak', '/x/README.txt'), '/x'), '');
});

test('a deleted README leaves no overview', () => {
  assert.equal(findReadme(files('/x/a.md'), '/x'), '');
});

test('path helpers', () => {
  assert.equal(parentFolder('/a/b/c.md'), '/a/b');
  assert.equal(parentFolder('/c.md'), '/');
  assert.equal(readmePathFor('/'), '/README.md');
  assert.equal(readmePathFor('/a/b'), '/a/b/README.md');
});
