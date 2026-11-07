// Pure helpers for the sources panel under an agent answer. No imports, so `node --test` can run the spec.

export interface AnswerSource {
  id: string; tool: string; url?: string; title?: string; query?: string; class: string; access: 'listed' | 'read' | string;
  document_id?: string; published_at?: string; retrieved_at?: string; excerpt?: string;
  /** The team member who retrieved it, when a delegated team did. */
  member?: string;
  /** The web search provider that listed it, and "grounded_source" for a page Gemini cited. */
  search_provider?: string; result_type?: string;
  /** Source type, evidence role, dates kept apart, period, evidence state and why it ranked where it did. */
  trust?: SourceTrust;
}
export interface SourceTrust {
  source_type: string; evidence_role: string; type_basis?: string;
  published_at?: string; modified_at?: string; effective_at?: string; retrieved_at?: string;
  file_created_at?: string; file_modified_at?: string; http_last_modified?: string;
  date_source?: string; date_confidence?: string; date_notes?: string[];
  period?: string; period_source?: string;
  evidence_state?: string; access_note?: string;
  rank?: number; why_ranked?: string[]; also_seen_at?: string[];
}
export interface AnswerFinding { kind: string; sentence: string; source?: string; detail: string }

/** The exact passage of a source a verdict rests on: Match is the matched token, Before and After its surroundings. */
export interface ClaimSpan {
  source: string; start: number; end: number; before?: string; match: string; after?: string;
  url?: string; title?: string; document_id?: string; published_at?: string; retrieved_at?: string;
}
export interface ClaimFigure {
  text: string; metric?: string; period?: string; unit?: string; direction?: string;
  verdict: string; code: string; detail?: string; span?: ClaimSpan;
}
export type ClaimVerdict = 'supported' | 'contradicted' | 'insufficient' | 'not_checked';
/** The claim-level evidence record of one sentence of an answer. */
export interface AnswerClaim {
  id: string; sentence: string; kind: string; causal?: boolean; assertion?: string;
  entity?: string; metric?: string; period?: string; unit?: string; direction?: string; qualifiers?: string[]; citations?: string[];
  verdict: ClaimVerdict | string; code: string; explanation: string;
  figures?: ClaimFigure[]; spans?: ClaimSpan[]; calculation?: string; conflicts?: { value: string; span: ClaimSpan }[];
  limitations: string; injection_suspected?: boolean; verifier_version: string; verified_at: string;
}
export interface ClaimStats {
  claims: number; supported: number; contradicted: number; insufficient: number; not_checked: number;
  skipped?: number; elapsed_ms: number; model_calls: number; verifier_version: string;
}
const FINDING_LABELS: Record<string, string> = {
  fabricated: 'Not a source the agent retrieved',
  snippet_only: 'Search result not opened',
  figure_mismatch: 'Figure not in the cited text',
  uncited: 'Figure without a citation',
  quote_mismatch: 'Quote not in the cited text',
  not_primary: 'Not a primary filing',
  excluded_source: 'Source type not accepted',
  stale: 'Source too old or undated',
  unreachable: 'Link no longer resolves',
  claim_contradicted: 'Claim contradicted by the cited text',
  claim_insufficient: 'Claim not proven by the cited text',
  unsupported_claim: 'Judgement not labeled as such',
  summary_figure_not_in_members: 'Figure in no member answer',
  summary_misattributed: 'Figure attributed to the wrong member',
  sentiment_as_fact: 'Financial figure rests on discussion or opinion',
};

export function findingLabel(kind: string): string {
  return FINDING_LABELS[kind] ?? kind.replace(/_/g, ' ');
}

/** Sources the agent opened, and search results it only saw as a title and snippet. */
export function splitSources(list: readonly AnswerSource[]): { read: AnswerSource[]; listed: AnswerSource[] } {
  return { read: list.filter(s => s.access === 'read'), listed: list.filter(s => s.access !== 'read') };
}

/** Who retrieved a source, for the line under its title: a delegated team's member is named. */
export function sourceOrigin(s: Pick<AnswerSource, 'member'> & Partial<Pick<AnswerSource, 'search_provider' | 'result_type' | 'access'>>): string {
  // A source the agent opened is no longer "only a snippet".
  const found = s.access === 'read' ? '' : searchOrigin(s.search_provider, s.result_type);
  const by = s.member ? `retrieved by team member ${s.member}` : '';
  return [by, found].filter(Boolean).join(' · ');
}

