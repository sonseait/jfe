import { afterAll, afterEach, beforeAll, beforeEach, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import { QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { AudioDetail, Imports } from '../src/native/Audio';
import Admin from '../src/native/Admin';
import { audioMIME, useAudioPlayer } from '../src/native/AudioPlayer';
import { api, useAuth, type DTO } from '../src/native/api';
import { queryClient } from '../src/lib/query-client';
import i18n from '../src/locales';
import { audioErrorKey } from '../src/native/audio-errors';
const originalScrollIntoView = Object.getOwnPropertyDescriptor(
  HTMLElement.prototype,
  'scrollIntoView',
);
beforeAll(() => {
  // jsdom lacks the scrolling API used by Mantine's preselected options.
  Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
    configurable: true,
    value: vi.fn(),
  });
});
afterAll(() => {
  if (originalScrollIntoView)
    Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', originalScrollIntoView);
  else Reflect.deleteProperty(HTMLElement.prototype, 'scrollIntoView');
});
const item: DTO<'ItemDTO'> = {
  id: 'track',
  libraryId: 'library',
  parentId: 'album',
  kind: 'track',
  title: 'Original',
  year: 0,
  season: 0,
  episode: 1,
  overview: '',
  poster: '',
  providerId: '',
  metadataLocked: false,
  favorite: false,
  watched: false,
  position: 0,
  duration: 120,
};
const file: DTO<'FileDTO'> = {
  id: 'file',
  name: 'track.flac',
  available: true,
  duration: 100,
  width: 0,
  height: 0,
  size: 10,
  tracks: [{ index: 0, type: 'audio', codec: 'flac', language: '', title: '' }],
};
const detail: DTO<'DetailDTO'> = { item, files: [file], cast: [], nextId: '' };
const tags: DTO<'AudioTagsDTO'> = {
  title: 'Original',
  album: 'Album',
  artists: ['Artist'],
  albumArtists: [],
  genres: [],
  track: '1',
  disc: '',
  date: '',
  composer: '',
  comment: 'Keep',
  author: '',
  narrator: '',
  musicBrainzId: '',
};
beforeEach(async () => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  await i18n.changeLanguage('en');
  useAuth.setState({
    token: 'test',
    user: {
      id: 'user',
      username: 'user',
      role: 'user',
      disabled: false,
      libraryIds: ['library'],
      importLibraryIds: ['library'],
    },
  });
});
afterEach(() => {
  queryClient.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  useAudioPlayer.getState().stop();
});
function show() {
  render(
    <MantineProvider env="test">
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <AudioDetail detail={detail} />
        </MemoryRouter>
      </QueryClientProvider>
    </MantineProvider>,
  );
}
it.each([
  ['music', 'Âm nhạc'],
  ['podcasts', 'Podcast'],
  ['audiobooks', 'Sách nói'],
])(
  'creates a %s library from the Vietnamese admin form without local folders',
  async (kind, label) => {
    await i18n.changeLanguage('vi');
    useAuth.setState({ user: { ...useAuth.getState().user!, role: 'admin' } });
    vi.spyOn(api, 'GET').mockResolvedValue({
      data: { items: [] },
      response: new Response(),
    } as never);
    const save = vi
      .spyOn(api, 'POST')
      .mockResolvedValue({ data: { id: 'new' }, response: new Response() } as never);
    render(
      <MantineProvider env="test">
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <Admin />
          </MemoryRouter>
        </QueryClientProvider>
      </MantineProvider>,
    );
    fireEvent.click(screen.getAllByRole('button', { name: 'Thêm thư viện' })[0]);
    const form = within(await screen.findByRole('dialog'));
    fireEvent.change(form.getByRole('textbox', { name: 'Tên' }), {
      target: { value: 'Audio library' },
    });
    expect(form.getByRole('button', { name: 'Lưu thay đổi' })).toBeDisabled();
    fireEvent.click(form.getByRole('combobox', { name: 'Loại nội dung' }));
    fireEvent.click(await screen.findByRole('option', { name: label }));
    expect(form.getByText(/Để trống nếu chỉ nhập từ YouTube/)).toBeVisible();
    fireEvent.click(form.getByRole('button', { name: 'Lưu thay đổi' }));
    await waitFor(() =>
      expect(save).toHaveBeenCalledWith('/api/v1/libraries', {
        body: { name: 'Audio library', kind, paths: [], scanIntervalHours: 0 },
      }),
    );
  },
);
it('writes only explicitly edited tags after review, with the file fingerprint', async () => {
  vi.spyOn(api, 'GET').mockImplementation(
    async (path) =>
      ({
        data:
          path === '/api/v1/items/{id}'
            ? detail
            : { fileId: 'file', fingerprint: 'fingerprint', writable: true, tags },
        response: new Response(),
      }) as never,
  );
  const save = vi
    .spyOn(api, 'POST')
    .mockResolvedValue({ data: { items: [] }, response: new Response() } as never);
  show();
  fireEvent.click(screen.getByRole('button', { name: 'Edit file tags' }));
  const title = await screen.findByRole('textbox', { name: 'Title' });
  await waitFor(() => expect(title).toHaveValue('Original'));
  fireEvent.change(title, { target: { value: 'Edited' } });
  fireEvent.click(screen.getByRole('button', { name: 'Review changes' }));
  expect(save).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Write tags to files' }));
  await waitFor(() =>
    expect(save).toHaveBeenCalledWith('/api/v1/audio/tags', {
      body: { patch: { title: 'Edited' }, items: [{ fileId: 'file', fingerprint: 'fingerprint' }] },
    }),
  );
});
it('hides tag editing when the user only has read access', () => {
  useAuth.setState({ user: { ...useAuth.getState().user!, importLibraryIds: [] } });
  show();
  expect(screen.queryByRole('button', { name: 'Edit file tags' })).not.toBeInTheDocument();
});
it('preserves queue order and handles repeat without crossing accounts', () => {
  const player = useAudioPlayer.getState();
  player.play([item, { ...item, id: 'second' }]);
  player.advance(1);
  expect(useAudioPlayer.getState().index).toBe(1);
  useAudioPlayer.setState({ repeat: 'all' });
  player.advance(1, true);
  expect(useAudioPlayer.getState().index).toBe(0);
  useAuth.setState({ user: { ...useAuth.getState().user!, id: 'other' } });
  player.play([{ ...item, id: 'private' }]);
  expect(useAudioPlayer.getState().queues.user).toHaveLength(2);
  expect(useAudioPlayer.getState().queues.other[0].id).toBe('private');
});
it('detects audio containers independently from video codecs', () => {
  expect(audioMIME(file)).toBe('audio/flac');
  expect(
    audioMIME({ ...file, name: 'book.m4b', tracks: [{ ...file.tracks[0], codec: 'aac' }] }),
  ).toBe('audio/mp4; codecs="mp4a.40.2"');
});

