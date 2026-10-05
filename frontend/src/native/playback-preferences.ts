import type { DTO } from './api';

const aliases: Record<string, string[]> = {
  vi: ['vi', 'vie', 'viet', 'vietnamese', 'tieng viet'],
  en: ['en', 'eng', 'english', 'tieng anh'],
};
function matchesLanguage(value: string, language: string) {
  const normalized = value
    .normalize('NFD')
    .replace(/\p{Diacritic}/gu, '')
    .toLowerCase();
  const words = normalized.replace(/[^a-z0-9]+/g, ' ').trim();
  const base = language.toLowerCase().split(/[-_]/)[0];
  return (aliases[base] ?? [base]).some((alias) => (' ' + words + ' ').includes(' ' + alias + ' '));
}
export function preferredSubtitle(
  tracks: DTO<'TrackDTO'>[],
  uploaded: DTO<'SubtitleDTO'>[],
  language: string,
  canTranscode: boolean,
) {
  // Text overlays avoid re-encoding the video and also work with transcoding disabled.
  const external = uploaded.find((sub) => matchesLanguage(sub.name, language));
  if (external) return 'upload:' + external.id;
  if (!canTranscode) return '-1';
  const subtitles = tracks.filter((track) => track.type === 'subtitle');
  const embedded =
    subtitles.find((track) => matchesLanguage(track.language, language)) ??
    subtitles.find((track) => matchesLanguage(track.title, language));
  return embedded ? String(embedded.index) : '-1';
}

export type AutoQuality = { maxHeight: 0 | 720 | 1080 | 2160; maxBitrate: number };
export function networkDownlink() {
  const connection = (navigator as Navigator & { connection?: { downlink?: number } }).connection;
  const mbps = connection?.downlink;
  return mbps && Number.isFinite(mbps) && mbps > 0 ? mbps * 125000 : null;
}
export function selectAutoQuality(
  file: DTO<'FileDTO'>,
  bytesPerSecond: number | null,
  speed = 1,
): AutoQuality {
  if (
    !bytesPerSecond ||
    !Number.isFinite(bytesPerSecond) ||
    bytesPerSecond <= 0 ||
    !Number.isFinite(speed) ||
    speed <= 0
  )
    return { maxHeight: 0, maxBitrate: 0 };
  // Keep headroom for audio, protocol overhead and short network fluctuations.
  const budget = (bytesPerSecond * 8 * 0.65) / speed;
  const sourceRate = file.duration > 0 && file.size > 0 ? (file.size * 8) / file.duration : 0;
  if (sourceRate > 0 && sourceRate <= budget) return { maxHeight: 0, maxBitrate: 0 };
  const bitrate = Math.min(
    100000000,
    Math.max(100000, Math.floor((budget - 192000) / 500000) * 500000),
  );
  const height = bitrate >= 20000000 ? 2160 : bitrate >= 8000000 ? 1080 : 720;
  const width = height === 720 ? 1280 : height === 1080 ? 1920 : 3840;
  return {
    maxHeight: file.height > 0 && file.height <= height && file.width <= width ? 0 : height,
    maxBitrate: bitrate,
  };
}
export function createAutoQuality(initial: AutoQuality) {
  let current = initial;
  let direction = '';
  let candidate = initial;
  let count = 0;
  let changedAt = -Infinity;
  return {
    sample(target: AutoQuality, now: number): AutoQuality | null {
      if (target.maxHeight === current.maxHeight && target.maxBitrate === current.maxBitrate) {
        direction = '';
        count = 0;
        return null;
      }
      const lower =
        target.maxBitrate > 0 &&
        (current.maxBitrate === 0 || target.maxBitrate < current.maxBitrate);
      const nextDirection = lower ? 'lower' : 'higher';
      if (direction !== nextDirection) {
        count = 0;
        candidate = target;
      }
      // Consecutive measurements may vary. Use their most conservative budget.
      if (
        candidate.maxBitrate === 0 ||
        (target.maxBitrate > 0 && target.maxBitrate < candidate.maxBitrate)
      )
        candidate = target;
      count++;
      direction = nextDirection;
      if (count < (lower ? 2 : 4) || now - changedAt < 30000) return null;
      current = candidate;
      changedAt = now;
      count = 0;
      direction = '';
      return current;
    },
  };
}

// Native video does not expose in-flight download throughput. Measure a bounded
// range through the existing authorized playback URL before loading the source.
export async function measurePlaybackRate(
  url: string,
  signal: AbortSignal,
): Promise<number | null> {
  const abort = new AbortController();
  const cancel = () => abort.abort();
  signal.addEventListener('abort', cancel, { once: true });
  if (signal.aborted) abort.abort();
  const timer = window.setTimeout(cancel, 3000);
  const started = performance.now();
  let bytes = 0;
  try {
    const response = await fetch(url, {
      headers: { Range: 'bytes=0-262143' },
      cache: 'no-store',
      signal: abort.signal,
    });
    if (response.status !== 206 || !response.body) {
      await response.body?.cancel();
      return null;
    }
    const reader = response.body.getReader();
    try {
      while (bytes < 262144) {
        const chunk = await reader.read();
        if (chunk.done) break;
        bytes += chunk.value.byteLength;
      }
    } finally {
      await reader.cancel().catch(() => {});
    }
  } catch {
    // A timeout with partial data is still a useful conservative measurement.
  } finally {
    window.clearTimeout(timer);
    signal.removeEventListener('abort', cancel);
  }
  const elapsed = performance.now() - started;
  return !signal.aborted && bytes > 0 && elapsed > 0 ? (bytes * 1000) / elapsed : null;
}