const SEARCH_NAMES: Record<string, string> = {
  searxng: 'SearXNG', brave: 'Brave Search', brave_search: 'Brave Search', tavily: 'Tavily',
  google_grounding: 'Google Search (Gemini grounding)', google_cse: 'Google Custom Search',
};

/** Which search provider found a listed source. A grounded source was cited by Gemini and was not opened. */
export function searchOrigin(provider: string | undefined, resultType: string | undefined): string {
  if (!provider) return '';
  const name = SEARCH_NAMES[provider] ?? provider.replace(/_search$/, '').replace(/_/g, ' ');
  return resultType === 'grounded_source' ? `cited by Gemini through ${name}; not opened` : `found by ${name}; only a snippet`;
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
  { id: 'evidence_only', label: 'Evidence only', hint: 'The answer is held until its citations pass the check, with up to two corrections. What still fails is flagged, or removed if you choose that below.' },
  { id: 'primary_only', label: 'Primary sources only', hint: 'As Evidence only, and only filings the agent opened count as support. Search results are leads, not proof.' },
];

/** Reads a stored mode, falling back to standard for anything unknown. */
export function parseEvidenceMode(raw: string | null | undefined): EvidenceMode {
  return EVIDENCE_MODES.some(m => m.id === raw) ? (raw as EvidenceMode) : 'standard';
}

export type RankingProfile = 'facts_first' | 'balanced' | 'sentiment_first';
export const RANKING_PROFILES: readonly { id: RankingProfile; label: string; hint: string }[] = [
  { id: 'facts_first', label: 'Facts first', hint: 'Official and primary sources rank first. Community posts appear when the question asks for reaction.' },
  { id: 'balanced', label: 'Balanced', hint: 'Community discussion counts more, but filings still win for reported figures.' },
  { id: 'sentiment_first', label: 'Sentiment first', hint: 'Recent discussion ranks higher. It stays labeled as sentiment and never counts as a reported figure.' },
];
export function parseRankingProfile(raw: string | null | undefined): RankingProfile {
  return RANKING_PROFILES.some(p => p.id === raw) ? (raw as RankingProfile) : 'facts_first';
}

export type SourceClass = 'primary_filing' | 'web_page';
/** The mode and limits the server takes as `evidence` (see docs/agent-evidence.md). */
export interface EvidencePolicy {
  mode: EvidenceMode; on_failure?: 'flag' | 'block'; max_age_days?: number; allow_classes?: SourceClass[]; deny_classes?: SourceClass[];
  ranking_profile?: RankingProfile; search_window_days?: number;
}
/** What the settings form edits. */
export interface EvidenceSettings {
  mode: EvidenceMode; block: boolean; maxAgeDays: number; filings: boolean; webPages: boolean;
  /** Order of web search results and the freshness window of "latest" searches (0 = none). */
  rankingProfile: RankingProfile; searchWindowDays: number;
}

export const MAX_EVIDENCE_AGE_DAYS = 3650;

/** The source types a mode counts when no list is set. */
export function defaultTypes(mode: EvidenceMode): { filings: boolean; webPages: boolean } {
  return { filings: true, webPages: mode !== 'primary_only' };
}

export function settingsFromPolicy(p: EvidencePolicy | null | undefined): EvidenceSettings {
  const mode = parseEvidenceMode(p?.mode);
  const base = p?.allow_classes?.length
    ? { filings: p.allow_classes.includes('primary_filing'), webPages: p.allow_classes.includes('web_page') }
    : defaultTypes(mode);
  const deny = p?.deny_classes ?? [];
  const maxAge = Number(p?.max_age_days);
  const win = Number(p?.search_window_days);
  return {
    rankingProfile: parseRankingProfile(p?.ranking_profile),
    searchWindowDays: Number.isInteger(win) && win > 0 && win <= MAX_EVIDENCE_AGE_DAYS ? win : 0,
    mode,
    block: p?.on_failure === 'block' && mode !== 'standard',
    maxAgeDays: Number.isInteger(maxAge) && maxAge > 0 && maxAge <= MAX_EVIDENCE_AGE_DAYS ? maxAge : 0,
    filings: base.filings && !deny.includes('primary_filing'),
    webPages: base.webPages && !deny.includes('web_page'),
  };
}

