// Runs with Node's built-in test runner (Node 24+ strips TypeScript types natively):
//   node --test src/app/components/plugin-tools.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  collectInput, fileBytes, fileText, formatBytes, initialValues, normalizeResult, paragraphs, tableToTsv, tabIcon, isTextFile,
  type Tool,
} from './plugin-tools.ts';

const tool: Tool = {
  id: 'k4', label: 'K4', inputs: [
    { name: 'year', label: 'Income year', type: 'number', required: true, default: '2026' },
    { name: 'method', label: 'Method', type: 'select', options: [{ value: 'a', label: 'A' }, { value: 'b', label: 'B' }] },
    { name: 'pnr', label: 'Personal number', type: 'text', required: true },
    { name: 'note', label: 'Note', type: 'textarea' },
    { name: 'flag', label: 'Flag', type: 'checkbox', default: 'true' },
    { name: 'files', label: 'Files', type: 'files', required: true },
    { name: 'one', label: 'One', type: 'file' },
  ],
};

test('defaults come from the manifest', () => {
  const v = initialValues(tool);
  assert.equal(v['year'], '2026');
  assert.equal(v['method'], 'a');
  assert.equal(v['flag'], true);
  assert.deepEqual(v['files'], []);
});

test('collectInput reports what the backend would reject', () => {
  const values = { ...initialValues(tool), year: 'abc', pnr: '  ' };
  const { input, errors } = collectInput(tool, values);
  assert.match(errors['year'], /number/);
  assert.match(errors['pnr'], /required/);
  assert.match(errors['files'], /required/);
  assert.equal('one' in errors, false);
  assert.equal(input['method'], 'a');
  assert.equal(input['flag'], true);
});

test('collectInput builds the request input', () => {
  const values = { ...initialValues(tool), year: '2026,0', pnr: '199001011234', note: '', flag: false,
    files: [{ name: 'a.csv', content: 'x;y', size: 3 }], one: [{ name: 'b.csv', content: 'z', size: 1 }] };
  const { input, errors } = collectInput(tool, values);
  assert.deepEqual(errors, {});
  assert.deepEqual(input, { year: 2026, method: 'a', pnr: '199001011234', files: [{ name: 'a.csv', content: 'x;y' }], one: 'z' });
});

test('select values must be a declared choice', () => {
  const { errors } = collectInput(tool, { ...initialValues(tool), method: 'zzz', pnr: 'x', files: [{ name: 'a', content: 'c', size: 1 }] });
  assert.match(errors['method'], /choices/);
});

test('file blocks decode utf8 and base64 to the intended bytes', () => {
  assert.deepEqual([...fileBytes({ encoding: 'utf8', content: 'åäö' })], [...new TextEncoder().encode('åäö')]);
  // "Åke" in ISO-8859-1 is C5 6B 65.
  const latin1 = Buffer.from([0xc5, 0x6b, 0x65]).toString('base64');
  assert.deepEqual([...fileBytes({ encoding: 'base64', content: latin1 })], [0xc5, 0x6b, 0x65]);
  assert.equal(fileText({ encoding: 'base64', content: latin1 }), 'Åke');
  assert.equal(fileText({ encoding: 'utf8', content: 'plain' }), 'plain');
  assert.equal(isTextFile({ mime: 'text/csv' }), true);
  assert.equal(isTextFile({ mime: 'application/pdf' }), false);
});

test('tables copy as TSV with tabs and newlines flattened', () => {
  const tsv = tableToTsv({ columns: [{ label: 'Name' }, { label: 'Amount', align: 'right' }], rows: [['A\tB', '1\n2'], ['C', '3']] });
  assert.equal(tsv, 'Name\tAmount\nA B\t1 2\nC\t3');
});

test('results are normalised defensively', () => {
  const r = normalizeResult({ title: 'T', blocks: [
    { type: 'notice', level: 'loud' as never, text: 'x' },
    { type: 'table', columns: [{ label: 'a' }, { label: 'b' }], rows: [['1']] },
    { type: 'chart' as never },
    { type: 'stats', stats: [] },
    { type: 'file' },
    { type: 'text', text: 'ok' },
  ] });
  assert.deepEqual(r.blocks.map(b => b.type), ['notice', 'table', 'text']);
  assert.equal(r.blocks[0].level, 'info');
  assert.deepEqual(r.blocks[1].rows, [['1', '']]);
  assert.deepEqual(normalizeResult(null).blocks, []);
});

test('paragraphs, sizes and icons', () => {
  assert.deepEqual(paragraphs('one\ntwo\n\n\nthree\n'), ['one\ntwo', 'three']);
  assert.equal(formatBytes(512), '512 B');
  assert.equal(formatBytes(2048), '2.0 kB');
  assert.equal(formatBytes(5 * 1024 * 1024), '5.0 MB');
  assert.equal(tabIcon({ label: 'x', icon: 'bi-receipt' }), 'bi-receipt');
  assert.equal(tabIcon({ label: 'x', icon: '<script>' }), 'bi-tools');
  assert.equal(tabIcon({ label: 'x' }), 'bi-tools');
});
