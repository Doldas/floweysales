/** Which README a Workspace folder shows as its overview. */

export interface ReadmeCandidate {
  path: string;
}

/** Parent folder of a Workspace path (`/` for top-level entries). */
export function parentFolder(path: string): string {
  const parts = path.split('/').filter(Boolean);
  return parts.length <= 1 ? '/' : '/' + parts.slice(0, -1).join('/');
}

/**
 * Picks the README of exactly this folder, never one inherited from a parent or sibling folder.
 * The rule is deterministic: `README.md` wins; otherwise a file named `readme.md` in any letter case, the
 * first in plain code-point order. Returns '' when the folder has no README.
 */
export function findReadme(files: readonly ReadmeCandidate[], folder: string): string {
  const here = files
    .map((f) => f.path)
    .filter((p) => parentFolder(p) === folder && /^readme\.md$/i.test(p.slice(p.lastIndexOf('/') + 1)))
    .sort();
  return here.find((p) => p.endsWith('/README.md')) ?? here[0] ?? '';
}

/** Path for a new README in a folder. */
export function readmePathFor(folder: string): string {
  return (folder === '/' ? '' : folder) + '/README.md';
}
