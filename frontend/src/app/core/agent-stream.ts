/** Extract complete SSE frames even when CRLF or UTF-8 text arrives split
 * across arbitrary network chunks. Heartbeat comments are valid empty frames. */
export function takeSseFrames(previous: string, chunk: string): { frames: string[]; remainder: string } {
  let buffer = (previous + chunk).replace(/\r\n/g, '\n');
  const frames: string[] = [];
  let separator: number;
  while ((separator = buffer.indexOf('\n\n')) >= 0) {
    frames.push(buffer.slice(0, separator));
    buffer = buffer.slice(separator + 2);
  }
  return { frames, remainder: buffer };
}

/** Only server-generated, safe integer event cursors may advance replay. */
export function sseSequence(frame: string): number | null {
  const match = /^id:\s*(\d+)\s*$/m.exec(frame);
  if (!match) return null;
  const value = Number(match[1]);
  return Number.isSafeInteger(value) && value > 0 ? value : null;
}
