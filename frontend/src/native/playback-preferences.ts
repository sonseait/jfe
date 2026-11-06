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
  // All subtitle sources are rendered by FFmpeg through NVENC playback.
  if (!canTranscode) return '-1';
  const external = uploaded.find((sub) => matchesLanguage(sub.name, language));
  if (external) return 'upload:' + external.id;
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
): AutoQuality {
  if (!bytesPerSecond || !Number.isFinite(bytesPerSecond) || bytesPerSecond <= 0)
    return { maxHeight: 0, maxBitrate: 0 };
  // Keep headroom for audio, protocol overhead and short network fluctuations.
  const budget = bytesPerSecond * 8 * 0.65;
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

export const SUBTITLE_FONTS = [
  'Arial',
  'Noto Sans',
  'Noto Serif',
  'Noto Sans Mono',
  'Noto Sans CJK',
  'DejaVu Sans',
  'DejaVu Serif',
  'DejaVu Sans Mono',
  'Liberation Sans',
  'Liberation Serif',
  'Liberation Mono',
] as const;
export type FilmPreferences = {
  audio?: string;
  subtitle?: string;
  quality?: string;
  speed?: string;
  volume?: number;
  font?: string;
  tracks?: string;
};
export function filmTrackSignature(file?: DTO<'FileDTO'>) {
  return JSON.stringify(
    file?.tracks.map(({ index, type, codec, language, title }) => ({
      index,
      type,
      codec,
      language,
      title,
    })) ?? [],
  );
}
function filmPreferenceKey(userId: string, fileId: string) {
  return `jfe.film-options.v1:${userId}:${fileId}`;
}
export function readFilmPreferences(
  userId?: string,
  fileId?: string,
  tracks?: string,
): FilmPreferences {
  if (!userId || !fileId) return {};
  try {
    const value = JSON.parse(localStorage.getItem(filmPreferenceKey(userId, fileId)) ?? '{}');
    if (!value || typeof value !== 'object' || Array.isArray(value)) return {};
    return {
      ...(value.tracks === tracks
        ? {
            audio:
              typeof value.audio === 'string' && /^-?\d+$/.test(value.audio)
                ? value.audio
                : undefined,
            subtitle: typeof value.subtitle === 'string' ? value.subtitle : undefined,
          }
        : {}),
      quality: ['auto', '0', '720', '1080', '2160'].includes(value.quality)
        ? value.quality
        : undefined,
      speed: ['0.75', '1', '1.25', '1.5', '2'].includes(value.speed) ? value.speed : undefined,
      volume:
        typeof value.volume === 'number' &&
        Number.isFinite(value.volume) &&
        value.volume >= 0 &&
        value.volume <= 1
          ? value.volume
          : undefined,
      font: SUBTITLE_FONTS.includes(value.font) ? value.font : undefined,
      tracks: value.tracks,
    };
  } catch {
    return {};
  }
}
export function saveFilmPreferences(
  userId: string | undefined,
  fileId: string | undefined,
  tracks: string,
  patch: FilmPreferences,
) {
  if (!userId || !fileId) return;
  try {
    const old = readFilmPreferences(userId, fileId, tracks);
    localStorage.setItem(
      filmPreferenceKey(userId, fileId),
      JSON.stringify({ ...old, ...patch, tracks }),
    );
  } catch {
    /* Playback still works when browser storage is unavailable. */
  }
}
