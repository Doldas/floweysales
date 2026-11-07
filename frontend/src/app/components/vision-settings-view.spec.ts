// Runs with Node's built-in test runner:
//   node --test src/app/components/vision-settings-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { probeAdvice, statusText, utcTime, visionRequestBody } from './vision-settings-view.ts';

const limits = { max_pages_per_document: 6, max_images_per_run: 20, max_tokens_per_run: 200000, timeout_seconds: 90, concurrency: 2 };

test('a blank key is not sent, so the saved key is kept', () => {
  const body = visionRequestBody({ enabled: true, baseURL: ' https://v.example/v1 ', model: ' m ', fallback: '', supportsImages: true, apiKey: '  ', limits });
  assert.equal('api_key' in body, false);
  assert.equal(body['base_url'], 'https://v.example/v1');
  assert.equal(body['model'], 'm');
  assert.equal(visionRequestBody({ enabled: false, baseURL: '', model: '', fallback: '', supportsImages: false, apiKey: ' k ', limits })['api_key'], 'k');
});

test('every status says what happens to scanned pages', () => {
  assert.match(statusText('not_configured'), /reported as unread/);
  assert.match(statusText('needs_test'), /image test/);
  assert.match(statusText('no_image_support_declared'), /not marked as accepting images/);
  assert.match(statusText('ready'), /Ready/);
});

test('failed tests give advice instead of provider detail', () => {
  assert.match(probeAdvice('image_rejected', 'x'), /vision-capable/);
  assert.match(probeAdvice('wrong_answer', 'x'), /text-only/);
  assert.equal(probeAdvice('other', 'plain message'), 'plain message');
});

test('times are shown in UTC', () => {
  assert.equal(utcTime('2026-11-06T14:05:33+01:00'), '2026-11-06 13:05 UTC');
  assert.equal(utcTime('garbage'), '');
});
