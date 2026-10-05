import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MetadataEditor } from '../src/native/Admin';
import MetadataRefresh from '../src/native/MetadataRefresh';
import { api, type DTO } from '../src/native/api';
import i18n from '../src/locales';

const clients: QueryClient[] = [];
function show(providerId = '42', editor = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  const item = {
    id: 'show-id',
    kind: 'series',
    title: 'A show',
    year: 2024,
    overview: '',
    metadataLocked: false,
    providerId,
  } as DTO<'ItemDTO'>;
  render(
    <MantineProvider env="test">
      <QueryClientProvider client={client}>
        {editor ? (
          <MetadataEditor item={item} opened close={() => {}} />
        ) : (
          <MetadataRefresh item={item} />
        )}
      </QueryClientProvider>
    </MantineProvider>,
  );
}
beforeEach(async () => {
  await i18n.changeLanguage('en');
  Object.defineProperty(document, 'fonts', { configurable: true, value: new EventTarget() });
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
  clients.splice(0).forEach((client) => client.clear());
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it('offers exactly two modes and sends missing mode by default', async () => {
  const post = vi
    .spyOn(api, 'POST')
    .mockResolvedValue({ data: {}, response: new Response() } as never);
  show();
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Refresh metadata' }));
  expect(screen.queryByRole('button', { name: 'Save changes' })).not.toBeInTheDocument();
  expect(screen.getAllByRole('radio')).toHaveLength(2);
  expect(screen.getByRole('radio', { name: 'Only episodes missing metadata' })).toBeChecked();
  fireEvent.click(
    within(screen.getByRole('dialog')).getByRole('button', { name: 'Refresh metadata' }),
  );
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/v1/items/{id}/metadata/refresh', {
      params: { path: { id: 'show-id' } },
      body: { mode: 'missing' },
    }),
  );
});

it('requires confirmation before replacing all metadata', async () => {
  const post = vi
    .spyOn(api, 'POST')
    .mockResolvedValue({ data: {}, response: new Response() } as never);
  show();
  fireEvent.click(screen.getByRole('button', { name: 'Refresh metadata' }));
  fireEvent.click(screen.getByRole('radio', { name: 'Replace all metadata' }));
  expect(post).not.toHaveBeenCalled();
  const confirmation = await screen.findByRole('dialog', { name: 'Refresh metadata' });
  fireEvent.click(within(confirmation).getByRole('button', { name: 'Replace all metadata' }));
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/v1/items/{id}/metadata/refresh', {
      params: { path: { id: 'show-id' } },
      body: { mode: 'replace' },
    }),
  );
});

it('disables missing mode until the series is identified and translates the options', async () => {
  await i18n.changeLanguage('vi');
  show('');
  fireEvent.click(screen.getByRole('button', { name: 'Cập nhật metadata' }));
  expect(
    screen.getByRole('radio', { name: 'Chỉ cập nhật các tập còn thiếu metadata' }),
  ).toBeDisabled();
  expect(screen.getByRole('radio', { name: 'Thay thế toàn bộ metadata' })).toBeChecked();
});

it('keeps refresh controls out of the manual metadata editor', () => {
  show('42', true);
  expect(screen.getByRole('button', { name: 'Save changes' })).toBeVisible();
  expect(screen.queryByRole('button', { name: 'Refresh metadata' })).not.toBeInTheDocument();
  expect(screen.queryByRole('radio')).not.toBeInTheDocument();
});