it('shows translated retry and entry errors and confirms source removal in Vietnamese', async () => {
  await i18n.changeLanguage('vi');
  const responses: Record<string, unknown> = {
    '/api/v1/system': { capabilities: { youtube: true } },
    '/api/v1/libraries': { items: [] },
    '/api/v1/import-sources': {
      items: [
        {
          id: 'source',
          title: 'Album',
          url: 'https://youtu.be/abcdefghijk',
          paused: false,
          follow: false,
          entries: [
            {
              videoId: 'abcdefghijk',
              ordinal: 1,
              title: 'Song',
              state: 'failed',
              error: 'audio_storage_full',
            },
          ],
        },
      ],
    },
    '/api/v1/audio/jobs': {
      items: [
        {
          id: 'retry-job',
          kind: 'youtube_preview',
          state: 'pending',
          progress: 0,
          error: 'youtube_forbidden',
        },
      ],
    },
  };
  vi.spyOn(api, 'GET').mockImplementation(
    async (path) => ({ data: responses[path], response: new Response() }) as never,
  );
  const remove = vi
    .spyOn(api, 'DELETE')
    .mockResolvedValue({ data: {}, response: new Response() } as never);
  render(
    <MantineProvider env="test">
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <Imports />
        </MemoryRouter>
      </QueryClientProvider>
    </MantineProvider>,
  );
  expect(await screen.findByText(/YouTube từ chối truy cập \(HTTP 403\)/)).toBeVisible();
  expect(screen.getByText(/Mã tác vụ: retry-job/)).toBeVisible();
  expect(screen.getByText(/Kho import không đủ dung lượng trống/)).toBeVisible();
  fireEvent.click(screen.getByRole('button', { name: 'Xóa' }));
  const dialog = within(await screen.findByRole('dialog', { name: 'Xóa' }));
  expect(dialog.getByText('Xóa nguồn này? Các file đã tải sẽ được giữ lại.')).toBeVisible();
  expect(remove).not.toHaveBeenCalled();
  fireEvent.click(dialog.getByRole('button', { name: 'Xóa' }));
  await waitFor(() =>
    expect(remove).toHaveBeenCalledWith('/api/v1/import-sources/{id}', {
      params: { path: { id: 'source' } },
    }),
  );
});

