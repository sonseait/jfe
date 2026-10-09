/// <reference types="chromecast-caf-sender" />
import type { DTO } from './api';

let sdk: Promise<cast.framework.CastContext> | undefined;
export function castSupported() {
  return (
    window.isSecureContext &&
    /Chrome|Chromium|CriOS|Edg\//.test(navigator.userAgent) &&
    !/iPhone|iPad|iPod/.test(navigator.userAgent)
  );
}

export function loadCast() {
  if (sdk) return sdk;
  sdk = new Promise((resolve, reject) => {
    const initialize = () => {
      // CAF resolves loadMedia from the Web Sender callback. Explicitly bound
      // that request so an idle receiver cannot hold our load queue indefinitely.
      chrome.cast.media.timeout.load = 60000;
      chrome.cast.media.timeout.getStatus = 10000;
      const context = cast.framework.CastContext.getInstance();
      context.setOptions({
        receiverApplicationId: chrome.cast.media.DEFAULT_MEDIA_RECEIVER_APP_ID,
        autoJoinPolicy: chrome.cast.AutoJoinPolicy.ORIGIN_SCOPED,
        resumeSavedSession: false,
      });
      resolve(context);
    };
    if (window.cast?.framework) return initialize();
    window.__onGCastApiAvailable = (available) => {
      if (available) initialize();
      else reject(new Error('Cast unavailable'));
    };
    const script = document.createElement('script');
    script.src = 'https://www.gstatic.com/cv/js/sender/v1/cast_sender.js?loadCastFramework=1';
    script.async = true;
    script.onerror = () => reject(new Error('Cast unavailable'));
    document.head.appendChild(script);
  });
  return sdk;
}

// A conservative baseline shared by Chromecast generations: SDR H.264, AAC,
// at most 1080p. Never infer receiver support from the sender's browser codecs.
export function castCapabilities(file: DTO<'FileDTO'>): DTO<'PlaybackCapabilitiesDTO'> {
  const video = file.tracks.find((track) => track.type === 'video');
  const supported =
    video?.codec === 'h264' &&
    !video.hdrFormat &&
    (video.bitDepth ?? 8) === 8 &&
    ['Baseline', 'Constrained Baseline', 'Main', 'High'].includes(video.profile ?? '') &&
    (video.level ?? 0) > 0 &&
    (video.level ?? 0) <= 41 &&
    (video.frameRate ?? 0) <= 30 &&
    (video.width ?? file.width) <= 1920 &&
    (video.height ?? file.height) <= 1080;
  return {
    containers: ['mp4'],
    audio: ['aac'],
    remuxAudio: ['aac'],
    remux: true,
    video:
      supported && video
        ? [
            {
              codec: video.codec,
              profile: video.profile ?? '',
              bitDepth: video.bitDepth ?? 8,
              level: video.level ?? 0,
              maxWidth: 1920,
              maxHeight: 1080,
              hdrFormat: '',
            },
          ]
        : [],
  };
}

export function castAudioMIME(file: DTO<'FileDTO'>) {
  const extension = file.name.split('.').pop()?.toLowerCase();
  const codec = file.tracks.find((track) => track.type === 'audio')?.codec;
  if ((file.tracks.find((track) => track.type === 'audio')?.channels ?? 0) > 2) return '';
  if (extension === 'mp3' && codec === 'mp3') return 'audio/mpeg';
  if (['m4a', 'm4b', 'mp4'].includes(extension ?? '') && codec === 'aac') return 'audio/mp4';
  return '';
}

// Current Google Web Sender fields missing from the installed DefinitelyTyped
// declarations. These describe our worker's MPEG-TS HLS segments explicitly.
export function castMediaInfo(session: DTO<'PlaybackDTO'>, url: string, mime: string) {
  const hls = session.protocol === 'hls' || session.method !== 'direct';
  const info = new chrome.cast.media.MediaInfo(
    url,
    hls ? 'application/x-mpegURL' : mime,
  ) as chrome.cast.media.MediaInfo & {
    hlsSegmentFormat?: 'ts';
    hlsVideoSegmentFormat?: 'mpeg2_ts';
  };
  if (hls) {
    info.hlsSegmentFormat = 'ts';
    if (session.method !== 'audio') info.hlsVideoSegmentFormat = 'mpeg2_ts';
  }
  return info;
}

export function castErrorCode(cause: unknown) {
  const code = typeof cause === 'object' && cause !== null && 'code' in cause ? cause.code : cause;
  // SDK errors only: never render arbitrary exception text, URLs or credentials.
  return typeof code === 'string' &&
    Object.values(chrome.cast.ErrorCode).includes(code as chrome.cast.ErrorCode)
    ? code
    : undefined;
}

// CAF getMediaSession() deliberately hides media with idleReason, including
// ERROR and FINISHED. Keep the exact owned media available for error/completion.
export function castSessionMedia(session: cast.framework.CastSession, contentId: string) {
  const media = session.getSessionObj().media;
  for (let index = media.length - 1; index >= 0; index--) {
    if (media[index].media?.contentId === contentId) return media[index];
  }
  return null;
}
