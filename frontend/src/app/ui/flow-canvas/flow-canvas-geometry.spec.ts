// Runs with Node's built-in test runner:
//   node --test src/app/ui/flow-canvas/flow-canvas-geometry.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  NODE_HEIGHT, NODE_WIDTH, connectorPath, edgeBetween, fitViewBox, neighborInDirection, portOffset, toneForStatus, toneLabel, viewBoxAttr, zoomViewBox,
  type DiagramNode,
} from './flow-canvas-geometry.ts';

const node = (id: string, x: number, y: number, extra: Partial<DiagramNode> = {}): DiagramNode => ({ id, x, y, kind: 'function', tag: 'T', label: id, ...extra });

test('connector uses the Flow Editor curve with a 45 unit minimum handle', () => {
  assert.equal(connectorPath(0, 0, 10, 20), 'M 0 0 C 45 0, -35 20, 10 20');
  assert.equal(connectorPath(0, 0, 1000, 0), 'M 0 0 C 450 0, 550 0, 1000 0');
});

test('edges anchor at card edges and at named ports', () => {
  const a = node('a', 0, 0, { outputs: [{ name: 'x' }, { name: 'y' }] });
  const b = node('b', 300, 100, { inputs: [{ name: 'in' }] });
  const d = edgeBetween([a, b], { from: 'a', to: 'b', fromPort: 'y', toPort: 'in' });
  assert.ok(d.startsWith(`M ${NODE_WIDTH} ${portOffset(1, 2)} `));
  assert.ok(d.endsWith(` 300 ${100 + portOffset(0, 1)}`));
  assert.equal(edgeBetween([a], { from: 'a', to: 'missing' }), '');
  assert.ok(edgeBetween([a, node('c', 5, 5)], { from: 'a', to: 'c' }).endsWith(` 5 ${5 + NODE_HEIGHT / 2}`));
});

test('fit view box contains all nodes and keeps the aspect ratio', () => {
  const v = fitViewBox([{ x: 0, y: 0 }, { x: 1000, y: 40 }]);
  assert.ok(v.x <= -60 && v.x + v.width >= 1000 + NODE_WIDTH + 60);
  assert.ok(Math.abs(v.width / v.height - 820 / 380) < 1e-9);
  assert.deepEqual(fitViewBox([]), { x: 0, y: 0, width: 820, height: 380 });
});

test('zoom keeps the center and clamps', () => {
  const v = zoomViewBox({ x: 0, y: 0, width: 800, height: 400 }, 0.5);
  assert.deepEqual(v, { x: 200, y: 100, width: 400, height: 200 });
  assert.equal(zoomViewBox(v, 0.0001).width, 200);
  assert.equal(zoomViewBox(v, 1000).width, 3400);
  assert.equal(viewBoxAttr(v), '200 100 400 200');
});

test('statuses map to tones and tones are named in text', () => {
  assert.equal(toneForStatus('SUCCESS'), 'ok');
  assert.equal(toneForStatus('dead'), 'fail');
  assert.equal(toneForStatus(undefined), 'none');
  assert.equal(toneLabel('fail'), 'failed');
  assert.equal(toneLabel('none'), '');
});

test('arrow keys pick the nearest node in that direction', () => {
  const nodes = [node('a', 0, 0), node('b', 200, 0), node('c', 200, 150), node('d', 400, 10)];
  assert.equal(neighborInDirection(nodes, 'a', 1, 0), 'b');
  assert.equal(neighborInDirection(nodes, 'b', 1, 0), 'd');
  assert.equal(neighborInDirection(nodes, 'b', 0, 1), 'c');
  assert.equal(neighborInDirection(nodes, 'a', -1, 0), 'a');
});

test("fit never zooms in past the minimum width and keeps few nodes centered", () => {
  const v = fitViewBox([{ x: 100, y: 100 }], 820 / 380, 60, 820);
  assert.equal(v.width, 820);
  assert.ok(Math.abs((v.x + v.width / 2) - (100 + NODE_WIDTH / 2)) < 1e-9);
});