/** The request object for the settings; defaults are left out so the server applies its own. */
export function policyFromSettings(s: EvidenceSettings): EvidencePolicy {
  const out: EvidencePolicy = { mode: s.mode };
  if (s.rankingProfile !== 'facts_first') out.ranking_profile = s.rankingProfile;
  if (s.searchWindowDays > 0) out.search_window_days = s.searchWindowDays;
  if (s.mode === 'standard') return out;
  if (s.block) out.on_failure = 'block';
  if (s.maxAgeDays > 0) out.max_age_days = s.maxAgeDays;
  const def = defaultTypes(s.mode);
  if (s.filings !== def.filings || s.webPages !== def.webPages) {
    out.allow_classes = [...(s.filings ? ['primary_filing' as const] : []), ...(s.webPages ? ['web_page' as const] : [])];
  }
  return out;
}

/**
 * Changes one thing in the evidence policy of a flow agent node and keeps the rest
 * (including settings the form does not show, such as a deny list). Off, or standard,
 * is no policy at all.
 */
export function patchNodeEvidence(
  current: EvidencePolicy | undefined,
  patch: { mode?: string; block?: boolean; maxAgeDays?: number },
): EvidencePolicy | undefined {
  const mode = patch.mode ?? current?.mode ?? '';
  if (mode !== 'evidence_only' && mode !== 'primary_only') return undefined;
  const next: EvidencePolicy = { ...(current ?? { mode }), mode };
  // Each mode counts its own source types, so a new mode starts from those.
  if (patch.mode !== undefined && patch.mode !== current?.mode) delete next.allow_classes;
  if (patch.block !== undefined) { if (patch.block) next.on_failure = 'block'; else delete next.on_failure; }
  if (patch.maxAgeDays !== undefined) {
    if (Number.isInteger(patch.maxAgeDays) && patch.maxAgeDays > 0 && patch.maxAgeDays <= MAX_EVIDENCE_AGE_DAYS) next.max_age_days = patch.maxAgeDays;
    else delete next.max_age_days;
  }
  return next;
}

/** Why the settings cannot be used, or '' when they can. */
export function settingsProblem(s: EvidenceSettings): string {
  if (!Number.isInteger(s.searchWindowDays) || s.searchWindowDays < 0 || s.searchWindowDays > MAX_EVIDENCE_AGE_DAYS) return `The search window must be a whole number of days from 0 to ${MAX_EVIDENCE_AGE_DAYS}.`;
  if (s.mode === 'standard') return '';
  if (!Number.isInteger(s.maxAgeDays) || s.maxAgeDays < 0 || s.maxAgeDays > MAX_EVIDENCE_AGE_DAYS) return `The age limit must be a whole number of days from 0 to ${MAX_EVIDENCE_AGE_DAYS}.`;
  if (!s.filings && !s.webPages) return 'Choose at least one source type to count.';
  return '';
}

/** One line saying what the settings do, for the composer. */
export function settingsSummary(s: EvidenceSettings): string {
  if (s.mode === 'standard') return '';
  const parts = [s.block ? 'Unsupported statements are removed.' : 'Unsupported statements are flagged.'];
  if (s.maxAgeDays > 0) parts.push(`Only sources from the last ${s.maxAgeDays} days count; a source with no date does not.`);
  const def = defaultTypes(s.mode);
  if (s.filings !== def.filings || s.webPages !== def.webPages) {
    parts.push(`Counted source types: ${[s.filings ? 'filings' : '', s.webPages ? 'web pages' : ''].filter(Boolean).join(' and ') || 'none'}.`);
  }
  return parts.join(' ');
}

/** The line under an answer that had statements removed. */
export function removedSummary(removed: readonly AnswerFinding[]): string {
  const n = removed.length;
  return n ? `${n} ${n === 1 ? 'statement was' : 'statements were'} removed from the answer because the citation check could not support ${n === 1 ? 'it' : 'them'}.` : '';
}

/** One status line for a team member's progress while the run waits on a delegated team. */
export function delegationProgress(ev: { type?: string; name?: string; agent?: string; stance?: string; error?: string }): string {
  const who = ev.name || ev.agent || 'A member';
  if (ev.type === 'member_start') return `${who} is working`;
  if (ev.type !== 'member_done') return '';
  if (ev.error) return `${who} failed: ${ev.error}`;
  return ev.stance ? `${who} answered (${ev.stance.replace(/_/g, ' ')})` : `${who} answered`;
}