it('does not render unrecognized worker diagnostics as translation keys', () => {
  expect(audioErrorKey('unexpected error https://example.test/?token=secret')).toBe(
    'audioUI.jobFailed',
  );
});

it('submits a public URL preview only to a granted audio library', async () => {
  const responses: Record<string, unknown> = {
    '/api/v1/system': { capabilities: { youtube: true } },
    '/api/v1/libraries': { items: [{ id: 'library', name: 'Music library', kind: 'music' }] },
    '/api/v1/import-sources': { items: [] },
    '/api/v1/audio/jobs': { items: [] },
  };
  vi.spyOn(api, 'GET').mockImplementation(
    async (path) => ({ data: responses[path], response: new Response() }) as never,
  );
  const save = vi
    .spyOn(api, 'POST')
    .mockResolvedValue({ data: { id: 'source' }, response: new Response() } as never);
  render(
    <MantineProvider env="test">
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <Imports />
        </MemoryRouter>
      </QueryClientProvider>
    </MantineProvider>,
  );
  fireEvent.change(screen.getByRole('textbox', { name: 'YouTube URL' }), {
    target: { value: 'https://youtu.be/abcdefghijk' },
  });
  fireEvent.click(await screen.findByRole('combobox', { name: 'Destination library' }));
  fireEvent.click(await screen.findByRole('option', { name: 'Music library' }));
  fireEvent.click(screen.getByRole('button', { name: 'Preview' }));
  await waitFor(() =>
    expect(save).toHaveBeenCalledWith('/api/v1/imports', {
      body: { url: 'https://youtu.be/abcdefghijk', libraryId: 'library', follow: false },
    }),
  );
});

it.each(['admin', 'user'] as const)('explains missing import libraries for %s', async (role) => {
  useAuth.setState({ user: { ...useAuth.getState().user!, role, importLibraryIds: [] } });
  const responses: Record<string, unknown> = {
    '/api/v1/system': { capabilities: { youtube: true } },
    '/api/v1/libraries': {
      items: [
        { id: 'movies', name: 'Movies', kind: 'movies' },
        ...(role === 'user' ? [{ id: 'library', name: 'Read-only music', kind: 'music' }] : []),
      ],
    },
    '/api/v1/import-sources': { items: [] },
    '/api/v1/audio/jobs': { items: [] },
  };
  vi.spyOn(api, 'GET').mockImplementation(
    async (path) => ({ data: responses[path], response: new Response() }) as never,
  );
  render(
    <MantineProvider env="test">
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <Imports />
        </MemoryRouter>
      </QueryClientProvider>
    </MantineProvider>,
  );
  expect(await screen.findByRole('combobox', { name: 'Destination library' })).toBeDisabled();
  expect(screen.getByRole('button', { name: 'Preview' })).toBeDisabled();
  if (role === 'admin') {
    expect(screen.getByText(/Create a Music, Podcasts or Audiobooks library first/)).toBeVisible();
    expect(screen.getByRole('link', { name: 'Manage libraries' })).toHaveAttribute(
      'href',
      '/admin/libraries',
    );
  } else {
    expect(screen.getByText(/Ask an administrator to grant library access/)).toBeVisible();
    expect(screen.queryByRole('link', { name: 'Manage libraries' })).not.toBeInTheDocument();
  }
});
