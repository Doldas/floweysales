/** Wording for the World view. Pure, tested with node --test. */
import type { CompanyWorld, WorldHeadline, WorldRole, WorldTheme, WorldView } from '../models';

export const ROLE_LABEL: Record<WorldRole, string> = { benefits: 'Tends to gain', hurt: 'Tends to lose', mixed: 'Cuts both ways' };
export const ROLE_ICON: Record<WorldRole, string> = { benefits: '▲', hurt: '▼', mixed: '◆' };

/** "Gain" and "lose" are relative to the driver rising, so say which driver. */
export function roleLine(role: WorldRole, driver: string): string {
  return role === 'mixed' ? `${ROLE_LABEL[role]} when ${driver} change` : `${ROLE_LABEL[role]} when ${driver} rise`;
}

/** Hours since a headline came out, or undefined when it has no timestamp. */
export function ageHours(h: WorldHeadline, nowMs: number): number | undefined {
  return h.published ? Math.max(0, (nowMs - h.published * 1000) / 3_600_000) : undefined;
}

export function ageLabel(h: WorldHeadline, nowMs: number): string {
  const hours = ageHours(h, nowMs);
  if (hours === undefined) return h.age ?? '';
  if (hours < 1) return 'under an hour ago';
  if (hours < 48) return `${Math.floor(hours)} h ago`;
  return `${Math.floor(hours / 24)} days ago`;
}

export function themeById(w: WorldView, id: string): WorldTheme | undefined {
  return w.themes.find(t => t.id === id);
}

/** Theme labels for the ids in a sector row; ids the view does not know are skipped. */
export function themeLabels(w: WorldView, ids: string[] | undefined): string[] {
  return (ids ?? []).map(id => themeById(w, id)?.label).filter((l): l is string => !!l);
}

/**
 * A prompt that hands the agent exactly these headlines and exposures and
 * asks it to stay inside them.
 */
export function worldPrompt(w: WorldView): string {
  const lines = w.themes.slice(0, 6).map(t =>
    `- ${t.label} (${t.count} headlines): ${t.headlines.slice(0, 3).map(h => h.title).join(' | ')}. Exposed: ${t.exposures.filter(x => x.kind !== 'macro').map(x => `${x.name} ${ROLE_LABEL[x.role].toLowerCase()}`).join('; ')}.`);
  return `Summarize what is happening in the world for investors in under 200 words. Use only the headlines and exposures below, name the themes in order of importance, and say which sectors and commodities each touches. Do not add facts or prices of your own. This is research, not advice.\n\nAs of ${w.generated_at}:\n` + lines.join('\n');
}

export function companyPrompt(c: CompanyWorld): string {
  const lines = c.themes.slice(0, 6).map(t => {
    const own = (t.own_headlines ?? []).map(h => h.title).slice(0, 2);
    const world = t.headlines.slice(0, 2).map(h => h.title);
    return `- ${t.label}${t.role ? ` (${ROLE_LABEL[t.role].toLowerCase()} when ${t.driver} rise: ${t.why})` : ''}. ${own.length ? `Company headlines: ${own.join(' | ')}. ` : ''}${world.length ? `World headlines: ${world.join(' | ')}.` : ''}`;
  });
  return `Explain how current world events may affect ${c.symbol}${c.sector ? ` (${c.sector})` : ''} in under 200 words. Use only the items below, separate what the headlines say from what is inference, and do not add numbers of your own. This is research, not advice.\n\nAs of ${c.generated_at}:\n` + lines.join('\n');
}
