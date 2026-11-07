/**
 * View model for the document evidence of a read_source result: which pages
 * of a PDF were read as text, which by the vision model, what is verified and
 * what is not. Pure and free of imports so node --test can run it.
 */
export interface EvidencePage { page: number; class: string; method: string; vision: string; note: string; label: string }
export interface EvidenceObservation {
  page: number; label: string; original: string; unit: string; period: string; verified: boolean; flags: string[]; flagText: string[];
}
export interface DocumentEvidence {
  url: string; resolvedFrom: string; pagesTotal: number; pagesExamined: number;
  pages: EvidencePage[]; observations: EvidenceObservation[];
  stages: { retrieved: boolean; textParsed: boolean; visionInterpreted: boolean; verifiedValues: number; unverifiedValues: number };
  limitations: string[]; visionStatus: string; summary: string;
}

const FLAGS: Record<string, string> = {
  value_not_in_excerpt: 'The printed value is not in the quoted source text',
  no_source_excerpt: 'No source text was quoted',
  period_missing: 'No period is stated',
  ambiguous_separator: 'The number format is ambiguous (thousands or decimal separator)',
  not_a_number: 'The value is not a plain number',
  no_value: 'No value',
  annual_figure_labelled_quarter: 'An annual figure was labelled as a quarter',
  quarter_labelled_annual: 'A quarter was labelled as annual',
  quarter_not_stated: 'Marked as a quarter, but no quarter is printed',
  model_uncertain: 'The model was unsure of this value',
  total_mismatch: 'The components do not add up to the total',
  total_consistent: 'The components add up to the total',
  bbox_dropped: 'The reported region was invalid and dropped',
};

export function flagText(flag: string): string { return FLAGS[flag] ?? flag.replace(/_/g, ' '); }

export function methodLabel(method: string): string {
  return ({ text: 'Text layer', vision: 'Vision model', 'text+vision': 'Text layer and vision model', none: 'Not read' } as Record<string, string>)[method] ?? method;
}

export function visionPageLabel(v: string): string {
  return ({ ok: 'interpreted', cached: 'interpreted (cached)', failed: 'failed', skipped: 'skipped', unavailable: 'no vision model' } as Record<string, string>)[v] ?? '';
}

const isObj = (v: unknown): v is Record<string, any> => !!v && typeof v === 'object' && !Array.isArray(v);

/** Returns null when the result is not a PDF read (HTML pages, errors, cut JSON). */
export function parseDocumentEvidence(result: string | undefined): DocumentEvidence | null {
  if (!result) return null;
  let doc: unknown;
  try { doc = JSON.parse(result); } catch { return null; }
  if (!isObj(doc) || !isObj(doc['document']) || doc['document']['type'] !== 'pdf') return null;
  const d = doc['document'];
  const pages: EvidencePage[] = (Array.isArray(d['pages']) ? d['pages'] : []).filter(isObj).map(p => ({
    page: Number(p['page']) || 0, class: String(p['class'] ?? ''), method: String(p['method'] ?? 'none'),
    vision: String(p['vision'] ?? ''), note: String(p['note'] ?? ''), label: methodLabel(String(p['method'] ?? 'none')),
  }));
  const observations: EvidenceObservation[] = [];
  for (const ev of Array.isArray(doc['visual_evidence']) ? doc['visual_evidence'] : []) {
    for (const o of Array.isArray(ev?.observations) ? ev.observations : []) {
      const flags: string[] = Array.isArray(o.flags) ? o.flags.map(String) : [];
      observations.push({
        page: Number(o.page) || Number(ev.page) || 0, label: String(o.label ?? ''), original: String(o.original_value ?? ''),
        unit: [o.unit, o.currency].filter(Boolean).join(' '), period: String(o.period ?? ''), verified: o.verified === true,
        flags, flagText: flags.filter(f => f !== 'total_consistent').map(flagText),
      });
    }
  }
  const e = isObj(d['evidence']) ? d['evidence'] : {};
  const stages = {
    retrieved: e['retrieved'] !== false, textParsed: e['text_parsed'] === true, visionInterpreted: e['vision_interpreted'] === true,
    verifiedValues: Number(e['verified_values']) || 0, unverifiedValues: Number(e['unverified_values']) || 0,
  };
  const out: DocumentEvidence = {
    url: String(doc['document_url'] ?? doc['url'] ?? ''), resolvedFrom: String(doc['resolved_from'] ?? ''),
    pagesTotal: Number(d['pages_total']) || 0, pagesExamined: Number(d['pages_examined']) || pages.length,
    pages, observations, stages, visionStatus: String(d['vision_status'] ?? ''),
    limitations: Array.isArray(d['limitations']) ? d['limitations'].map(String) : [], summary: '',
  };
  out.summary = evidenceSummary(out);
  return out;
}

/** One sentence that keeps retrieval, text parsing, vision reading and verification apart. */
export function evidenceSummary(d: DocumentEvidence): string {
  const parts = ['PDF retrieved'];
  parts.push(d.stages.textParsed ? 'text parsed' : 'no text layer');
  parts.push(d.stages.visionInterpreted ? 'pages interpreted by the vision model' : 'no vision interpretation');
  const v = d.stages.verifiedValues, u = d.stages.unverifiedValues;
  if (v + u > 0) parts.push(`${v} of ${v + u} vision-read value${v + u === 1 ? '' : 's'} passed the automatic checks (none confirmed independently)`);
  return parts.join(' · ');
}
