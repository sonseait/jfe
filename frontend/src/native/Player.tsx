import { usePersonalSubtitleTiming } from './personal-subtitle-timing';
import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { Alert, Button, Group, Loader, Menu, ScrollArea, Slider } from '@mantine/core';
import {
  Check,
  ChevronDown,
  Maximize,
  Minimize2,
  Pause,
  Play,
  SkipForward,
  Volume2,
  X,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { create } from 'zustand';
import toast from 'react-hot-toast';
import type Hls from 'hls.js';
import type { LoaderStats } from 'hls.js';
import {
  measurePlaybackRate,
  preferredSubtitle,
  selectAutoQuality,
  networkDownlink,
  createAutoQuality,
} from './playback-preferences';
import { playbackCapabilities } from './playback-support';
import { playRemux, RemuxDecodeError } from './remux-player';
import { createAdaptiveBuffer, INITIAL_BUFFER_CONFIG } from './playback-buffer';
import {
  bufferedRanges,
  transferRate,
  formatTransferRate,
  containsTime,
  hasSegment,
} from './playback-metrics';
import { useDebouncedValue } from '@mantine/hooks';
import { SubtitleTiming, SubtitleUpload, UploadedSubtitleDelete } from './Subtitles';
import { StreamIndicator } from './StreamIndicator';
import { queryClient } from '../lib/query-client';
import { api, result, useAuth, useResource, type DTO } from './api';
function clockTime(seconds: number) {
  const value = Math.max(0, Math.floor(seconds));
  return `${Math.floor(value / 60)}:${String(value % 60).padStart(2, '0')}`;
}
function PlaybackOption({
  label,
  value,
  data,
  onChange,
  disabled,
  title,
  portalTarget,
  footer,
}: {
  label: string;
  value: string;
  data: { value: string; label: string }[];
  onChange: (value: string) => void;
  disabled?: boolean;
  title?: string;
  portalTarget?: HTMLElement;
  footer?: ReactNode;
}) {
  const selected = data.find((option) => option.value === value)?.label ?? label;
  return (
    <Menu
      keepMounted
      portalProps={{ target: portalTarget }}
      position="top-start"
      width={240}
      shadow="lg"
    >
      <Menu.Target>
        <button
          type="button"
          className="playback-option"
          aria-label={`${label}: ${selected}`}
          disabled={disabled}
          title={title ?? `${label}: ${selected}`}
        >
          <span className="playback-option-label">{label}</span>
          <span className="playback-option-value">{selected}</span>
          <ChevronDown size={12} aria-hidden="true" />
        </button>
      </Menu.Target>
      <Menu.Dropdown className="playback-option-menu">
        <Menu.Label>{label}</Menu.Label>
        <ScrollArea.Autosize mah="min(280px, 45dvh)" type="auto" scrollbars="y">
          {data.map((option) => (
            <Menu.RadioItem
              key={option.value}
              value={option.value}
              checked={option.value === value}
              checkIcon={<Check size={14} />}
              closeMenuOnClick
              onChange={() => {
                if (option.value !== value) onChange(option.value);
              }}
            >
              {option.label}
            </Menu.RadioItem>
          ))}
        </ScrollArea.Autosize>
        {footer && (
          <>
            <Menu.Divider />
            {footer}
          </>
        )}
      </Menu.Dropdown>
    </Menu>
  );
}
interface PlaybackState {
  detail?: DTO<'DetailDTO'>;
  fileId?: string;
  generation: number;
  expanded: boolean;
  play: (detail: DTO<'DetailDTO'>, fileId: string) => void;
  stop: () => void;
  expand: () => void;
}
export const useNativePlayer = create<PlaybackState>((set) => ({
  generation: 0,
  expanded: true,
  play: (detail, fileId) =>
    set((s) => ({ detail, fileId, generation: s.generation + 1, expanded: true })),
  stop: () => set({ detail: undefined, fileId: undefined }),
  expand: () => set((s) => ({ expanded: !s.expanded })),
}));
export default function Player() {
  const generation = useNativePlayer((s) => s.generation);
  return <Surface key={generation} />;
}
function Surface() {
  const { t, i18n } = useTranslation();
  const state = useNativePlayer();
  const auth = useAuth((s) => s.token);
  const system = useResource('system', async (signal) =>
    result(await api.GET('/api/v1/system', { signal })),
  );
  const canTranscode = system.data?.capabilities.transcoding ?? false;
  const video = useRef<HTMLVideoElement>(null);
  const frame = useRef<HTMLCanvasElement>(null);
  const wrapper = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState(state.detail?.item.position ?? 0);
  const positionRef = useRef(position);
  const scrubbing = useRef(false);
  const [playing, setPlaying] = useState(false);
  const [error, setError] = useState(false);
  const [errorKey, setErrorKey] = useState('playbackError');
  const [fallback, setFallback] = useState(0);
  const [loading, setLoading] = useState(false);
  const [downloadRate, setDownloadRate] = useState<number | null>(null);
  const [buffered, setBuffered] = useState<[number, number][]>([]);
  const [fullscreen, setFullscreen] = useState(false);
  const [audio, setAudio] = useState('-1');
  const [subtitleChoice, setSubtitle] = useState<string | null>(null);
  const subtitles = useResource(
    ['subtitles', state.fileId],
    async (signal) =>
      result(
        await api.GET('/api/v1/files/{id}/subtitles', {
          params: { path: { id: state.fileId! } },
          signal,
        }),
      ),
    Boolean(state.fileId && auth),
  );
  const file = state.detail?.files.find((f) => f.id === state.fileId);
  const subtitle =
    subtitleChoice ??
    preferredSubtitle(file?.tracks ?? [], subtitles.data?.items ?? [], i18n.language, canTranscode);
  const uploadedId = subtitle.startsWith('upload:')
    ? subtitle.slice(7)
    : (file?.tracks.find((track) => track.index === Number(subtitle) && track.type === 'subtitle')
        ?.subtitleId ?? '');
  const personalTiming = usePersonalSubtitleTiming(state.fileId, subtitle);
  const subtitleDelay = personalTiming.value;
  const [quality, setQuality] = useState('auto');
  const [autoQuality, setAutoQuality] = useState(() =>
    file ? selectAutoQuality(file, networkDownlink()) : { maxHeight: 0 as const, maxBitrate: 0 },
  );
  const adaptiveQuality = useRef(createAutoQuality(autoQuality));
  const effectiveQuality = canTranscode
    ? quality === 'auto'
      ? String(autoQuality.maxHeight)
      : quality
    : '0';
  const sourceBitrate =
    file && file.duration > 0 ? Math.min(100000000, Math.ceil((file.size * 8) / file.duration)) : 0;
  const automaticBitrate =
    canTranscode && quality === 'auto'
      ? autoQuality.maxBitrate || (subtitle !== '-1' ? sourceBitrate : 0)
      : 0;
  const sampleQuality = useRef<(rate: number | null, speed: number) => void>(() => {});
  sampleQuality.current = (rate, speed) => {
    if (!canTranscode || quality !== 'auto' || !file || rate === null) return;
    const target = adaptiveQuality.current.sample(
      selectAutoQuality(file, rate, speed),
      performance.now(),
    );
    if (target) {
      start.current = positionRef.current;
      setAutoQuality(target);
    }
  };
  const effectiveSubtitle = canTranscode && !uploadedId ? subtitle : '-1';
  const effectiveSubtitleID = canTranscode ? uploadedId : '';
  const hasBurnedSubtitle = effectiveSubtitle !== '-1' || Boolean(effectiveSubtitleID);
  const [burnDelay] = useDebouncedValue(subtitleDelay, 500);
  const effectiveBurnDelay = hasBurnedSubtitle ? burnDelay : 0;
  const [revision, setRevision] = useState(0);
  const [offset, setOffset] = useState(0);
  const [volume, setVolume] = useState(0.8);
  const [speed, setSpeed] = useState('1');
  const [method, setMethod] = useState('direct');
  const [streamInfo, setStreamInfo] = useState<DTO<'PlaybackStreamDTO'>>();
  const [streamSize, setStreamSize] = useState<{ width: number; height: number } | null>(null);
  const start = useRef(state.detail?.item.position ?? 0);
  const seekInSession = useRef<(position: number) => boolean>(() => false);
  useEffect(() => {
    const sync = () =>
      setFullscreen(document.fullscreenElement === wrapper.current && Boolean(wrapper.current));
    document.addEventListener('fullscreenchange', sync);
    return () => document.removeEventListener('fullscreenchange', sync);
  }, []);
  useEffect(() => {
    if (!auth) useNativePlayer.getState().stop();
  }, [auth]);
  useEffect(() => {
    if (
      !state.fileId ||
      !video.current ||
      !auth ||
      system.isLoading ||
      subtitles.isLoading ||
      personalTiming.loading
    )
      return;
    const el = video.current;
    const frozen = frame.current;
    const headers = { Authorization: `Bearer ${auth}` };
    const abort = new AbortController();
    let cancelled = false;
    let session: DTO<'PlaybackDTO'> | undefined;
    let hls: Hls | undefined;
    let seq = 0;
    let base = 0;
    let activeStats: (() => LoaderStats) | undefined;
    let rateAt = 0;
    let mediaURL = '';
    let observer: PerformanceObserver | undefined;
    let infoTimer: number | undefined;
    seekInSession.current = (position) => {
      if (cancelled || session?.state !== 'ready' || !mediaURL || el.readyState === 0) return false;
      const local = position - base;
      const inBuffer = containsTime(el.buffered, local);
      const available =
        session.method === 'direct' ||
        inBuffer ||
        (hls
          ? hasSegment(hls.latestLevelDetails?.fragments ?? [], local)
          : containsTime(el.seekable, local));
      if (!available) return false;
      el.currentTime = local;
      // startLoad cancels the current request itself and schedules the selected segment immediately.
      if (hls && !inBuffer) hls.startLoad(local);
      return true;
    };
    setBuffered([]);
    setDownloadRate(null);
    setStreamSize(null);
    setStreamInfo(undefined);
    const onVideoResize = () => {
      if (
        !cancelled &&
        mediaURL &&
        session?.state === 'ready' &&
        el.videoWidth > 0 &&
        el.videoHeight > 0
      ) {
        setStreamSize({ width: el.videoWidth, height: el.videoHeight });
      }
    };
    const onProgress = () => {
      if (!cancelled && session?.state === 'ready') {
        setBuffered(bufferedRanges(el.buffered, base, session.duration));
      }
    };
    const recordRate = (rate: number | null) => {
      if (rate !== null && !cancelled) {
        rateAt = performance.now();
        setDownloadRate(rate);
      }
    };
    // Native media exposes completed requests only; unavailable/cache-only metrics stay unknown.
    if (typeof PerformanceObserver !== 'undefined') {
      observer = new PerformanceObserver((list) => {
        if (hls || cancelled || !mediaURL) return;
        for (const entry of list.getEntries() as PerformanceResourceTiming[]) {
          if (
            entry.name === mediaURL &&
            entry.transferSize > 0 &&
            entry.initiatorType !== 'fetch'
          ) {
            const rate = transferRate(
              entry.encodedBodySize,
              entry.responseStart,
              entry.responseEnd,
            );
            recordRate(rate);
            sampleQuality.current(rate, el.playbackRate);
          }
        }
      });
      try {
        observer.observe({ type: 'resource' });
      } catch {
        observer.disconnect();
      }
    }
    const metricsTimer = window.setInterval(() => {
      onProgress();
      const stats = activeStats?.();
      if (stats && !stats.aborted && stats.loaded > 0) {
        recordRate(
          transferRate(stats.loaded, stats.loading.start, stats.loading.end || performance.now()),
        );
      } else if (rateAt && performance.now() - rateAt > 3000) {
        setDownloadRate(hls || session?.protocol === 'mp4' ? 0 : null);
      }
    }, 250);
    let chain = Promise.resolve();
    let last = Date.now();
    const initial = start.current;
    setLoading(true);
    setError(false);
    setErrorKey('playbackError');
    setPlaying(false);
    const report = () => {
      if (!session || session.state !== 'ready') return;
      const id = session.id;
      const body = { sequence: ++seq, position: positionRef.current };
      chain = chain
        .then(async () => {
          result(
            await api.POST('/api/v1/playback/{id}/progress', {
              params: { path: { id } },
              body,
              keepalive: true,
              headers,
            }),
          );
          void queryClient.invalidateQueries({
            queryKey: ['native', useAuth.getState().user?.id, 'resume'],
          });
        })
        .catch(() => {});
    };
    const onTime = () => {
      if (el.readyState < 2) return;
      const current = el.currentTime + base;
      positionRef.current = current;
      if (!scrubbing.current) setPosition(current);
      if (Date.now() - last >= 10000) {
        last = Date.now();
        report();
      }
    };
    const onPause = () => {
      setPlaying(false);
      report();
    };
    const onWaiting = () => setLoading(true);
    const onReady = () => {
      if (el.readyState < 2) return;
      setLoading(false);
      if (frozen) frozen.hidden = true;
    };
    const onPlaying = () => {
      onReady();
      setPlaying(true);
      setLoading(false);
    };
    const failPlayback = (decodeFailure: boolean) => {
      if (cancelled) return;
      // Network errors do not imply that the video codec needs conversion.
      if (decodeFailure && session?.method === 'direct' && fallback === 0) {
        start.current = positionRef.current;
        setFallback(1);
        return;
      }
      if (decodeFailure && session?.method === 'remux' && canTranscode && fallback < 2) {
        start.current = positionRef.current;
        setFallback(2);
        return;
      }
      setError(true);
      setLoading(false);
    };
    const onError = () => failPlayback(!el.error || el.error.code === 3 || el.error.code === 4);
    const onLoaded = () => {
      onVideoResize();
      if (session?.method === 'direct' && initial > 0) el.currentTime = initial;
      void el.play().catch(() => {
        setLoading(false);
      });
    };
    el.addEventListener('progress', onProgress);
    el.addEventListener('resize', onVideoResize);
    el.addEventListener('timeupdate', onTime);
    el.addEventListener('pause', onPause);
    el.addEventListener('playing', onPlaying);
    el.addEventListener('waiting', onWaiting);
    el.addEventListener('seeking', onWaiting);
    el.addEventListener('seeked', onReady);
    el.addEventListener('loadeddata', onReady);
    el.addEventListener('error', onError);
    el.addEventListener('loadedmetadata', onLoaded);
    window.addEventListener('pagehide', report);
    void (async () => {
      try {
        const support = file
          ? await playbackCapabilities(
              file,
              Number(audio),
              (mime) => el.canPlayType(mime),
              (mime) => typeof MediaSource !== 'undefined' && MediaSource.isTypeSupported(mime),
              navigator.mediaCapabilities?.decodingInfo.bind(navigator.mediaCapabilities),
            )
          : undefined;
        if (cancelled) return;
        const response = await api.POST('/api/v1/playback', {
          headers,
          body: {
            fileId: state.fileId!,
            capabilities: support?.capabilities,
            position: initial,
            directPlay: fallback === 0 && Boolean(support?.capabilities.containers.length),
            forceTranscode: fallback === 2,
            autoQuality: canTranscode && quality === 'auto',
            audioIndex: Number(audio),
            subtitleIndex: Number(effectiveSubtitle),
            ...(effectiveSubtitleID ? { subtitleId: effectiveSubtitleID } : {}),
            subtitleDelay: effectiveBurnDelay,
            maxBitrate:
              quality === 'auto'
                ? automaticBitrate
                : ((
                    { '720': 4000000, '1080': 8000000, '2160': 20000000 } as Record<string, number>
                  )[effectiveQuality] ?? 0),
            maxHeight: Number(effectiveQuality) as 0 | 720 | 1080 | 2160,
          },
        });
        if (
          response.response.status === 409 &&
          response.error?.detail === 'Video transcoding is disabled'
        )
          throw new Error('native.transcodingDisabled');
        if (
          response.response.status === 409 &&
          response.error?.detail === 'HDR video transcoding is unavailable'
        )
          throw new Error('native.hdrTranscodingUnavailable');
        session = result(response);
        if (cancelled) {
          await api.DELETE('/api/v1/playback/{id}', {
            headers,
            params: { path: { id: session.id } },
          });
          return;
        }
        const streamToken = session.streamToken;
        const deadline = Date.now() + 120000;
        while (session.state === 'preparing') {
          if (Date.now() > deadline) throw new Error('native.playbackTimeout');
          await new Promise<void>((resolve, reject) => {
            const done = () => {
              clearTimeout(timer);
              reject(new Error('Cancelled'));
            };
            const timer = setTimeout(() => {
              abort.signal.removeEventListener('abort', done);
              resolve();
            }, 500);
            abort.signal.addEventListener('abort', done, { once: true });
          });
          session = result(
            await api.GET('/api/v1/playback/{id}', {
              params: { path: { id: session.id } },
              signal: abort.signal,
            }),
          );
        }
        if (cancelled) return;
        if (session.state !== 'ready')
          throw new Error(
            session.decision?.toneMapped
              ? 'native.hdrGPUFailed'
              : session.method === 'transcode'
                ? 'native.videoGPUFailed'
                : 'native.playbackFailed',
          );
        setMethod(session.method);
        setStreamInfo(session.stream);
        if (
          session.method !== 'direct' &&
          (!session.stream || session.stream.bitrateSource === 'pending')
        ) {
          const id = session.id;
          let attempts = 0;
          const refreshInfo = async () => {
            if (cancelled || ++attempts > 20) return;
            try {
              const updated = result(
                await api.GET('/api/v1/playback/{id}', {
                  params: { path: { id } },
                  signal: abort.signal,
                }),
              );
              if (cancelled) return;
              if (updated.stream) {
                setStreamInfo(updated.stream);
                if (updated.stream.bitrateSource !== 'pending') return;
              }
            } catch {
              if (cancelled) return;
            }
            infoTimer = window.setTimeout(() => void refreshInfo(), 1000);
          };
          infoTimer = window.setTimeout(() => void refreshInfo(), 1000);
        }
        base = session.method === 'direct' ? 0 : session.position;
        setOffset(base);
        const url = `${session.url}?token=${encodeURIComponent(streamToken ?? '')}`;
        mediaURL = new URL(url, window.location.href).href;
        if (session.method === 'direct' && quality === 'auto' && canTranscode) {
          for (let sample = 0; sample < 2 && !cancelled; sample++) {
            const rate = await measurePlaybackRate(mediaURL, abort.signal);
            recordRate(rate);
            sampleQuality.current(rate, el.playbackRate);
          }
          if (cancelled) return;
        }
        if (session.method === 'direct') el.src = url;
        else if (session.protocol === 'mp4' && support) {
          setDownloadRate(0);
          void playRemux(el, url, support.remuxMime, abort.signal, (bytes, started, ended) => {
            const rate = transferRate(bytes, started, ended);
            recordRate(rate);
            sampleQuality.current(rate, el.playbackRate);
          }).catch((error: unknown) => {
            if (!cancelled) failPlayback(error instanceof RemuxDecodeError);
          });
        } else {
          const { default: Hls } = await import('hls.js');
          if (cancelled) return;
          if (Hls.isSupported()) {
            hls = new Hls({ startPosition: 0, ...INITIAL_BUFFER_CONFIG });
            const adaptiveBuffer = createAdaptiveBuffer(hls.config);
            hls.on(Hls.Events.FRAG_LOADING, (_event, data) => {
              activeStats = () => data.part?.stats ?? data.frag.stats;
              setDownloadRate(0);
            });
            hls.on(Hls.Events.FRAG_LOADED, (_event, data) => {
              if (
                data.frag.type === 'main' &&
                typeof data.frag.sn === 'number' &&
                !data.frag.stats.aborted
              ) {
                const stats = data.part?.stats ?? data.frag.stats;
                adaptiveBuffer.sample(
                  stats.loaded,
                  data.part?.duration ?? data.frag.duration,
                  stats.loading.end - stats.loading.start,
                  el.playbackRate,
                );
              }
              recordRate(
                transferRate(
                  data.frag.stats.loaded,
                  data.frag.stats.loading.start,
                  data.frag.stats.loading.end,
                ),
              );
              if (data.frag.type === 'main' && !data.frag.stats.aborted) {
                const stats = data.part?.stats ?? data.frag.stats;
                sampleQuality.current(
                  transferRate(stats.loaded, stats.loading.first, stats.loading.end),
                  el.playbackRate,
                );
              }
              activeStats = undefined;
            });
            hls.on(Hls.Events.BUFFER_APPENDED, onProgress);
            let recovered = false;
            hls.on(Hls.Events.ERROR, (_event, data) => {
              if (data.details === Hls.ErrorDetails.BUFFER_FULL_ERROR) adaptiveBuffer.suspend();
              if (!data.fatal) return;
              if (data.type === Hls.ErrorTypes.MEDIA_ERROR && !recovered) {
                recovered = true;
                hls?.recoverMediaError();
              } else failPlayback(data.type === Hls.ErrorTypes.MEDIA_ERROR);
            });
            hls.attachMedia(el);
            hls.loadSource(url);
          } else if (el.canPlayType('application/vnd.apple.mpegurl')) el.src = url;
          else throw new Error('HLS unsupported');
        }
      } catch (cause) {
        if (!cancelled) {
          if (cause instanceof Error && cause.message.startsWith('native.'))
            setErrorKey(cause.message);
          setError(true);
          setLoading(false);
        }
      }
    })();
    return () => {
      cancelled = true;
      start.current = positionRef.current;
      seekInSession.current = () => false;
      window.clearTimeout(infoTimer);
      window.clearInterval(metricsTimer);
      observer?.disconnect();
      el.removeEventListener('progress', onProgress);
      el.removeEventListener('resize', onVideoResize);
      abort.abort();
      report();
      const owned = session;
      if (owned)
        void chain.finally(() =>
          api.DELETE('/api/v1/playback/{id}', {
            params: { path: { id: owned.id } },
            keepalive: true,
            headers,
          }),
        );
      window.removeEventListener('pagehide', report);
      el.removeEventListener('timeupdate', onTime);
      el.removeEventListener('pause', onPause);
      el.removeEventListener('playing', onPlaying);
      el.removeEventListener('waiting', onWaiting);
      el.removeEventListener('seeking', onWaiting);
      el.removeEventListener('seeked', onReady);
      el.removeEventListener('loadeddata', onReady);
      // Preserve the last decoded frame while the next HLS session is prepared.
      if (frozen && el.readyState >= 2 && el.videoWidth > 0) {
        try {
          frozen.width = Math.min(el.videoWidth, 1280);
          frozen.height = Math.round((frozen.width * el.videoHeight) / el.videoWidth);
          frozen.getContext('2d')?.drawImage(el, 0, 0, frozen.width, frozen.height);
          frozen.hidden = false;
        } catch {
          frozen.hidden = true;
        }
      }
      el.removeEventListener('error', onError);
      el.removeEventListener('loadedmetadata', onLoaded);
      el.pause();
      hls?.destroy();
      el.removeAttribute('src');
      el.load();
    };
  }, [
    state.fileId,
    auth,
    audio,
    effectiveSubtitle,
    effectiveSubtitleID,
    effectiveBurnDelay,
    effectiveQuality,
    automaticBitrate,
    quality,
    system.isLoading,
    subtitles.isLoading,
    personalTiming.loading,
    revision,
    fallback,
    file,
    canTranscode,
  ]);
  useEffect(() => {
    if (video.current) {
      video.current.volume = volume;
      video.current.playbackRate = Number(speed);
    }
  }, [volume, speed, revision]);
  if (!state.detail || !file) return null;
  const restart = (fn: () => void) => {
    start.current = positionRef.current;
    fn();
  };
  const next = async () => {
    if (!state.detail?.nextId) return;
    try {
      const detail = result(
        await api.GET('/api/v1/items/{id}', { params: { path: { id: state.detail.nextId } } }),
      );
      const f = detail.files.find((f) => f.available);
      if (f) state.play(detail, f.id);
    } catch {
      toast.error(t('error'));
    }
  };
  const full = async () => {
    try {
      if (document.fullscreenElement) await document.exitFullscreen();
      else if (wrapper.current?.requestFullscreen) await wrapper.current.requestFullscreen();
      else {
        const el = video.current as HTMLVideoElement & { webkitEnterFullscreen?: () => void };
        if (!el.webkitEnterFullscreen) throw new Error();
        el.webkitEnterFullscreen();
      }
    } catch {
      toast.error(t('error'));
    }
  };
  return (
    <div
      ref={wrapper}
      className={`player ${state.expanded || fullscreen ? 'player-expanded' : 'player-mini'}`}
    >
      <div className="player-top">
        <div>
          <span className="eyebrow">{t('nowPlaying')}</span>
          <h2>{state.detail.item.title}</h2>
        </div>
        <Group gap="xs">
          <button
            className="icon-button"
            aria-label={t(state.expanded ? 'collapse' : 'expand')}
            onClick={() => {
              if (fullscreen) void document.exitFullscreen();
              state.expand();
            }}
          >
            {state.expanded ? <Minimize2 /> : <Maximize />}
          </button>
          <button className="icon-button" aria-label={t('stop')} onClick={state.stop}>
            <X />
          </button>
        </Group>
      </div>
      <div className="media-stage">
        <video
          ref={video}
          preload="auto"
          playsInline
          onClick={() => {
            const el = video.current;
            if (el) {
              if (el.paused) void el.play().catch(() => setError(true));
              else el.pause();
            }
          }}
          onEnded={() => {
            if (state.detail?.nextId) void next();
          }}
        />
        <canvas ref={frame} className="player-frozen-frame" hidden aria-hidden="true" />
        {loading && (
          <div className="native-player-loading" role="status">
            <Loader size={18} type="dots" />
            <span>{t('playerSub.buffering')}</span>
            <span className="player-download-rate" aria-label={t('playerMetrics.downloadSpeed')}>
              {downloadRate === null
                ? t('playerMetrics.unavailable')
                : formatTransferRate(downloadRate, i18n.language)}
            </span>
          </div>
        )}
      </div>
      {error && (
        <Alert color="red" className="player-error">
          {t(errorKey)}
          <Button variant="subtle" onClick={() => restart(() => setRevision((v) => v + 1))}>
            {t('retry')}
          </Button>
        </Alert>
      )}
      <div className="player-controls">
        <div className="seek-row">
          <span>{clockTime(position)}</span>
          <Slider
            className="player-seek"
            style={
              {
                '--buffer-gradient': buffered.length
                  ? buffered
                      .map(
                        ([start, end]) =>
                          `linear-gradient(to right, transparent ${start}%, #ffffff55 ${start}%, #ffffff55 ${end}%, transparent ${end}%)`,
                      )
                      .join(', ')
                  : 'none',
              } as CSSProperties
            }
            data-buffered-ranges={buffered.length}
            aria-label={t('position')}
            thumbLabel={t('position')}
            size={3}
            thumbSize={10}
            label={clockTime}
            step={0.1}
            min={0}
            max={file.duration || 1}
            value={Math.min(position, file.duration)}
            onChange={(value) => {
              scrubbing.current = true;
              setPosition(value);
            }}
            onChangeEnd={(value) => {
              scrubbing.current = false;
              positionRef.current = value;
              if (!seekInSession.current(value)) {
                start.current = value;
                setRevision((v) => v + 1);
              }
            }}
          />
          <span>{clockTime(file.duration)}</span>
        </div>
        <div className="transport">
          <Group wrap="nowrap" className="transport-primary">
            <button
              className="play-toggle"
              aria-label={t(playing ? 'pause' : 'play')}
              onClick={() => {
                if (video.current?.paused) void video.current.play().catch(() => setError(true));
                else video.current?.pause();
              }}
            >
              {playing ? <Pause /> : <Play />}
            </button>
            {state.detail.nextId && (
              <button className="icon-button" aria-label={t('next')} onClick={() => void next()}>
                <SkipForward />
              </button>
            )}
            <div className="volume">
              <Volume2 size={18} />
              <Slider
                aria-label={t('volume')}
                thumbLabel={t('volume')}
                size={3}
                thumbSize={10}
                label={(value) => `${Math.round(value * 100)}%`}
                min={0}
                max={1}
                step={0.01}
                value={volume}
                onChange={setVolume}
              />
            </div>
          </Group>
          <div className="playback-selectors-scroll">
            <div className="playback-selectors">
              <PlaybackOption
                portalTarget={wrapper.current ?? undefined}
                label={t('quality')}
                disabled={!canTranscode}
                title={!canTranscode ? t('native.transcodingDisabled') : undefined}
                value={canTranscode ? quality : '0'}
                onChange={(v) => restart(() => setQuality(v ?? 'auto'))}
                data={[
                  { value: 'auto', label: t('playerQuality.auto') },
                  { value: '0', label: t('original') },
                  { value: '720', label: '720p' },
                  { value: '1080', label: '1080p' },
                  { value: '2160', label: '4K (2160p)' },
                ]}
              />
              <PlaybackOption
                portalTarget={wrapper.current ?? undefined}
                label={t('audio')}
                value={audio}
                onChange={(v) => restart(() => setAudio(v ?? '-1'))}
                data={[
                  { value: '-1', label: t('default') },
                  ...file.tracks
                    .filter((t) => t.type === 'audio')
                    .map((track) => ({
                      value: String(track.index),
                      label: `${track.language || track.codec} ${track.index}`,
                    })),
                ]}
              />
              <PlaybackOption
                portalTarget={wrapper.current ?? undefined}
                label={t('subtitles')}
                footer={
                  <>
                    {canTranscode ? (
                      <SubtitleUpload
                        fileId={file.id}
                        onUploaded={async (id) => {
                          await subtitles.refetch();
                          restart(() => {
                            setSubtitle(`upload:${id}`);
                          });
                        }}
                      />
                    ) : (
                      <span>{t('native.transcodingDisabled')}</span>
                    )}
                    {subtitles.data?.items.find((sub) => sub.id === uploadedId)?.uploaded && (
                      <UploadedSubtitleDelete
                        fileId={file.id}
                        subtitleId={uploadedId}
                        onDeleted={async () => {
                          await subtitles.refetch();
                          restart(() => {
                            setSubtitle('-1');
                          });
                        }}
                      />
                    )}
                  </>
                }
                value={canTranscode && uploadedId ? 'upload:' + uploadedId : effectiveSubtitle}
                onChange={(v) =>
                  restart(() => {
                    setSubtitle(v ?? '-1');
                  })
                }
                data={[
                  { value: '-1', label: t('off') },
                  ...file.tracks
                    .filter((t) => t.type === 'subtitle' && !t.externalSubtitle && canTranscode)
                    .map((track) => ({
                      value: String(track.index),
                      label: `${track.language || track.codec} ${track.index} · ${t('playerSub.burned')}`,
                    })),
                  ...(canTranscode
                    ? (subtitles.data?.items.map((sub) => ({
                        value: `upload:${sub.id}`,
                        label: sub.name,
                      })) ?? [])
                    : []),
                ]}
              />
              <SubtitleTiming
                key={subtitle}
                active={personalTiming.ready && hasBurnedSubtitle}
                burned={hasBurnedSubtitle}
                portalTarget={wrapper.current ?? undefined}
                value={subtitleDelay}
                onChange={(v) => restart(() => personalTiming.change(v))}
                onChangeEnd={personalTiming.save}
              />
              <PlaybackOption
                portalTarget={wrapper.current ?? undefined}
                label={t('speed')}
                value={speed}
                onChange={(v) => setSpeed(v ?? '1')}
                data={['0.75', '1', '1.25', '1.5', '2'].map((v) => ({ value: v, label: `${v}x` }))}
              />
            </div>
          </div>
          <Group gap="xs" wrap="nowrap">
            {streamSize && (
              <StreamIndicator
                method={method}
                stream={streamInfo}
                size={streamSize}
                portalTarget={wrapper.current ?? undefined}
              />
            )}
            <button
              className="icon-button"
              aria-label={t(fullscreen ? 'exitFullscreen' : 'fullscreen')}
              onClick={() => void full()}
            >
              {fullscreen ? <Minimize2 /> : <Maximize />}
            </button>
          </Group>
        </div>
      </div>
      <span hidden>{offset}</span>
    </div>
  );
}
