// Runs with Node's built-in test runner:
//   node --test src/app/ui/flow-canvas/flow-canvas-geometry.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  NODE_HEIGHT, NODE_WIDTH, PORT_HIT_RADIUS, connectorPath, diagramPorts, draggedPosition, edgeBetween, fitViewBox, nearestDiagramPort, neighborInDirection, portOffset, toneForStatus, toneLabel, viewBoxAttr, zoomViewBox,
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

test('a typed pin replaces the plain port, and a node with neither has no connection point on that side', () => {
  const points = diagramPorts([
    node('start', 0, 0, { outPort: 'Start connection from Start' }),
    node('fn', 200, 0, { inPort: 'Connect into F', outPort: 'Start connection from F', inputs: [{ name: 'a', type: 'number' }, { name: 'b', type: 'text' }], outputs: [{ name: 'r', type: 'number' }] }),
    node('out', 400, 0, { inPort: 'Connect into Out' }),
    node('plain', 600, 0),
  ]);
  const of = (id: string, side: 'in' | 'out') => points.filter(p => p.nodeId === id && p.side === side);
  assert.equal(of('start', 'in').length, 0, 'a trigger has no input');
  assert.equal(of('out', 'out').length, 0, 'an output has no output');
  assert.deepEqual(of('fn', 'in').map(p => [p.pin, p.type, p.x, p.y]), [['a', 'number', 200, portOffset(0, 2)], ['b', 'text', 200, portOffset(1, 2)]]);
  assert.deepEqual(of('fn', 'out').map(p => [p.pin, p.x, p.y]), [['r', 200 + NODE_WIDTH, portOffset(0, 1)]]);
  assert.deepEqual(of('start', 'out').map(p => [p.pin, p.x, p.y]), [[undefined, NODE_WIDTH, NODE_HEIGHT / 2]]);
  assert.deepEqual(of('out', 'in').map(p => [p.pin, p.x, p.y]), [[undefined, 400, NODE_HEIGHT / 2]]);
  assert.equal(points.filter(p => p.nodeId === 'plain').length, 0, 'a node that opts in to nothing has no ports');
});

test('the nearest connection point of the wanted side within the hit radius is found', () => {
  const points = diagramPorts([node('a', 0, 0, { outPort: 'a' }), node('b', 300, 0, { inPort: 'b', inputs: [{ name: 'x' }, { name: 'y' }] })]);
  const x = points.find(p => p.pin === 'x')!;
  assert.equal(nearestDiagramPort(points, x.x + 2, x.y + 3, 'in')?.pin, 'x');
  assert.equal(nearestDiagramPort(points, x.x, x.y, 'out'), null, 'the side must match');
  assert.equal(nearestDiagramPort(points, x.x + PORT_HIT_RADIUS + 1, x.y, 'in'), null, 'outside the radius');
  assert.equal(nearestDiagramPort(points, 1000, 1000, 'in'), null);
  // Two pins close to the cursor: the closer one wins.
  const y = points.find(p => p.pin === 'y')!;
  assert.equal(nearestDiagramPort(points, y.x, y.y - 1, 'in')?.pin, 'y');
});

test('a dragged card moves by the distance dragged scaled to the view, in whole units', () => {
  const view = { width: 820, height: 380 };
  assert.deepEqual(draggedPosition({ x: 65, y: 150 }, 100, 50, view, { width: 820, height: 380 }), { x: 165, y: 200 });
  // A canvas drawn at 1126 px wide shows 820 units: 120 px is about 87 units.
  assert.deepEqual(draggedPosition({ x: 65, y: 150 }, 120, 60, view, { width: 1126, height: 522 }), { x: 65 + Math.round(120 * 820 / 1126), y: 150 + Math.round(60 * 380 / 522) });
  // Zoomed in to half the width, the same pixels move half as far.
  assert.equal(draggedPosition({ x: 0, y: 0 }, 100, 0, { width: 410, height: 190 }, { width: 820, height: 380 }).x, 50);
  assert.deepEqual(draggedPosition({ x: 10, y: 10 }, -3.4, 0.4, view, { width: 820, height: 380 }), { x: 7, y: 10 });
});

test('a wire that names no port of its node ends in the middle of the card', () => {
  const a = node('a', 0, 0, { outputs: [{ name: 'r' }, { name: 's' }] });
  const b = node('b', 300, 0, { inputs: [{ name: 'x' }, { name: 'y' }] });
  const named = edgeBetween([a, b], { from: 'a', to: 'b', fromPort: 's', toPort: 'y' });
  const unnamed = edgeBetween([a, b], { from: 'a', to: 'b' });
  const unknown = edgeBetween([a, b], { from: 'a', to: 'b', fromPort: 'nope', toPort: 'nope' });
  assert.equal(unnamed, unknown);
  assert.notEqual(named, unnamed);
  assert.match(unnamed, new RegExp(`^M ${NODE_WIDTH} ${NODE_HEIGHT / 2} C .*, 300 ${NODE_HEIGHT / 2}$`));
});
