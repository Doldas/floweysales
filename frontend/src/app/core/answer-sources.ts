// Pure helpers for the sources panel under an agent answer. No imports, so `node --test` can run the spec.

export interface AnswerSource {
  id: string; tool: string; url?: string; title?: string; query?: string; class: string; access: 'listed' | 'read' | string;
  document_id?: string; published_at?: string; retrieved_at?: string; excerpt?: string;
}
export interface AnswerFinding { kind: string; sentence: string; source?: string; detail: string }

const FINDING_LABELS: Record<string, string> = {
  fabricated: 'Not a source the agent retrieved',
  snippet_only: 'Search result not opened',
  figure_mismatch: 'Figure not in the cited text',
  uncited: 'Figure without a citation',
};

export function findingLabel(kind: string): string {
  return FINDING_LABELS[kind] ?? kind.replace(/_/g, ' ');
}

/** Sources the agent opened, and search results it only saw as a title and snippet. */
export function splitSources(list: readonly AnswerSource[]): { read: AnswerSource[]; listed: AnswerSource[] } {
  return { read: list.filter(s => s.access === 'read'), listed: list.filter(s => s.access !== 'read') };
}

/** One line for the panel header. A clean audit says what was checked, never that the answer is true. */
export function auditSummary(findings: readonly AnswerFinding[]): string {
  if (!findings.length) return 'Citation check passed: every cited source was retrieved and its figures appear in the text. This does not confirm the claims are true.';
  const n = findings.length;
  return `Citation check found ${n} ${n === 1 ? 'problem' : 'problems'}. Treat the flagged sentences with care.`;
}

/** The link to show for a source, or '' when it is not an http(s) address. */
export function safeUrl(url: string | undefined): string {
  return url && /^https?:\/\//i.test(url) ? url : '';
}

export type EvidenceMode = 'standard' | 'evidence_only' | 'primary_only';
export const EVIDENCE_MODES: readonly { id: EvidenceMode; label: string; hint: string }[] = [
  { id: 'standard', label: 'Standard', hint: 'Answers stream as written. Citations are checked afterward and flagged.' },
  { id: 'evidence_only', label: 'Evidence only', hint: 'The answer is held until its citations pass the check, with up to two corrections. Claims without support are removed or marked insufficient.' },
  { id: 'primary_only', label: 'Primary sources only', hint: 'As Evidence only, and only filings the agent opened count as support. Search results are leads, not proof.' },
];

/** Reads a stored mode, falling back to standard for anything unknown. */
export function parseEvidenceMode(raw: string | null | undefined): EvidenceMode {
  return EVIDENCE_MODES.some(m => m.id === raw) ? (raw as EvidenceMode) : 'standard';
}
