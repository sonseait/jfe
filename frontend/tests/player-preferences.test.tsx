import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import { QueryClientProvider } from '@tanstack/react-query';
import Player, { useNativePlayer } from '../src/native/Player';
import { api, useAuth, type DTO } from '../src/native/api';
import { queryClient } from '../src/lib/query-client';
import i18n from '../src/locales';

const file: DTO<'FileDTO'> = {
  id: 'file',
  name: 'movie.mp4',
  size: 3000000000,
  duration: 1000,
  width: 3840,
  height: 2160,
  available: true,
  tracks: [
    { index: 0, type: 'video', codec: 'h264', title: '', language: '' },
    { index: 2, type: 'subtitle', codec: 'subrip', title: '', language: 'eng' },
    { index: 3, type: 'subtitle', codec: 'subrip', title: 'Tiếng Việt', language: 'und' },
  ],
};
const detail: DTO<'DetailDTO'> = {
  item: {
    id: 'item',
    libraryId: 'library',
    parentId: '',
    kind: 'movie',
    title: 'Movie',
    year: 2026,
    season: 0,
    episode: 0,
    overview: '',
    poster: '',
    providerId: '',
    metadataLocked: false,
    favorite: false,
    watched: false,
    position: 42,
    duration: 1000,
  },
  files: [file],
  cast: [],
  nextId: '',
};
let observer: PerformanceObserverCallback;
beforeEach(() => {
  queryClient.clear();
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 416 })));
  useAuth.setState({ token: 'test-token' });
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  vi.stubGlobal(
    'PerformanceObserver',
    class {
      constructor(callback: PerformanceObserverCallback) {
        observer = callback;
      }
      observe() {}
      disconnect() {}
    },
  );
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});
  vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {});
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue();
  vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
});
afterEach(() => {
  useNativePlayer.getState().stop();
  queryClient.clear();
  useAuth.setState({ token: undefined });
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
async function show(transcoding = true, external = false) {
  await i18n.changeLanguage('vi');
  vi.spyOn(api, 'GET').mockImplementation(((path: string) =>
    Promise.resolve({
      data:
        path === '/api/v1/system'
          ? { capabilities: { transcoding } }
          : path === '/api/v1/files/{id}/subtitles'
            ? { items: external ? [{ id: 'vi', name: 'movie.vi.srt', cueCount: 1 }] : [] }
            : { cues: [{ start: 0, end: 1000, text: 'Vietnamese overlay' }] },
      response: new Response(),
    })) as never);
  const post = vi.spyOn(api, 'POST').mockResolvedValue({
    data: {
      id: 'session',
      method: 'direct',
      state: 'ready',
      url: '/stream',
      streamToken: 'test',
      position: 42,
      duration: 1000,
    },
    response: new Response(),
  } as never);
  vi.spyOn(api, 'DELETE').mockResolvedValue({ data: {}, response: new Response() } as never);
  useNativePlayer.getState().play(detail, file.id);
  const view = render(
    <MantineProvider env="test">
      <QueryClientProvider client={queryClient}>
        <Player />
      </QueryClientProvider>
    </MantineProvider>,
  );
  await waitFor(() => expect(post).toHaveBeenCalledWith('/api/v1/playback', expect.anything()));
  return { post, view };
}

it('selects the Vietnamese track automatically and preserves a manual Off choice when language changes', async () => {
  const { post, view } = await show();
  expect(post).toHaveBeenCalledWith(
    '/api/v1/playback',
    expect.objectContaining({
      body: expect.objectContaining({ subtitleIndex: 3, autoQuality: true }),
    }),
  );
  fireEvent.click(screen.getByRole('button', { name: /^Phụ đề:/ }));
  fireEvent.click(screen.getByRole('menuitemradio', { name: 'Tắt' }));
  await waitFor(() =>
    expect(post).toHaveBeenLastCalledWith(
      '/api/v1/playback',
      expect.objectContaining({
        body: expect.objectContaining({ subtitleIndex: -1 }),
      }),
    ),
  );
  await act(() => i18n.changeLanguage('en'));
  expect(screen.getByRole('button', { name: 'Subtitles: Off' })).toBeVisible();
  view.unmount();
});

it('uses a Vietnamese overlay with transcoding disabled and keeps quality controls disabled', async () => {
  const { post, view } = await show(false, true);
  expect(await screen.findByText('Vietnamese overlay')).toBeVisible();
  expect(post).toHaveBeenCalledWith(
    '/api/v1/playback',
    expect.objectContaining({
      body: expect.objectContaining({
        subtitleIndex: -1,
        maxBitrate: 0,
        maxHeight: 0,
        autoQuality: false,
      }),
    }),
  );
  expect(screen.getByRole('button', { name: /^Chất lượng:/ })).toBeDisabled();
  view.unmount();
});

it('adapts to completed transfers at the current position and keeps manual quality fixed', async () => {
  const { post, view } = await show();
  await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
  const video = view.container.querySelector('video')!;
  Object.defineProperty(video, 'readyState', { configurable: true, value: 2 });
  video.currentTime = 120;
  fireEvent.timeUpdate(video);
  const slow = () =>
    observer(
      {
        getEntries: () => [
          {
            name: new URL('/stream?token=test', window.location.href).href,
            transferSize: 250000,
            encodedBodySize: 250000,
            responseStart: 100,
            responseEnd: 1100,
          },
        ],
      } as unknown as PerformanceObserverEntryList,
      {} as PerformanceObserver,
    );
  act(() => {
    slow();
    slow();
  });
  await waitFor(() =>
    expect(post).toHaveBeenLastCalledWith(
      '/api/v1/playback',
      expect.objectContaining({
        body: expect.objectContaining({
          position: 120,
          maxHeight: 720,
          maxBitrate: 1000000,
          autoQuality: true,
        }),
      }),
    ),
  );
  fireEvent.click(screen.getByRole('button', { name: /^Chất lượng:/ }));
  fireEvent.click(screen.getByRole('menuitemradio', { name: '1080p' }));
  await waitFor(() =>
    expect(post).toHaveBeenLastCalledWith(
      '/api/v1/playback',
      expect.objectContaining({
        body: expect.objectContaining({ autoQuality: false, maxHeight: 1080 }),
      }),
    ),
  );
  const calls = post.mock.calls.length;
  act(() => {
    for (let n = 0; n < 6; n++) slow();
  });
  expect(post.mock.calls).toHaveLength(calls);
  view.unmount();
});
