export function bufferedRanges(ranges: TimeRanges, offset: number, duration: number) {
  const result: [number, number][] = [];
  if (!Number.isFinite(duration) || duration <= 0) return result;
  for (let i = 0; i < ranges.length; i++) {
    const start = Math.max(0, ranges.start(i) + offset);
    const end = Math.min(duration, ranges.end(i) + offset);
    if (end > start) result.push([(start / duration) * 100, (end / duration) * 100]);
  }
  return result;
}

export function containsTime(ranges: TimeRanges, time: number) {
  if (!Number.isFinite(time) || time < 0) return false;
  for (let i = 0; i < ranges.length; i++) {
    if (time >= ranges.start(i) && time < ranges.end(i)) return true;
  }
  return false;
}

export function hasSegment(
  fragments: readonly { start: number; duration: number; gap?: boolean }[],
  time: number,
) {
  return (
    Number.isFinite(time) &&
    time >= 0 &&
    fragments.some(
      (fragment) =>
        !fragment.gap && time >= fragment.start && time < fragment.start + fragment.duration,
    )
  );
}

export function transferRate(bytes: number, start: number, end: number): number | null {
  if (bytes <= 0 || end <= start || !Number.isFinite(bytes + start + end)) return null;
  return (bytes * 1000) / (end - start);
}

export function formatTransferRate(bytesPerSecond: number, locale: string) {
  const units = ['B/s', 'KiB/s', 'MiB/s', 'GiB/s'];
  const unit = Math.min(3, Math.floor(Math.log(Math.max(1, bytesPerSecond)) / Math.log(1024)));
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: unit ? 1 : 0 }).format(bytesPerSecond / 1024 ** unit)} ${units[unit]}`;
}
