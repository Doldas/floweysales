// Pure helpers that turn an agent answer, with the evidence policy it ran under and what it retrieved,
// into a Markdown or JSON export. Only type imports, so `node --test` can run the spec.
import type { AnswerFinding, AnswerSource, EvidencePolicy } from './answer-sources';

/** Version of the JSON shape below. A change that removes or renames a field raises it. */
export const ANSWER_EXPORT_SCHEMA = 'floweysales.answer-export/v1';

const MODE_LABELS: Record<string, string> = {
  standard: 'Standard',
  evidence_only: 'Evidence only',
  primary_only: 'Primary sources only',
};

const SECRET_PARAM = /key|token|secret|sig|auth|pass|session|credential|code|bearer/i;
const MAX_EXCERPT = 600;

/**
 * The address of a source as it may leave the app: http(s) only, no user name or password, no fragment, and the
 * value of a query parameter that looks like a credential replaced. Anything else becomes ''.
 */
export function redactUrl(raw: string | undefined): string {
  if (!raw) return '';
  let u: URL;
  try { u = new URL(raw.trim()); } catch { return ''; }
  if (u.protocol !== 'http:' && u.protocol !== 'https:') return '';
  u.username = '';
  u.password = '';
  u.hash = '';
  for (const key of [...u.searchParams.keys()]) {
    if (SECRET_PARAM.test(key)) u.searchParams.set(key, 'REDACTED');
  }
  return u.toString();
}

/** One line naming the policy, for the top of an export. */
export function policyLabel(policy: EvidencePolicy | null | undefined): string {
  const mode = policy?.mode ?? 'standard';
  const name = MODE_LABELS[mode] ?? 'Standard';
  if (mode === 'standard') return `${name} (citations are checked after the answer; nothing is held back or removed)`;
  const parts = [name, policy?.on_failure === 'block' ? 'unsupported statements removed' : 'unsupported statements flagged'];
  if (policy?.max_age_days) parts.push(`sources from the last ${policy.max_age_days} days only`);
  if (policy?.allow_classes?.length) parts.push(`counted types: ${policy.allow_classes.join(', ')}`);
  else if (mode === 'primary_only') parts.push('counted types: primary_filing');
  if (policy?.deny_classes?.length) parts.push(`excluded types: ${policy.deny_classes.join(', ')}`);
  return parts.join('; ');
}

export interface ExportSource {
  id: string; tool: string; class: string; access: string;
  title?: string; url?: string; query?: string; document_id?: string; member?: string;
  published_at?: string; retrieved_at?: string; excerpt?: string;
}

export interface AnswerExport {
  schema: typeof ANSWER_EXPORT_SCHEMA;
  /** UTC, RFC 3339. */
  exported_at: string;
  question?: string;
  answer: string;
  policy: EvidencePolicy;
  policy_label: string;
  /** What the citation check did and did not establish. */
  check: { findings: number; removed: number; statement: string };
  sources: ExportSource[];
  findings: AnswerFinding[];
  removed: AnswerFinding[];
}

export interface ExportInput {
  answer: string;
  question?: string;
  policy?: EvidencePolicy | null;
  sources?: readonly AnswerSource[];
  findings?: readonly AnswerFinding[];
  removed?: readonly AnswerFinding[];
  now?: Date;
}

function cut(text: string | undefined): string | undefined {
  if (!text) return undefined;
  return text.length > MAX_EXCERPT ? text.slice(0, MAX_EXCERPT).trimEnd() + '…' : text;
}

function exportSource(s: AnswerSource): ExportSource {
  const out: ExportSource = { id: s.id, tool: s.tool, class: s.class, access: s.access };
  const url = redactUrl(s.url);
  if (s.title) out.title = s.title;
  if (url) out.url = url;
  if (s.query) out.query = s.query;
  if (s.document_id) out.document_id = s.document_id;
  if (s.member) out.member = s.member;
  if (s.published_at) out.published_at = s.published_at;
  if (s.retrieved_at) out.retrieved_at = s.retrieved_at;
  const excerpt = cut(s.excerpt);
  if (excerpt) out.excerpt = excerpt;
  return out;
}

