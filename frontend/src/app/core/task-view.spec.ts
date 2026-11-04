// Runs with Node's built-in test runner:
//   node --test src/app/core/task-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { MAX_DELIVERABLES, canResume, checklistLine, parseChecklist, parseDeliverables, taskStatusLabel } from './task-view.ts';

test('required parts are typed one per line; blanks, dashes and repeats are dropped', () => {
  assert.deepEqual(parseDeliverables('Latest price\n\n- DCF\n* dcf\n  Risk note  \n'), ['Latest price', 'DCF', 'Risk note']);
  assert.deepEqual(parseDeliverables('   \n\n'), []);
});

test('the list is capped as the server caps it', () => {
  const many = Array.from({ length: 20 }, (_, i) => `Part ${i}`).join('\n');
  assert.equal(parseDeliverables(many).length, MAX_DELIVERABLES);
  assert.equal(parseDeliverables('x'.repeat(500))[0].length, 200);
});

test('a checklist that was never reported is not shown as done', () => {
  assert.equal(checklistLine([]), '');
  assert.equal(
    checklistLine([{ title: 'a', status: 'done' }, { title: 'b', status: 'missing', note: 'no data' }, { title: 'c', status: 'pending' }]),
    '1 of 3 done · 1 reported missing · 1 not yet reported',
  );
  assert.equal(checklistLine([{ title: 'a', status: 'done' }]), '1 of 1 done');
});

test('the checklist of a checkpoint event is read defensively', () => {
  assert.deepEqual(parseChecklist('[{"title":"DCF","status":"done"}]'), [{ title: 'DCF', status: 'done' }]);
  assert.deepEqual(parseChecklist('not json'), []);
  assert.deepEqual(parseChecklist('{"title":"x"}'), []);
  assert.deepEqual(parseChecklist(undefined), []);
});

test('any task that is not complete can be resumed', () => {
  assert.equal(canResume({ status: 'incomplete', resumable: true }), true);
  assert.equal(canResume({ status: 'failed', resumable: true }), true);
  assert.equal(canResume({ status: 'cancelled', resumable: true }), true);
  assert.equal(canResume({ status: 'complete', resumable: false }), false);
  assert.equal(canResume({ status: 'running', resumable: true }), true);
  assert.equal(taskStatusLabel('incomplete'), 'Incomplete: parts are missing');
  assert.equal(taskStatusLabel('something_new'), 'something_new');
});