const VERDICT_LABELS: Record<string, string> = {
  supported: 'Supported by the cited text',
  contradicted: 'Contradicted by the cited text',
  insufficient: 'Not proven (insufficient evidence)',
  not_checked: 'Not checked',
};

/** The heading of a claim's verdict. Only "supported" is proof; the others are never shown as confirmation. */
export function verdictLabel(verdict: string): string {
  return VERDICT_LABELS[verdict] ?? verdict.replace(/_/g, ' ');
}

/** The question a claim's explanation answers: why it is supported, or why it is not. */
export function whyLabel(verdict: string): string {
  return verdict === 'supported' ? 'Why is this claim supported?' : 'Why is this claim not supported?';
}

/** Contradicted claims first, then unproven, not checked and supported, keeping answer order within a group. */
export function sortClaims(claims: readonly AnswerClaim[]): AnswerClaim[] {
  const rank: Record<string, number> = { contradicted: 0, insufficient: 1, not_checked: 2, supported: 3 };
  return claims.map((c, i) => ({ c, i })).sort((a, b) => (rank[a.c.verdict] ?? 4) - (rank[b.c.verdict] ?? 4) || a.i - b.i).map(x => x.c);
}

/** One line for the claims header; says how many claims were proven and what the check cost. */
export function claimsSummary(claims: readonly AnswerClaim[], stats?: ClaimStats | null): string {
  if (!claims.length) return '';
  const count = (v: string) => claims.filter(c => c.verdict === v).length;
  const parts = [`${count('supported')} supported`, `${count('contradicted')} contradicted`, `${count('insufficient')} not proven`, `${count('not_checked')} not checked`];
  let text = `${claims.length} ${claims.length === 1 ? 'claim' : 'claims'}: ${parts.join(', ')}.`;
  if (stats) {
    text += ` Checked by fixed rules in ${stats.elapsed_ms} ms with no model call`;
    text += stats.skipped ? `; ${stats.skipped} more ${stats.skipped === 1 ? 'sentence was' : 'sentences were'} not checked (limit reached).` : '.';
  }
  return text;
}

/** What the sentence asserts, as the verifier read it: the facts it compared with the source. */
export function claimFacts(c: AnswerClaim): { label: string; value: string }[] {
  const rows: [string, string | undefined][] = [
    ['Entity', c.entity], ['Metric', c.metric], ['Period', c.period], ['Unit', c.unit], ['Direction', c.direction],
    ['Qualifiers', c.qualifiers?.length ? c.qualifiers.join(', ') : undefined], ['Kind', c.kind + (c.causal ? ' (states a cause)' : '')],
  ];
  return rows.filter((r): r is [string, string] => !!r[1]).map(([label, value]) => ({ label, value }));
}

/** The passage of a source with the matched token separated, for highlighting without HTML. */
export function spanParts(s: ClaimSpan): { before: string; match: string; after: string } {
  return { before: s.before ?? '', match: s.match, after: s.after ?? '' };
}

/** Where a span came from, for the line above it: source id, title or document, and times in UTC. */
export function spanOrigin(s: ClaimSpan): string {
  const parts = [`[${s.source}]`, s.title || s.document_id || s.url || ''];
  if (s.published_at) parts.push(`published ${s.published_at.slice(0, 10)}`);
  if (s.retrieved_at) parts.push(`retrieved ${s.retrieved_at.replace('T', ' ').replace(/(:\d\d)?Z$/, '')} UTC`);
  return parts.filter(Boolean).join(' · ');
}

const TYPE_LABELS: Record<string, string> = {
  official_filing: 'Official filing', exchange_announcement: 'Exchange announcement', government_data: 'Government data',
  peer_reviewed: 'Peer-reviewed study', company_ir: 'Company investor relations', first_party_dataset: 'Data provider record',
  established_news: 'Established news', professional_analysis: 'Professional analysis', community_social: 'Community / social',
  marketing: 'Marketing', unknown: 'Unknown type',
};
const ROLE_LABELS: Record<string, string> = {
  primary: 'Primary evidence', self_reported: 'Self-reported (what the party stated)', secondary: 'Secondary reporting',
  opinion: 'Opinion', sentiment: 'Sentiment (what people say)', unknown: 'Role unknown',
};
const STATE_LABELS: Record<string, string> = {
  snippet_only: 'Search snippet only', page_read: 'Page read', pdf_text: 'PDF text read',
  vision_derived: 'Includes vision-derived content', access_denied: 'Access denied, not read', read_failed: 'Could not be read',
};
const DATE_SOURCE_LABELS: Record<string, string> = {
  search_provider: 'search provider', json_ld: 'page structured data', meta_tag: 'page meta tag', time_element: 'page time element',
  filing_index: 'filing index', pdf_cover: 'report cover', unknown: 'unknown',
};

