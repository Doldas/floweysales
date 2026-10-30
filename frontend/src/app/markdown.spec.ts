// Runs with Node's built-in test runner:
//   node --test src/app/markdown.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { renderMarkdown } from './markdown.ts';

test('identifiers with underscores are not emphasised', () => {
  const html = renderMarkdown('Source: get_sec_facts and calculate_dcf');
  assert.match(html, /get_sec_facts and calculate_dcf/);
  assert.doesNotMatch(html, /<em>/);
});

test('underscore and asterisk emphasis still work at word boundaries', () => {
  assert.match(renderMarkdown('an _important_ point'), /an <em>important<\/em> point/);
  assert.match(renderMarkdown('(__bold__)'), /\(<strong>bold<\/strong>\)/);
  assert.match(renderMarkdown('*star*'), /<em>star<\/em>/);
});

test('model output cannot inject markup', () => {
  const html = renderMarkdown('<script>alert(1)</script> [x](javascript:alert(1))');
  assert.doesNotMatch(html, /<script>/);
  assert.doesNotMatch(html, /href="javascript:/);
});
