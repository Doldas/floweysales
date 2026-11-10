// Node 24+: node --test src/app/agent/agent-chat-blocks.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { ChatEntry } from '../core/agent-session.ts';
import { groupAgentBlocks } from './agent-chat-blocks.ts';

const entry = (id: number, role: ChatEntry['role'], content = '', thinking = '') =>
  ({ id, role, content, thinking } as ChatEntry);

test('consecutive thought and tool steps collapse into one ordered trace', () => {
  const entries = [
    entry(1, 'user', 'Analyze BTC'), entry(2, 'assistant', '', 'Planning'),
    entry(3, 'tool', 'Search result'), entry(4, 'assistant', '', 'Checking filings'),
    entry(5, 'tool', 'Report result'), entry(6, 'assistant', 'Here is the analysis'),
  ];
  const blocks = groupAgentBlocks(entries);
  assert.deepEqual(blocks.map(block => block.kind), ['user', 'trace', 'assistant']);
  assert.deepEqual(blocks[1].kind === 'trace' ? blocks[1].entries.map(item => item.id) : [], [2, 3, 4, 5]);
  assert.equal(blocks[2].kind === 'assistant' ? blocks[2].entry.content : '', 'Here is the analysis');
});

test('assistant answer with reasoning stays visible and is never hidden in trace', () => {
  const blocks = groupAgentBlocks([entry(1, 'tool', ''), entry(2, 'assistant', 'Visible answer', 'Internal reasoning')]);
  assert.equal(blocks[0].kind, 'trace');
  assert.equal(blocks[1].kind, 'assistant');
});

test('new user message separates trace groups', () => {
  const blocks = groupAgentBlocks([entry(1, 'tool', ''), entry(2, 'user', 'Next question'), entry(3, 'tool', '')]);
  assert.deepEqual(blocks.map(block => block.kind), ['trace', 'user', 'trace']);
});

test('new streaming entries do not mutate earlier grouped results', () => {
  const inputs = [entry(1, 'tool', '')];
  const first = groupAgentBlocks(inputs);
  inputs.push(entry(2, 'tool', ''));
  const second = groupAgentBlocks(inputs);
  assert.equal(first[0].kind === 'trace' ? first[0].entries.length : 0, 1);
  assert.equal(second[0].kind === 'trace' ? second[0].entries.length : 0, 2);
});
