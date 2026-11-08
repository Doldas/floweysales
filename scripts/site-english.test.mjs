import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

// Product convention: the published site and its API catalog are US English.
const SWEDISH_LETTERS = /[åäöÅÄÖ]/;

for (const file of ['site/api-catalog.json', 'site/index.html', 'site/api.html', 'site/api.js', 'site/guide.js']) {
  test(`${file} contains no Swedish text`, () => {
    const lines = readFileSync(new URL(`../${file}`, import.meta.url), 'utf8').split('\n');
    const hits = lines.map((l, i) => [i + 1, l]).filter(([, l]) => SWEDISH_LETTERS.test(l)).map(([n, l]) => `${n}: ${l.trim().slice(0, 80)}`);
    assert.deepEqual(hits, []);
  });
}

test('the API catalog is valid JSON with a description for every endpoint', () => {
  const catalog = JSON.parse(readFileSync(new URL('../site/api-catalog.json', import.meta.url), 'utf8'));
  const walk = (x, out = []) => {
    if (Array.isArray(x)) x.forEach(v => walk(v, out));
    else if (x && typeof x === 'object') { if ('path' in x && 'method' in x) out.push(x); Object.values(x).forEach(v => walk(v, out)); }
    return out;
  };
  for (const e of walk(catalog)) assert.ok(typeof e.description === 'string' && e.description.trim(), `${e.method} ${e.path} needs a description`);
});
