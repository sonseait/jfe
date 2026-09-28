import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, useLocation } from 'react-router-dom';
import Cast from '../src/native/Cast';
import { api, useAuth } from '../src/native/api';
import i18n from '../src/locales';

const actor = { id: 'actor-id', name: 'Test Actor', character: 'Lead', image: '' };
function Location() {
  return <span data-testid="location">{useLocation().pathname}</span>;
}
function show(members = [actor]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <MantineProvider env="test">
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <Cast members={members} />
          <Location />
        </MemoryRouter>
      </QueryClientProvider>
    </MantineProvider>,
  );
  return client;
}
beforeEach(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
});
afterEach(() => {
  useAuth.setState({ token: undefined });
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it('shows an authenticated portrait and falls back to initials on image failure', async () => {
  useAuth.setState({ token: 'test token&' });
  const client = show([{ ...actor, image: '/api/v1/items/item/cast/actor/image?token=' }]);
  const portrait = document.querySelector('.film-cast-person img');
  expect(portrait).toHaveAttribute(
    'src',
    '/api/v1/items/item/cast/actor/image?token=test%20token%26',
  );
  expect(screen.getByText('Test Actor')).toHaveAttribute('title', 'Test Actor');
  fireEvent.error(portrait!);
  expect(await screen.findByText('TA')).toBeInTheDocument();
  client.clear();
});

it('opens actor filmography, paginates and navigates to a film', async () => {
  await i18n.changeLanguage('en');
  const get = vi
    .spyOn(api, 'GET')
    .mockResolvedValueOnce({
      data: {
        items: [{ id: 'one', title: 'First Film', year: 2025, poster: '', kind: 'movie' }],
        hasMore: true,
        nextCursor: 'page-two',
      },
      response: new Response(),
    } as never)
    .mockResolvedValueOnce({
      data: {
        items: [{ id: 'two', title: 'Second Film', year: 2026, poster: '', kind: 'movie' }],
        hasMore: false,
        nextCursor: '',
      },
      response: new Response(),
    } as never);
  const client = show();
  expect(screen.getByText('Lead')).toBeVisible();
  expect(get).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Test Actor Lead' }));
  expect(await screen.findByRole('link', { name: /First Film/ })).toBeVisible();
  expect(get).toHaveBeenCalledWith(
    '/api/v1/items',
    expect.objectContaining({
      params: { query: { personId: 'actor-id', cursor: undefined, limit: 36 } },
    }),
  );
  fireEvent.click(screen.getByRole('button', { name: 'Load more' }));
  const second = await screen.findByRole('link', { name: /Second Film/ });
  expect(get).toHaveBeenLastCalledWith(
    '/api/v1/items',
    expect.objectContaining({
      params: { query: { personId: 'actor-id', cursor: 'page-two', limit: 36 } },
    }),
  );
  fireEvent.click(second);
  await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('/item/two'));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  client.clear();
});

it('shows a localized empty cast state without dead controls', async () => {
  await i18n.changeLanguage('vi');
  const get = vi.spyOn(api, 'GET');
  const client = show([]);
  expect(screen.getByText('Chưa có thông tin dàn diễn viên.')).toBeVisible();
  expect(screen.queryByRole('button')).not.toBeInTheDocument();
  expect(get).not.toHaveBeenCalled();
  client.clear();
});

it('handles an actor with no accessible titles', async () => {
  await i18n.changeLanguage('en');
  vi.spyOn(api, 'GET').mockResolvedValue({
    data: { items: [], hasMore: false, nextCursor: '' },
    response: new Response(),
  } as never);
  const client = show();
  fireEvent.click(screen.getByRole('button', { name: 'Test Actor Lead' }));
  expect(await screen.findByText('No titles available in your libraries.')).toBeVisible();
  client.clear();
});
