import type { DTO } from './api';

// Probe the source codecs together, rather than testing H.264 support for every file.
export function canDirectPlay(file: DTO<'FileDTO'>, canPlay: (mime: string) => string) {
  const extension = file.name.split('.').pop()?.toLowerCase();
  const container =
    file.container ??
    (extension === 'webm' ? 'webm' : ['mp4', 'm4v', 'mov'].includes(extension ?? '') ? 'mp4' : '');
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

// Codec strings must describe the scanned source, especially HEVC Main10.
export function sourceVideoCodec(track: DTO<'TrackDTO'>) {
  const depth = track.bitDepth ?? 8;
  const level = track.level;
  if (track.codec === 'hevc') {
    if (!level || !track.profile) return '';
    if (track.profile === 'Main' && depth === 8) return `hvc1.1.6.L${level}.B0`;
    if (track.profile === 'Main 10' && depth === 10) return `hvc1.2.4.L${level}.B0`;
    return '';
  }
  if (track.codec === 'h264') {
    const profiles: Record<string, string> = {
      'Constrained Baseline': '42e0',
      Baseline: '4200',
      Main: '4d00',
      High: '6400',
    };
    if (depth !== 8 || !level || !profiles[track.profile ?? '']) return '';
    return `avc1.${profiles[track.profile!]}${level.toString(16).padStart(2, '0')}`;
  }
  if (track.codec === 'vp8' && depth === 8) return 'vp8';
  if (track.codec === 'vp9' && depth === 8) return 'vp09.00.10.08';
  return '';
}

const audioCodecStrings: Record<string, string> = {
  aac: 'mp4a.40.2',
  mp3: 'mp4a.40.34',
  opus: 'opus',
  vorbis: 'vorbis',
  ac3: 'ac-3',
  eac3: 'ec-3',
  flac: 'flac',
};
export async function playbackCapabilities(
  file: DTO<'FileDTO'>,
  audioIndex: number,
  canPlay: (mime: string) => string,
  mseSupported: (mime: string) => boolean,
  decodingInfo?: (
    configuration: MediaDecodingConfiguration,
  ) => Promise<MediaCapabilitiesDecodingInfo>,
) {
  const track = file.tracks.find((t) => t.type === 'video');
  const audio = file.tracks.find(
    (t) => t.type === 'audio' && (audioIndex === -1 || t.index === audioIndex),
  );
  const codec = track ? sourceVideoCodec(track) : '';
  const extension = file.name.split('.').pop()?.toLowerCase();
  const container =
    file.container ??
    (extension === 'webm' ? 'webm' : ['mp4', 'mov', 'm4v'].includes(extension ?? '') ? 'mp4' : '');
  const videoMime = `video/mp4; codecs="${codec}"`;
  const sourceCodec =
    track?.codec === 'hevc' && track.codecTag === 'hev1' ? codec.replace('hvc1', 'hev1') : codec;
  const sourceMime = `video/${container || 'mp4'}; codecs="${sourceCodec}"`;
  const audioCodecs = Object.entries(audioCodecStrings)
    .filter(([, c]) =>
      Boolean(canPlay(`audio/${container === 'webm' ? 'webm' : 'mp4'}; codecs="${c}"`)),
    )
    .map(([name]) => name);
  const remuxAudio = Object.entries(audioCodecStrings)
    .filter(([, c]) => mseSupported(`audio/mp4; codecs="${c}"`))
    .map(([name]) => name);
  const outputAudio =
    audio && remuxAudio.includes(audio.codec)
      ? audioCodecStrings[audio.codec]
      : audio
        ? audioCodecStrings.aac
        : '';
  const remuxMime = `video/mp4; codecs="${[codec, outputAudio].filter(Boolean).join(', ')}"`;
  // Native-file and MSE decode support can differ, including HDR support.
  const measure = async (type: MediaDecodingType, contentType: string, initial: boolean) => {
    if (!initial || !track) return false;
    if (track.hdrFormat && !['hdr10', 'hlg'].includes(track.hdrFormat)) return false;
    if (!decodingInfo) return !track.hdrFormat;
    try {
      return (
        await decodingInfo({
          type,
          video: {
            contentType,
            width: track.width ?? file.width,
            height: track.height ?? file.height,
            bitrate: track.bitrate || Math.max(1, (file.size * 8) / Math.max(1, file.duration)),
            framerate: track.frameRate || 24,
            ...(track.hdrFormat
              ? {
                  colorGamut: 'rec2020' as const,
                  transferFunction: track.hdrFormat === 'hlg' ? ('hlg' as const) : ('pq' as const),
                }
              : {}),
          },
        })
      ).supported;
    } catch {
      return false;
    }
  };
  const [nativeSupported, remuxSupported] = await Promise.all([
    measure('file', sourceMime, Boolean(codec && canPlay(sourceMime))),
    measure('media-source', videoMime, Boolean(codec && mseSupported(videoMime))),
  ]);
  const supported = nativeSupported || remuxSupported;
  const pairMime = `video/${container}; codecs="${[sourceCodec, audio ? audioCodecStrings[audio.codec] : ''].filter(Boolean).join(', ')}"`;
  const containers =
    container &&
    nativeSupported &&
    (!audio || audioCodecs.includes(audio.codec)) &&
    canPlay(pairMime)
      ? [container]
      : [];
  return {
    capabilities: {
      containers,
      video:
        supported && track
          ? [
              {
                codec: track.codec,
                profile: track.profile ?? '',
                bitDepth: track.bitDepth ?? 0,
                level: track.level ?? 0,
                maxWidth: track.width ?? file.width,
                maxHeight: track.height ?? file.height,
                hdrFormat: track.hdrFormat ?? '',
              },
            ]
          : [],
      audio: audioCodecs,
      remuxAudio,
      remux: Boolean(remuxSupported && mseSupported(remuxMime)),
    },
    remuxMime,
  };
}
