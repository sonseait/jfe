export function subtitleClock(seconds: number) {
  const ms = Math.round(Math.abs(seconds) * 1000);
  return `${seconds < 0 ? '-' : ''}${String(Math.floor(ms / 3600000)).padStart(2, '0')}:${String(Math.floor(ms / 60000) % 60).padStart(2, '0')}:${String(Math.floor(ms / 1000) % 60).padStart(2, '0')}.${String(ms % 1000).padStart(3, '0')}`;
}
export function activeSubtitleText(
  cues: { start: number; end: number; text: string }[],
  time: number,
  offsetMS: number,
) {
  const sourceTime = time - offsetMS / 1000;
  return cues
    .filter((c) => c.start <= sourceTime && sourceTime < c.end)
    .map((c) => c.text)
    .join('\n');
}
export function minimumSubtitleOffset(cues: { start: number }[]) {
  return cues.length ? -Math.round(Math.min(...cues.map((c) => c.start)) * 1000) : 0;
}
