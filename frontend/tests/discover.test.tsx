import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import Discover from '../src/native/Discover';
import { api, type DTO } from '../src/native/api';
import { useNativePlayer } from '../src/native/Player';
import i18n from '../src/locales';

const episode: DTO<'ItemDTO'> = {
  id: 'episode',
  libraryId: 'shows',
  parentId: 'show',
  kind: 'episode',
  title: 'Episode in progress',
  year: 2026,
  season: 1,
  episode: 2,
  overview: '',
  poster: '',
  providerId: '',
  metadataLocked: false,
  favorite: false,
  watched: false,
  position: 300,
  duration: 1200,
};
let client: QueryClient;
beforeEach(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
});
afterEach(() => {
  client.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
async function show(item = episode, language = 'en') {
  await i18n.changeLanguage(language);
  const get = vi.spyOn(api, 'GET').mockImplementation(
    async (path, options) =>
      ({
        data:
          path === '/api/v1/items/{id}'
            ? { item, files: [{ id: 'file', available: true }], cast: [] }
            : {
                items:
                  path === '/api/v1/items' &&
                  (options as { params?: { query?: { resume?: boolean } } } | undefined)?.params
                    ?.query?.resume
                    ? [item]
                    : [],
                hasMore: false,
                nextCursor: '',
              },
        response: new Response(),
      }) as never,
  );
  render(
    <MantineProvider env="test">
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <Discover />
        </MemoryRouter>
      </QueryClientProvider>
    </MantineProvider>,
  );
  return get;
}

it('shows episode progress and resumes the episode file from Home', async () => {
  const play = vi.spyOn(useNativePlayer.getState(), 'play').mockImplementation(() => {});
  const get = await show();
  expect(await screen.findByRole('link', { name: episode.title })).toHaveAttribute(
    'href',
    '/item/episode',
  );
  expect(screen.getByText('Continue at 5:00')).toBeVisible();
  expect(screen.getByRole('progressbar', { name: 'Watch progress' })).toHaveAttribute(
    'aria-valuenow',
    '25',
  );
  fireEvent.click(screen.getByRole('button', { name: 'Resume Episode in progress' }));
  await vi.waitFor(() =>
    expect(play).toHaveBeenCalledWith(expect.objectContaining({ item: episode }), 'file'),
  );
  expect(get).toHaveBeenCalledWith(
    '/api/v1/items',
    expect.objectContaining({ params: { query: { resume: true, limit: 8 } } }),
  );
});

it('localizes progress and clamps positions beyond the duration', async () => {
  await show({ ...episode, position: 1500 }, 'vi');
  expect(await screen.findByRole('progressbar', { name: 'Tiến độ xem' })).toHaveAttribute(
    'aria-valuenow',
    '100',
  );
  expect(screen.getByText('Tiếp tục câu chuyện')).toBeVisible();
});

it('handles unavailable duration without an invalid progress value', async () => {
  await show({ ...episode, duration: 0 });
  expect(await screen.findByRole('progressbar', { name: 'Watch progress' })).toHaveAttribute(
    'aria-valuenow',
    '0',
  );
});
