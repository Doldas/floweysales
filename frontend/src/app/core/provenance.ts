// Pure helpers for showing where a value came from. No imports, so `node --test` can run the spec.

export type ProvenanceKind = 'observed' | 'calculated' | 'assumed' | 'estimated' | 'ai_generated';
export type ProvenanceStatus = 'known' | 'partial' | 'unknown';

/** Mirrors internal/provenance.Fact. */
export interface Provenance {
  kind: ProvenanceKind;
  status: ProvenanceStatus;
  provider?: string;
  official?: boolean;
  url?: string;
  document_id?: string;
  published_at?: string;
  as_of?: string;
  retrieved_at?: string;
  method?: { id: string; version?: string; formula?: string };
  inputs?: string[];
  run_id?: string;
  note?: string;
}

export interface ProvenanceLine { label: string; value: string }

const KIND_LABEL: Record<ProvenanceKind, string> = {
  observed: 'Reported by a source',
  calculated: 'Calculated',
  assumed: 'Assumed',
  estimated: 'Estimated',
  ai_generated: 'Written by an AI model, not a source',
};

/** The short word on the trigger: how much of the origin is known. */
export function provenanceBadge(f: Provenance | null | undefined): string {
  if (!f || f.status === 'unknown') return 'Source unknown';
  if (f.kind !== 'observed') return KIND_LABEL[f.kind].split(',')[0];
  return f.status === 'partial' ? 'Source (partial)' : 'Source';
}

/**
 * The facts to list in the popover, in reading order. Unknown is stated plainly and nothing is
 * invented: a missing field is simply absent. `format` renders a UTC timestamp.
 */
export function provenanceLines(f: Provenance | null | undefined, format: (iso: string) => string): ProvenanceLine[] {
  if (!f || f.status === 'unknown') {
    return [{ label: 'Source', value: f?.note || 'No source was recorded for this value.' }];
  }
  const lines: ProvenanceLine[] = [{ label: 'What this is', value: KIND_LABEL[f.kind] }];
  if (f.provider) lines.push({ label: 'Provider', value: f.official === undefined ? f.provider : `${f.provider} (${f.official ? 'official' : 'unofficial'})` });
  if (f.document_id) lines.push({ label: 'Document', value: f.document_id });
  if (f.published_at) lines.push({ label: 'Published', value: format(f.published_at) });
  if (f.as_of) lines.push({ label: 'As of', value: format(f.as_of) });
  if (f.retrieved_at) lines.push({ label: 'Retrieved', value: format(f.retrieved_at) });
  if (f.method) lines.push({ label: 'How calculated', value: [f.method.id + (f.method.version ? ` v${f.method.version}` : ''), f.method.formula].filter(Boolean).join(': ') });
  if (f.inputs?.length) lines.push({ label: 'Built from', value: f.inputs.join(', ') });
  if (f.note) lines.push({ label: 'Note', value: f.note });
  return lines;
}

/** A link is only offered for http(s) addresses. */
export function safeSourceUrl(f: Provenance | null | undefined): string {
  const url = f?.url ?? '';
  return /^https?:\/\//i.test(url) ? url : '';
}
