import { useEffect, useRef, useState } from 'react';
import { Alert, Button, Group, Modal, ScrollArea, Select, Slider, Stack } from '@mantine/core';
import { ListMusic, Pause, Play, Repeat, Shuffle, SkipBack, SkipForward, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type Hls from 'hls.js';
import { api, result, useAuth, type DTO } from './api';
import { useNativePlayer } from './Player';
export const isAudioItem = (kind: string) =>
  ['track', 'podcast_episode', 'book_part'].includes(kind);
export const isAudioGroup = (kind: string) => ['album', 'podcast', 'audiobook'].includes(kind);
export const isAudioLibrary = (kind: string) => ['music', 'podcasts', 'audiobooks'].includes(kind);
interface AudioState {
  owner?: string;
  queues: Record<string, DTO<'ItemDTO'>[]>;
  index: number;
  generation: number;
  active: boolean;
  shuffle: boolean;
  repeat: 'off' | 'all' | 'one';
  rate: number;
  sleepMinutes: number;
  sleepUntil?: number;
  play: (items: DTO<'ItemDTO'>[], index?: number) => void;
  stop: () => void;
  advance: (delta: number, ended?: boolean) => void;
}
export const useAudioPlayer = create<AudioState>()(
  persist(
    (set, get) => ({
      queues: {},
      index: 0,
      generation: 0,
      active: false,
      shuffle: false,
      repeat: 'off',
      rate: 1,
      sleepMinutes: 0,
      play: (items, index = 0) => {
        const owner = useAuth.getState().user?.id;
        if (!owner || !items.length) return;
        useNativePlayer.getState().stop();
        set((s) => ({
          owner,
          queues: { ...s.queues, [owner]: items },
          index,
          active: true,
          generation: s.generation + 1,
        }));
      },
      stop: () => set({ active: false }),
      advance: (delta, ended = false) => {
        const s = get();
        const items = s.queues[s.owner ?? ''] ?? [];
        let index = ended && s.repeat === 'one' ? s.index : s.index + delta;
        if (s.shuffle && delta > 0 && items.length > 1)
          index = (s.index + 1 + Math.floor(Math.random() * (items.length - 1))) % items.length;
        if (index >= items.length && s.repeat === 'all') index = 0;
        if (index < 0) index = 0;
        if (index >= items.length) {
          s.stop();
          return;
        }
        set({ index, generation: s.generation + 1 });
      },
    }),
    {
      name: 'jfe.audio-queues',
      partialize: (s) => ({ queues: s.queues, shuffle: s.shuffle, repeat: s.repeat }),
    },
  ),
);
export function audioMIME(file: DTO<'FileDTO'>) {
  const ext = file.name.split('.').pop()?.toLowerCase() ?? '';
  const mime: Record<string, string> = {
    mp3: 'audio/mpeg',
    flac: 'audio/flac',
    m4a: 'audio/mp4',
    m4b: 'audio/mp4',
    aac: 'audio/aac',
    ogg: 'audio/ogg',
    opus: 'audio/ogg; codecs="opus"',
    wav: 'audio/wav',
    aiff: 'audio/aiff',
    aif: 'audio/aiff',
  };
  const codec = file.tracks.find((track) => track.type === 'audio')?.codec;
  if (ext === 'm4a' || ext === 'm4b')
    return codec === 'aac'
      ? 'audio/mp4; codecs="mp4a.40.2"'
      : codec === 'alac'
        ? 'audio/mp4; codecs="alac"'
        : '';
  return mime[ext] ?? '';
}
export default function AudioPlayer() {
  const s = useAudioPlayer();
  const user = useAuth((v) => v.user?.id);
  useEffect(
    () =>
      useNativePlayer.subscribe((v) => {
        if (v.detail) useAudioPlayer.getState().stop();
      }),
    [],
  );
  if (!s.active || s.owner !== user) return null;
  return <AudioSurface key={`${s.owner}:${s.generation}`} />;
}
function AudioSurface() {
  const { t } = useTranslation();
  const s = useAudioPlayer();
  const items = s.queues[s.owner ?? ''] ?? [];
  const item = items[s.index];
  const element = useRef<HTMLAudioElement>(null);
  const session = useRef<DTO<'PlaybackDTO'> | null>(null);
  const forceAudio = useRef(false);
  const sequence = useRef(0);
  const desired = useRef<number | null>(null);
  const [detail, setDetail] = useState<DTO<'DetailDTO'>>();
  const [error, setError] = useState(false);
  const [ready, setReady] = useState(false);
  const [paused, setPaused] = useState(true);
  const [position, setPosition] = useState(0);
  const [duration, setDuration] = useState(0);
  const [queue, setQueue] = useState(false);
  const speed = String(s.rate);
  const sleep = String(s.sleepMinutes);
  const [restart, setRestart] = useState(0);
  useEffect(() => {
    if (!item) return;
    let cancelled = false;
    let hls: Hls | undefined;
    let current: DTO<'PlaybackDTO'> | undefined;
    const player = element.current!;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let loaded: (() => void) | undefined;
    const stop = (id: string) =>
      api.DELETE('/api/v1/playback/{id}', { params: { path: { id } } }).catch(() => {});
    async function start() {
      try {
        const d = result(
          await api.GET('/api/v1/items/{id}', { params: { path: { id: item.id } } }),
        );
        if (cancelled) return;
        const file = d.files.find((f) => f.available);
        if (!file) throw new Error('unavailable');
        setDetail(d);
        setDuration(file.duration);
        const from = desired.current ?? (item.kind === 'track' ? 0 : d.item.position);
        current = result(
          await api.POST('/api/v1/playback', {
            body: {
              fileId: file.id,
              position: from,
              directPlay: Boolean(player.canPlayType(audioMIME(file))),
              audioIndex: -1,
              subtitleIndex: -1,
              maxHeight: 0,
              maxBitrate: 0,
              forceTranscode: forceAudio.current,
              subtitleDelay: 0,
            },
          }),
        );
        if (cancelled) {
          void stop(current.id);
          return;
        }
        session.current = current;
        sequence.current = 0;
        const check = async () => {
          if (cancelled || !current) return;
          const p = result(
            await api.GET('/api/v1/playback/{id}', { params: { path: { id: current.id } } }),
          );
          if (cancelled) return;
          if (p.state === 'failed' || p.state === 'stopped') throw new Error('playback failed');
          if (p.state !== 'ready') {
            timer = setTimeout(
              () =>
                void check().catch(() => {
                  if (!cancelled) setError(true);
                }),
              600,
            );
            return;
          }
          const url = `${current.url}?token=${encodeURIComponent(current.streamToken ?? '')}`;
          const seekOnLoad = () => {
            if (current?.method === 'direct') player.currentTime = from;
            void player.play().catch(() => setPaused(true));
          };
          loaded = seekOnLoad;
          player.addEventListener('loadedmetadata', seekOnLoad, { once: true });
          const module = current.method !== 'direct' ? await import('hls.js') : undefined;
          if (cancelled) return;
          if (module?.default.isSupported()) {
            hls = new module.default();
            hls.loadSource(url);
            hls.attachMedia(player);
            hls.on(module.default.Events.ERROR, (_event, data) => {
              if (data.fatal) setError(true);
            });
          } else player.src = url;
          setReady(true);
        };
        await check();
      } catch {
        if (!cancelled) setError(true);
      }
    }
    void start();
    const heartbeat = setInterval(() => {
      if (!current) return;
      const actual = player.currentTime + (current.method === 'direct' ? 0 : current.position);
      void api.POST('/api/v1/playback/{id}/progress', {
        params: { path: { id: current.id } },
        body: { sequence: ++sequence.current, position: actual },
      });
    }, 10000);
    return () => {
      cancelled = true;
      clearInterval(heartbeat);
      clearTimeout(timer);
      if (current) {
        const actual = player.currentTime + (current.method === 'direct' ? 0 : current.position);
        const id = current.id;
        void api
          .POST('/api/v1/playback/{id}/progress', {
            params: { path: { id } },
            body: { sequence: ++sequence.current, position: actual },
          })
          .finally(() => void stop(id));
      }
      if (loaded) player.removeEventListener('loadedmetadata', loaded);
      hls?.destroy();
      player.pause();
      player.removeAttribute('src');
      player.load();
      session.current = null;
    };
  }, [item, restart]);
  useEffect(() => {
    if (element.current) element.current.playbackRate = s.rate;
  }, [s.rate, ready]);
  useEffect(() => {
    if (!s.sleepUntil) return;
    const timer = setTimeout(
      () => {
        element.current?.pause();
        useAudioPlayer.setState({ sleepUntil: undefined, sleepMinutes: 0 });
      },
      Math.max(0, s.sleepUntil - Date.now()),
    );
    return () => clearTimeout(timer);
  }, [s.sleepUntil]);
  useEffect(() => {
    if (!('mediaSession' in navigator) || !item) return;
    navigator.mediaSession.metadata = new MediaMetadata({
      title: item.title,
      artist: detail?.audio?.tags.artists.join(', ') ?? '',
      album: detail?.audio?.tags.album ?? '',
    });
    navigator.mediaSession.setActionHandler('play', () => void element.current?.play());
    navigator.mediaSession.setActionHandler('pause', () => element.current?.pause());
    navigator.mediaSession.setActionHandler('nexttrack', () =>
      useAudioPlayer.getState().advance(1),
    );
    navigator.mediaSession.setActionHandler('previoustrack', () =>
      useAudioPlayer.getState().advance(-1),
    );
    return () => {
      navigator.mediaSession.metadata = null;
      for (const a of ['play', 'pause', 'nexttrack', 'previoustrack'] as const)
        navigator.mediaSession.setActionHandler(a, null);
    };
  }, [item, detail]);
  function seek(value: number) {
    const p = session.current;
    if (!p) return;
    if (p.method === 'direct') element.current!.currentTime = value;
    else {
      desired.current = value;
      setReady(false);
      setRestart((n) => n + 1);
    }
    setPosition(value);
  }
  if (!item) return null;
  return (
    <section className="audio-dock" aria-label={t('audioUI.player')}>
      <audio
        ref={element}
        onPlay={() => setPaused(false)}
        onPause={() => setPaused(true)}
        onError={() => {
          if (session.current?.method === 'direct' && !forceAudio.current) {
            forceAudio.current = true;
            desired.current = element.current?.currentTime ?? 0;
            setReady(false);
            setRestart((n) => n + 1);
          } else setError(true);
        }}
        onEnded={() => s.advance(1, true)}
        onTimeUpdate={() =>
          setPosition(
            (element.current?.currentTime ?? 0) +
              (session.current?.method === 'direct' ? 0 : (session.current?.position ?? 0)),
          )
        }
      />
      <div className="audio-dock-title">
        <strong>{item.title}</strong>
        <small>{detail?.audio?.tags.artists.join(', ')}</small>
      </div>
      {error ? (
        <Alert color="red">
          {t('error')}
          <Button
            onClick={() => {
              setError(false);
              setRestart((n) => n + 1);
            }}
          >
            {t('retry')}
          </Button>
        </Alert>
      ) : (
        <>
          <Group gap="xs">
            <Button
              variant="subtle"
              aria-label={t('audioUI.previous')}
              onClick={() => s.advance(-1)}
            >
              <SkipBack size={18} />
            </Button>
            <Button
              loading={!ready}
              aria-label={t(paused ? 'play' : 'pause')}
              onClick={() => {
                if (paused) void element.current?.play().catch(() => setError(true));
                else element.current?.pause();
              }}
            >
              {paused ? <Play size={18} /> : <Pause size={18} />}
            </Button>
            <Button variant="subtle" aria-label={t('audioUI.next')} onClick={() => s.advance(1)}>
              <SkipForward size={18} />
            </Button>
          </Group>
          <div className="audio-dock-seek">
            <Slider
              aria-label={t('audioUI.seek')}
              value={Math.min(position, duration)}
              max={duration || 1}
              onChange={setPosition}
              onChangeEnd={seek}
            />
            <small>
              {Math.floor(position / 60)}:{String(Math.floor(position % 60)).padStart(2, '0')} /{' '}
              {Math.ceil(duration / 60)} {t('audioUI.minutes')}
            </small>
          </div>
        </>
      )}
      <Group gap={2}>
        <Button
          variant={s.shuffle ? 'light' : 'subtle'}
          aria-label={t('audioUI.shuffle')}
          onClick={() => useAudioPlayer.setState({ shuffle: !s.shuffle })}
        >
          <Shuffle size={17} />
        </Button>
        <Button
          variant={s.repeat === 'off' ? 'subtle' : 'light'}
          aria-label={`${t('audioUI.repeat')}: ${t(`audioUI.${s.repeat}`)}`}
          onClick={() =>
            useAudioPlayer.setState({
              repeat: s.repeat === 'off' ? 'all' : s.repeat === 'all' ? 'one' : 'off',
            })
          }
        >
          <Repeat size={17} />
          {s.repeat === 'one' ? '1' : ''}
        </Button>
        <Button variant="subtle" aria-label={t('audioUI.queue')} onClick={() => setQueue(true)}>
          <ListMusic size={18} />
        </Button>
        <Button variant="subtle" aria-label={t('close')} onClick={s.stop}>
          <X size={18} />
        </Button>
      </Group>
      <Modal
        opened={queue}
        onClose={() => setQueue(false)}
        title={t('audioUI.queue')}
        scrollAreaComponent={ScrollArea.Autosize}
      >
        <Stack>
          <Select
            label={t('audioUI.speed')}
            value={speed}
            onChange={(v) => useAudioPlayer.setState({ rate: Number(v ?? '1') })}
            data={['0.5', '0.75', '1', '1.25', '1.5', '1.75', '2']}
          />
          <Select
            label={t('audioUI.sleep')}
            value={sleep}
            onChange={(v) =>
              useAudioPlayer.setState({
                sleepMinutes: Number(v ?? '0'),
                sleepUntil: v && v !== '0' ? Date.now() + Number(v) * 60000 : undefined,
              })
            }
            data={[
              { value: '0', label: t('audioUI.off') },
              ...[15, 30, 60].map((n) => ({
                value: String(n),
                label: `${n} ${t('audioUI.minutes')}`,
              })),
            ]}
          />
          {(detail?.audio?.chapters ?? []).map((c, n) => (
            <Button key={n} variant="subtle" onClick={() => seek(c.start)}>
              {c.title || `${t('audioUI.chapter')} ${n + 1}`}
            </Button>
          ))}
          {items.map((entry, n) => (
            <Button
              key={`${entry.id}:${n}`}
              variant={n === s.index ? 'light' : 'subtle'}
              onClick={() => s.play(items, n)}
            >
              {entry.title}
            </Button>
          ))}
        </Stack>
      </Modal>
    </section>
  );
}