export function sourceTypeLabel(t: string | undefined): string { return TYPE_LABELS[t ?? ''] ?? 'Unknown type'; }
export function evidenceRoleLabel(r: string | undefined): string { return ROLE_LABELS[r ?? ''] ?? 'Role unknown'; }
export function evidenceStateLabel(s: string | undefined, access?: string): string {
  return STATE_LABELS[s ?? ''] ?? (access === 'read' ? 'Page read' : 'Search snippet only');
}

function day(v: string): string {
  const m = /^(\d{4}-\d{2}-\d{2})/.exec(v);
  return m ? m[1] : v;
}

/** The dates of a source, each labeled, with times in UTC. A missing date is shown as unknown, never as today. */
export function sourceDates(t: SourceTrust | undefined): { label: string; value: string }[] {
  if (!t) return [{ label: 'Published', value: 'unknown' }];
  const src = DATE_SOURCE_LABELS[t.date_source ?? ''] ?? t.date_source;
  const rows: { label: string; value: string }[] = [
    { label: 'Published', value: t.published_at ? `${day(t.published_at)}${src && t.date_source !== 'unknown' ? ` (${src}, ${t.date_confidence ?? 'unknown'} confidence)` : ''}` : 'unknown' },
  ];
  if (t.modified_at) rows.push({ label: 'Updated', value: day(t.modified_at) });
  if (t.effective_at) rows.push({ label: 'Data as of', value: day(t.effective_at) });
  if (t.period) rows.push({ label: 'Report period', value: t.period });
  if (t.file_created_at) rows.push({ label: 'File created', value: day(t.file_created_at) });
  if (t.file_modified_at) rows.push({ label: 'File modified', value: day(t.file_modified_at) });
  if (t.http_last_modified) rows.push({ label: 'HTTP Last-Modified', value: t.http_last_modified });
  if (t.retrieved_at) rows.push({ label: 'Retrieved', value: `${t.retrieved_at.replace('T', ' ').replace(/(:\d\d)?(Z|[+-]00:00)$/, '')} UTC` });
  return rows;
}

/** Everything a source card shows. File dates are only hints and are labeled so. */
export function sourceCard(s: AnswerSource): {
  type: string; role: string; state: string; denied: boolean; dates: { label: string; value: string }[]; notes: string[]; why: string[]; copies: string[];
} {
  const t = s.trust;
  const notes = [...(t?.date_notes ?? [])];
  if (t && !t.published_at && (t.file_created_at || t.file_modified_at || t.http_last_modified)) notes.push('File and server dates describe the file, not when the report was published.');
  if (t?.access_note) notes.push(t.access_note);
  return {
    type: sourceTypeLabel(t?.source_type), role: evidenceRoleLabel(t?.evidence_role), state: evidenceStateLabel(t?.evidence_state, s.access),
    denied: t?.evidence_state === 'access_denied', dates: sourceDates(t), notes, why: t?.why_ranked ?? [], copies: t?.also_seen_at ?? [],
  };
}

/** Sort by the order the ranking gave, or by publication date with undated sources last. */
export function sortSources<T extends AnswerSource>(list: readonly T[], by: 'relevance' | 'date'): T[] {
  const rank = (s: T) => s.trust?.rank ?? Number.MAX_SAFE_INTEGER;
  const time = (s: T) => { const v = Date.parse(s.trust?.published_at ?? s.published_at ?? ''); return Number.isNaN(v) ? null : v; };
  return list.map((s, i) => ({ s, i })).sort((a, b) => {
    if (by === 'date') {
      const ta = time(a.s), tb = time(b.s);
      if (ta !== tb) return ta === null ? 1 : tb === null ? -1 : tb - ta;
    } else if (rank(a.s) !== rank(b.s)) return rank(a.s) - rank(b.s);
    return a.i - b.i;
  }).map(x => x.s);
}
