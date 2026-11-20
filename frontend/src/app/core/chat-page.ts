/** The page a chat message is sent from, as the server names it: the first route segment, "home" for the root. */
export function chatPage(url: string): string {
  const path = url.split(/[?#]/)[0];
  return path.split('/').filter(Boolean)[0] ?? 'home';
}
