import { afterEach, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import { StreamIndicator } from '../src/native/StreamIndicator';
import i18n from '../src/locales';

afterEach(() => cleanup());
it.each(['en', 'vi'])('reports tone mapping from the worker output in %s', async (language) => {
  await i18n.changeLanguage(language);
  render(
    <MantineProvider>
      <StreamIndicator
        method="transcode"
        size={{ width: 1920, height: 1080 }}
        stream={{
          toneMapped: true,
          videoTranscoded: true,
          videoCodec: 'h264',
          audioCodec: 'aac',
          videoBitrate: 4000000,
          audioBitrate: 192000,
          totalBitrate: 4192000,
          bitrateSource: 'segment',
        }}
      />
    </MantineProvider>,
  );
  fireEvent.click(screen.getByRole('button', { name: i18n.t('playerStream.label') }));
  expect(await screen.findByText(i18n.t('playerStream.toneMapped'))).toBeInTheDocument();
});
it('does not label copied HDR as tone mapped', async () => {
  await i18n.changeLanguage('en');
  render(
    <MantineProvider>
      <StreamIndicator
        method="remux"
        size={{ width: 3840, height: 2160 }}
        stream={{
          videoTranscoded: false,
          videoCodec: 'hevc',
          audioCodec: 'aac',
          videoBitrate: 10000000,
          audioBitrate: 192000,
          totalBitrate: 10192000,
          bitrateSource: 'segment',
        }}
      />
    </MantineProvider>,
  );
  fireEvent.click(screen.getByRole('button', { name: i18n.t('playerStream.label') }));
  expect(screen.queryByText(i18n.t('playerStream.toneMapped'))).toBeNull();
});
