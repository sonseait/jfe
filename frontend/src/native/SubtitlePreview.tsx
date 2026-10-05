import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Button, Group, Loader, Slider, Text } from '@mantine/core';
import { Play, Pause, Volume2, VolumeX } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type Hls from 'hls.js';
import { api, result, type DTO } from './api';
import { playbackCapabilities } from './playback-support';
import { playRemux } from './remux-player';
import { activeSubtitleText, subtitleClock } from './subtitle-sync';

export function SubtitlePreview({
  file,
  cues,
  offsetMS,
  seek,
  onTime,
  onStopReady,
}: {
  file: DTO<'FileDTO'>;
  cues: DTO<'SubtitleSyncJobDTO'>['cues'];
  offsetMS: number;
  seek: { time: number; generation: number };
  onTime: (time: number) => void;
  onStopReady: (stop: (() => Promise<void>) | undefined) => void;
}) {
  const { t } = useTranslation();
  const video = useRef<HTMLVideoElement>(null);
  const session = useRef<string | undefined>(undefined);
  const mediaAbort = useRef<AbortController | undefined>(undefined);
  const mediaCleanup = useRef<(() => void) | undefined>(undefined);
  const [position, setPosition] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [playing, setPlaying] = useState(false);
  const [muted, setMuted] = useState(false);
  const [start, setStart] = useState({ time: 0, generation: 0 });
  const base = useRef(0);
  const wantsPlayback = useRef(false);
  const mediaReady = useRef(false);
  const directSource = useRef(false);
  const scrubbing = useRef(false);
  const stop = useCallback(async (preservePlayback = false) => {
    if (!preservePlayback) {
      wantsPlayback.current = false;
      setPlaying(false);
    }
    mediaReady.current = false;
    video.current?.pause();
    mediaAbort.current?.abort();
    mediaAbort.current = undefined;
    mediaCleanup.current?.();
    mediaCleanup.current = undefined;
    const id = session.current;
    if (!id) return;
    result(await api.DELETE('/api/v1/playback/{id}', { params: { path: { id } } }));
    if (session.current === id) session.current = undefined;
  }, []);
  useEffect(() => {
    onStopReady(stop);
    return () => onStopReady(undefined);
  }, [onStopReady, stop]);
  const resume = useCallback(() => {
    const el = video.current;
    if (!el || !wantsPlayback.current || !mediaReady.current || el.readyState < 2) return;
    const controller = mediaAbort.current;
    void el.play().catch((e: unknown) => {
      if (!wantsPlayback.current || controller !== mediaAbort.current || controller?.signal.aborted)
        return;
      // Pausing while a play promise is pending is an expected cancellation.
      if (e instanceof DOMException && e.name === 'AbortError') return;
      wantsPlayback.current = false;
      setPlaying(false);
      setError('preview_failed');
    });
  }, []);
  const requestSeek = useCallback(
    (value: number) => {
      scrubbing.current = false;
      setPosition(value);
      onTime(value);
      const el = video.current;
      if (!el) return;
      const relative = value - base.current;
      const ranges = directSource.current ? el.seekable : el.buffered;
      const canSeek =
        mediaReady.current &&
        Array.from({ length: ranges.length }, (_, i) => [ranges.start(i), ranges.end(i)]).some(
          ([a, b]) => a <= relative && relative <= b,
        );
      if (canSeek) {
        el.currentTime = relative;
        resume();
      } else setStart((s) => ({ time: value, generation: s.generation + 1 }));
    },
    [onTime, resume],
  );
  useEffect(() => {
    if (seek.generation) requestSeek(Math.min(file.duration, seek.time));
  }, [seek, requestSeek, file.duration]);
  useEffect(() => {
    const el = video.current;
    if (!el) return;
    const abort = new AbortController();
    let id: string | undefined;
    let hls: Hls | undefined;
    const releaseMedia = () => {
      mediaReady.current = false;
      if (heartbeat) {
        clearInterval(heartbeat);
        heartbeat = undefined;
      }
      hls?.destroy();
      hls = undefined;
      el.pause();
      el.removeAttribute('src');
      el.load();
    };
    let heartbeat: ReturnType<typeof setInterval> | undefined;
    let sequence = 0;
    setLoading(true);
    setError('');
    const run = async () => {
      await stop(true);
      if (abort.signal.aborted) return;
      mediaAbort.current = abort;
      mediaCleanup.current = releaseMedia;
      const support = await playbackCapabilities(
        file,
        -1,
        (mime) => el.canPlayType(mime),
        (mime) => typeof MediaSource !== 'undefined' && MediaSource.isTypeSupported(mime),
        navigator.mediaCapabilities?.decodingInfo.bind(navigator.mediaCapabilities),
      );
      if (abort.signal.aborted) return;
      const response = await api.POST('/api/v1/playback', {
        body: {
          fileId: file.id,
          preview: true,
          capabilities: support.capabilities,
          position: start.time,
          audioIndex: -1,
          subtitleIndex: -1,
          directPlay: true,
          maxHeight: 720,
          autoQuality: true,
          maxBitrate: 2000000,
        },
      });
      if (response.error)
        throw new Error(
          response.error.detail === 'Video transcoding is disabled'
            ? 'transcoding_disabled'
            : 'preview_failed',
        );
      let playback = result(response);
      id = playback.id;
      if (abort.signal.aborted) {
        await api.DELETE('/api/v1/playback/{id}', { params: { path: { id } } });
        return;
      }
      session.current = id;
      const token = playback.streamToken;
      const deadline = Date.now() + 90000;
      while (playback.state === 'preparing') {
        if (abort.signal.aborted) return;
        if (Date.now() > deadline) throw new Error('preview_failed');
        await new Promise<void>((resolve) => setTimeout(resolve, 500));
        if (abort.signal.aborted) return;
        playback = result(
          await api.GET('/api/v1/playback/{id}', {
            params: { path: { id } },
            signal: abort.signal,
          }),
        );
      }
      if (playback.state !== 'ready') throw new Error('preview_failed');
      directSource.current = playback.method === 'direct';
      base.current = playback.method === 'direct' ? 0 : playback.position;
      setPosition(start.time);
      onTime(start.time);
      const url = `${playback.url}?token=${encodeURIComponent(token ?? '')}`;
      if (playback.method === 'direct') {
        el.src = url;
        el.addEventListener(
          'loadedmetadata',
          () => {
            if (!abort.signal.aborted) el.currentTime = start.time;
          },
          { once: true, signal: abort.signal },
        );
      } else if (playback.protocol === 'mp4') {
        void playRemux(el, url, support.remuxMime, abort.signal).catch(() => {
          if (!abort.signal.aborted) setError('preview_failed');
        });
      } else {
        const { default: Hls } = await import('hls.js');
        if (abort.signal.aborted) return;
        if (Hls.isSupported()) {
          hls = new Hls({ maxBufferLength: 10, backBufferLength: 10 });
          hls.on(Hls.Events.ERROR, (_event, data) => {
            if (data.fatal && !abort.signal.aborted) setError('preview_failed');
          });
          hls.loadSource(url);
          hls.attachMedia(el);
        } else if (el.canPlayType('application/vnd.apple.mpegurl')) el.src = url;
        else throw new Error('preview_failed');
      }
      if (abort.signal.aborted) return;
      mediaReady.current = true;
      setLoading(false);
      resume();
      heartbeat = setInterval(() => {
        if (id)
          void api.POST('/api/v1/playback/{id}/progress', {
            params: { path: { id } },
            body: {
              position: Math.min(file.duration, base.current + el.currentTime),
              sequence: ++sequence,
            },
          });
      }, 15000);
    };
    void run().catch((e: unknown) => {
      if (!abort.signal.aborted) {
        setError(e instanceof Error ? e.message : 'preview_failed');
        setLoading(false);
        void stop().catch(() => {});
      }
    });
    return () => {
      abort.abort();
      if (mediaAbort.current === abort) mediaAbort.current = undefined;
      releaseMedia();
      if (mediaCleanup.current === releaseMedia) mediaCleanup.current = undefined;
      if (heartbeat) clearInterval(heartbeat);
      if (id)
        void api.DELETE('/api/v1/playback/{id}', { params: { path: { id } } }).catch(() => {});
      if (session.current === id) session.current = undefined;
    };
  }, [file, start, stop, resume, onTime]);
  useEffect(() => {
    if (error) void stop().catch(() => {});
  }, [error, stop]);
  const overlay = activeSubtitleText(cues, position, offsetMS);
  return (
    <>
      <div className="subtitle-editor-preview">
        <video
          ref={video}
          playsInline
          muted={muted}
          onCanPlay={resume}
          onSeeked={resume}
          onPlay={() => {
            if (!wantsPlayback.current) video.current?.pause();
          }}
          onEnded={() => {
            wantsPlayback.current = false;
            setPlaying(false);
          }}
          onError={() => {
            if (mediaReady.current) setError('preview_failed');
          }}
          onTimeUpdate={() => {
            if (!mediaReady.current || scrubbing.current) return;
            const value = base.current + (video.current?.currentTime ?? 0);
            setPosition(value);
            onTime(value);
          }}
        />
        {overlay && (
          <div className="player-subtitle-overlay">
            <span>{overlay}</span>
          </div>
        )}
        {loading && <Loader className="subtitle-editor-preview-loader" />}
      </div>
      {error && (
        <Alert color="red">
          {t(`subtitleEditor.${error === 'transcoding_disabled' ? error : 'preview_failed'}`)}
        </Alert>
      )}
      <Group>
        <Button
          variant="subtle"
          disabled={Boolean(error)}
          leftSection={playing ? <Pause size={16} /> : <Play size={16} />}
          onClick={() => {
            wantsPlayback.current = !wantsPlayback.current;
            setPlaying(wantsPlayback.current);
            if (wantsPlayback.current) resume();
            else video.current?.pause();
          }}
        >
          {t(playing ? 'pause' : 'subtitleEditor.play')}
        </Button>
        <Button
          variant="subtle"
          aria-label={t('subtitleEditor.mute')}
          onClick={() => setMuted((v) => !v)}
        >
          {muted ? <VolumeX size={16} /> : <Volume2 size={16} />}
        </Button>
        <Text size="xs">
          {subtitleClock(position)} / {subtitleClock(file.duration)}
        </Text>
      </Group>
      <Slider
        thumbLabel={t('subtitleEditor.seek')}
        min={0}
        max={Math.max(1, file.duration)}
        step={0.1}
        value={position}
        label={subtitleClock}
        onChange={(value) => {
          scrubbing.current = true;
          setPosition(value);
        }}
        onChangeEnd={requestSeek}
      />
    </>
  );
}
