import type { DTO } from './api';

// Probe the source codecs together, rather than testing H.264 support for every file.
export function canDirectPlay(file: DTO<'FileDTO'>, canPlay: (mime: string) => string) {
  const extension = file.name.split('.').pop()?.toLowerCase();
  const container =
    extension === 'webm' ? 'webm' : ['mp4', 'm4v', 'mov'].includes(extension ?? '') ? 'mp4' : '';
  if (!container) return false;
  const video = file.tracks.find((track) => track.type === 'video');
  const audio = file.tracks.find((track) => track.type === 'audio');
  const videoCodecs: Record<string, string> = {
    h264: 'avc1.640028',
    hevc: 'hvc1.1.6.L120.B0',
    vp8: 'vp8',
    vp9: 'vp09.00.10.08',
    av1: 'av01.0.08M.08',
  };
  const audioCodecs: Record<string, string> = {
    aac: 'mp4a.40.2',
    mp3: 'mp4a.40.34',
    opus: 'opus',
    vorbis: 'vorbis',
    ac3: 'ac-3',
    eac3: 'ec-3',
    flac: 'flac',
  };
  if (!video || !videoCodecs[video.codec] || (audio && !audioCodecs[audio.codec])) return false;
  const codecs = [videoCodecs[video.codec], ...(audio ? [audioCodecs[audio.codec]] : [])];
  return Boolean(canPlay(`video/${container}; codecs="${codecs.join(', ')}"`));
}