function checkStatement(findings: number, removed: number): string {
  if (findings === 0 && removed === 0) {
    return 'Citation check passed: every cited source was retrieved and its figures appear in the text. This does not confirm the claims are true.';
  }
  const parts: string[] = [];
  if (findings) parts.push(`${findings} unresolved ${findings === 1 ? 'problem' : 'problems'}`);
  if (removed) parts.push(`${removed} ${removed === 1 ? 'statement' : 'statements'} removed`);
  return `Citation check found ${parts.join(' and ')}. Treat the flagged sentences with care.`;
}

/** The export object. The same fields feed the JSON and the Markdown, so they cannot disagree. */
export function buildAnswerExport(input: ExportInput): AnswerExport {
  const findings = [...(input.findings ?? [])];
  const removed = [...(input.removed ?? [])];
  const policy: EvidencePolicy = input.policy ?? { mode: 'standard' };
  const out: AnswerExport = {
    schema: ANSWER_EXPORT_SCHEMA,
    exported_at: (input.now ?? new Date()).toISOString(),
    answer: input.answer,
    policy,
    policy_label: policyLabel(policy),
    check: { findings: findings.length, removed: removed.length, statement: checkStatement(findings.length, removed.length) },
    sources: (input.sources ?? []).map(exportSource),
    findings,
    removed,
  };
  if (input.question) out.question = input.question;
  return out;
}

export function answerToJson(exp: AnswerExport): string {
  return JSON.stringify(exp, null, 2) + '\n';
}

function oneLine(text: string): string {
  return text.replace(/\s+/g, ' ').trim();
}

function quote(text: string): string {
  return text.split('\n').map(l => '> ' + l).join('\n');
}

/** Markdown with the policy first, then the answer, then what supports it. Times are UTC. */
export function answerToMarkdown(exp: AnswerExport): string {
  const lines: string[] = ['# Research answer', ''];
  lines.push(`- **Evidence policy:** ${exp.policy_label}`);
  lines.push(`- **Exported:** ${exp.exported_at.replace('T', ' ').replace(/\.\d+Z$/, 'Z').replace(/Z$/, ' UTC')}`);
  lines.push(`- **Citation check:** ${exp.check.statement}`);
  lines.push('- **Use:** research decision support. It is not a guarantee of correctness and not authorization to trade.');
  if (exp.question) lines.push('', '## Question', '', quote(exp.question));
  lines.push('', '## Answer', '', exp.answer.trim() || '_No answer text._');
  if (exp.removed.length) {
    lines.push('', '## Removed statements', '');
    for (const f of exp.removed) lines.push(`- ${f.kind}: ${oneLine(f.detail)} Removed: “${oneLine(f.sentence)}”`);
  }
  if (exp.findings.length) {
    lines.push('', '## Citation check findings', '');
    for (const f of exp.findings) lines.push(`- ${f.kind}${f.source ? ' (' + f.source + ')' : ''}: ${oneLine(f.detail)} “${oneLine(f.sentence)}”`);
  }
  lines.push('', '## Sources', '');
  if (!exp.sources.length) lines.push('_The agent retrieved no sources._');
  for (const s of exp.sources) {
    const how = s.access === 'read' ? 'opened by the agent' : 'search result only (title and snippet)';
    lines.push(`### [${s.id}] ${oneLine(s.title || s.url || s.id)}`, '');
    lines.push(`- Type: ${s.class}; ${how}; tool: ${s.tool}`);
    if (s.url) lines.push(`- Address: ${s.url}`);
    if (s.query) lines.push(`- Search input: ${oneLine(s.query)}`);
    if (s.document_id) lines.push(`- Document: ${s.document_id}`);
    if (s.member) lines.push(`- Retrieved by team member: ${s.member}`);
    if (s.published_at) lines.push(`- Published: ${s.published_at}`);
    if (s.retrieved_at) lines.push(`- Retrieved (UTC): ${s.retrieved_at}`);
    if (s.excerpt) lines.push('', quote(s.excerpt));
    lines.push('');
  }
  return lines.join('\n').replace(/\n{3,}/g, '\n\n').trimEnd() + '\n';
}
