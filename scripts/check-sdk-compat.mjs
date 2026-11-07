#!/usr/bin/env node
// Validates pluginsdk/compat.json against the SDK source and answers whether
// an SDK version / wire protocol combination is supported.
//   node scripts/check-sdk-compat.mjs                              validate matrix vs source
//   node scripts/check-sdk-compat.mjs --sdk v0.1.0 --protocol 1    check a combination
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const SEMVER = /^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-rc\.([1-9]\d*))?$/;

export function parseVersion(text) {
  const m = SEMVER.exec(String(text));
  if (!m) throw new Error(`invalid SemVer "${text}" (want vMAJOR.MINOR.PATCH[-rc.N])`);
  return { major: +m[1], minor: +m[2], patch: +m[3], rc: m[4] ? +m[4] : null };
}

// Orders versions; a release candidate sorts before its stable version.
export function compareVersions(a, b) {
  const x = parseVersion(a), y = parseVersion(b);
  for (const k of ['major', 'minor', 'patch']) if (x[k] !== y[k]) return x[k] - y[k];
  if (x.rc === y.rc) return 0;
  if (x.rc === null) return 1;
  if (y.rc === null) return -1;
  return x.rc - y.rc;
}

export function checkCombination(matrix, sdk, protocol) {
  const row = matrix.protocols.find((p) => p.protocol === Number(protocol));
  if (!row) return { ok: false, reason: `protocol ${protocol} is not in the compatibility matrix` };
  if (row.status !== 'supported' && row.status !== 'deprecated') {
    return { ok: false, reason: `protocol ${protocol} is ${row.status}` };
  }
  if (compareVersions(sdk, row.sdkMin) < 0) {
    return { ok: false, reason: `SDK ${sdk} is older than ${row.sdkMin}, the minimum for protocol ${protocol}` };
  }
  if (compareVersions(sdk, row.sdkMaxExclusive) >= 0) {
    return { ok: false, reason: `SDK ${sdk} is not below ${row.sdkMaxExclusive}, the limit for protocol ${protocol}` };
  }
  return { ok: true, reason: row.status === 'deprecated' ? `protocol ${protocol} is deprecated` : 'supported' };
}

export function validateMatrix(matrix, protocolConst) {
  const errors = [];
  if (matrix.schema !== 1) errors.push('schema must be 1');
  if (!Array.isArray(matrix.protocols) || matrix.protocols.length === 0) errors.push('protocols must be a non-empty array');
  const seen = new Set();
  for (const p of matrix.protocols ?? []) {
    if (seen.has(p.protocol)) errors.push(`duplicate protocol ${p.protocol}`);
    seen.add(p.protocol);
    if (!['supported', 'deprecated', 'removed'].includes(p.status)) errors.push(`protocol ${p.protocol}: bad status`);
    try {
      if (compareVersions(p.sdkMin, p.sdkMaxExclusive) >= 0) errors.push(`protocol ${p.protocol}: sdkMin must be below sdkMaxExclusive`);
    } catch (e) { errors.push(`protocol ${p.protocol}: ${e.message}`); }
  }
  const current = (matrix.protocols ?? []).find((p) => p.protocol === protocolConst);
  if (!current) errors.push(`protocol.go ProtocolVersion=${protocolConst} is missing from the matrix`);
  else if (current.status !== 'supported') errors.push(`current protocol ${protocolConst} must be "supported"`);
  try { parseVersion(matrix.sdkMinimumForOfficialPlugins); } catch (e) { errors.push(e.message); }
  return errors;
}

export function readProtocolConst(src) {
  const m = /const ProtocolVersion = (\d+)/.exec(src);
  if (!m) throw new Error('ProtocolVersion not found in pluginsdk/protocol.go');
  return Number(m[1]);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const args = process.argv.slice(2);
  const arg = (n) => { const i = args.indexOf(n); return i >= 0 ? args[i + 1] : undefined; };
  const matrix = JSON.parse(readFileSync(join(root, 'pluginsdk/compat.json'), 'utf8'));
  const current = readProtocolConst(readFileSync(join(root, 'pluginsdk/protocol.go'), 'utf8'));
  const sdk = arg('--sdk');
  if (sdk) {
    const r = checkCombination(matrix, sdk, arg('--protocol') ?? current);
    console.log(r.ok ? `compatible: ${r.reason}` : `INCOMPATIBLE: ${r.reason}`);
    process.exit(r.ok ? 0 : 1);
  }
  const errors = validateMatrix(matrix, current);
  if (errors.length) { for (const e of errors) console.error(`::error::${e}`); process.exit(1); }
  console.log('SDK compatibility matrix is consistent with protocol.go.');
}
