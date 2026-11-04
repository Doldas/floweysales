/**
 * Wording for model profiles, shared by the chat, the team page and the
 * administrator's settings. Pure and free of imports so node --test can run it.
 * What is not known about a model is said to be unknown, never guessed.
 */
export interface ProfileFacts {
  name: string; model: string;
  supports_tools: boolean | null; context_window: number | null;
  cost_input_per_mtok: number | null; cost_output_per_mtok: number | null;
}

export function contextText(tokens: number | null | undefined): string {
  if (tokens === null || tokens === undefined) return 'context size unknown';
  return tokens >= 1_000_000 ? `${+(tokens / 1_000_000).toFixed(1)}M context` : `${Math.round(tokens / 1000)}k context`;
}

export function costText(input: number | null | undefined, output: number | null | undefined): string {
  if ((input === null || input === undefined) && (output === null || output === undefined)) return 'cost unknown';
  const f = (v: number | null | undefined) => (v === null || v === undefined ? '?' : `$${+v.toFixed(2)}`);
  return `${f(input)} in / ${f(output)} out per million tokens`;
}

export function toolsText(supports: boolean | null | undefined): string {
  return supports === null || supports === undefined ? 'tool support unknown' : supports ? 'can call tools' : 'no tools';
}

/** One line a user can compare profiles by. */
export function profileSummary(p: ProfileFacts): string {
  return [p.model, toolsText(p.supports_tools), contextText(p.context_window), costText(p.cost_input_per_mtok, p.cost_output_per_mtok)].join(' · ');
}

/** The option label of a profile: its name, marked when it is the default. */
export function profileOption(p: { name: string; is_default: boolean }): string {
  return p.is_default ? `${p.name} (default)` : p.name;
}

/** What the "no choice" option of a select means right now. */
export function defaultOptionLabel(profiles: { name: string; is_default: boolean }[]): string {
  const d = profiles.find(p => p.is_default);
  return d ? `Default profile (${d.name})` : 'Server default model';
}

/** The model a profile id resolves to, or '' when the id is not (or no longer) offered. */
export function modelOf(profiles: { id: number; model: string; is_default: boolean }[], id: number | null | undefined): string {
  const p = id ? profiles.find(x => x.id === id) : profiles.find(x => x.is_default);
  return p?.model ?? '';
}

/** Ids of profiles a saved team uses that are no longer offered (deleted or disabled). */
export function missingProfiles(team: { members: { profile_id: number | null }[] }, profiles: { id: number }[]): number[] {
  const have = new Set(profiles.map(p => p.id));
  return team.members.map(m => m.profile_id).filter((id): id is number => id !== null && !have.has(id));
}

/** The request members of a team run: each character with its own profile when it has one. */
export function teamMembersBody(ids: string[], profileOf: Record<string, number | null>): { persona: string; profile_id?: number }[] {
  return ids.map(persona => {
    const id = profileOf[persona];
    return id ? { persona, profile_id: id } : { persona };
  });
}

/** What a saved team stores for the characters now chosen. */
export function savedMembers(ids: string[], profileOf: Record<string, number | null>): { persona: string; profile_id: number | null }[] {
  return ids.map(persona => ({ persona, profile_id: profileOf[persona] ?? null }));
}
