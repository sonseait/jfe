import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import { QueryClientProvider } from '@tanstack/react-query';
import GeneralSettings from '../src/native/GeneralSettings';
import { api, type DTO } from '../src/native/api';
import { queryClient } from '../src/lib/query-client';
import i18n from '../src/locales';

const settings: DTO<'GeneralSettingsDTO'> = {
  serverName: 'JFE',
  autoMetadata: true,
  metadataLanguage: 'en-US',
  castImages: true,
  watchedPercent: 95,
  tmdbConfigured: true,
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
});
afterEach(() => {
  queryClient.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
function show(config = settings) {
  vi.spyOn(api, 'GET').mockResolvedValue({ data: config, response: new Response() } as never);
  render(
    <MantineProvider env="test">
      <QueryClientProvider client={queryClient}>
        <GeneralSettings />
      </QueryClientProvider>
    </MantineProvider>,
  );
}
it('sends only edited fields and allows discarding unsaved changes', async () => {
  const save = vi
    .spyOn(api, 'PATCH')
    .mockResolvedValue({ data: settings, response: new Response() } as never);
  show();
  const name = await screen.findByRole('textbox', { name: 'Server name' });
  fireEvent.change(name, { target: { value: 'Discard me' } });
  fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }));
  expect(name).toHaveValue('JFE');
  expect(save).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('checkbox', { name: 'Automatically identify new titles' }));
  fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));
  await waitFor(() =>
    expect(save).toHaveBeenCalledWith('/api/v1/admin/settings', { body: { autoMetadata: false } }),
  );
});
it('preserves edits on a failed save', async () => {
  vi.spyOn(api, 'PATCH').mockRejectedValue(new Error('offline'));
  show();
  const name = await screen.findByRole('textbox', { name: 'Server name' });
  fireEvent.change(name, { target: { value: 'Keep me' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Save changes' })).toBeEnabled());
  expect(name).toHaveValue('Keep me');
});
it('gates TMDB controls when credentials are not configured', async () => {
  show({ ...settings, tmdbConfigured: false });
  expect(await screen.findByText('TMDB not configured')).toBeVisible();
  expect(
    screen.getByRole('checkbox', { name: 'Automatically identify new titles' }),
  ).toBeDisabled();
  expect(
    screen.getByRole('checkbox', { name: 'Download actor portraits from TMDB' }),
  ).toBeDisabled();
  expect(screen.getByRole('combobox', { name: 'Metadata language' })).toBeDisabled();
  expect(screen.getByRole('textbox', { name: 'Server name' })).toBeEnabled();
});
