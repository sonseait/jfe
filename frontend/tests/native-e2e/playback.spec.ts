import { expect, test } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { mkdir, rm, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

test('source-aware MKV uses fMP4 copy, external subtitles and bounded ranges', async ({
  page,
  request,
}, info) => {
  test.setTimeout(45000);
  const root = join(process.env.JFE_TEST_MEDIA_ROOT!, `Playback-${info.project.name}`);
  await mkdir(root, { recursive: true });
  const name = 'Preserved Spectrum (2026)';
  execFileSync('ffmpeg', [
    '-v',
    'error',
    '-i',
    join(process.env.JFE_TEST_MEDIA_ROOT!, 'Quiet Horizon (2025).mp4'),
    '-c',
    'copy',
    join(root, name + '.mkv'),
  ]);
  await writeFile(
    join(root, name + '.en.srt'),
    '1\n00:00:00,000 --> 00:00:30,000\nExternal source subtitle\n',
  );
  const system = await (await request.get('/api/v1/system')).json();
  const login = await (
    await request.post(system.setupRequired ? '/api/v1/auth/setup' : '/api/v1/auth/login', {
      data: { username: 'native-admin', password: 'native-testing-password' },
    })
  ).json();
  const headers = { Authorization: `Bearer ${login.token}` };
  const library = await (
    await request.post('/api/v1/libraries', {
      headers,
      data: {
        name: `Playback ${info.project.name}`,
        kind: 'movies',
        paths: [root],
        scanIntervalHours: 0,
      },
    })
  ).json();
  try {
    await request.post(`/api/v1/libraries/${library.id}/scan`, { headers });
    let itemId = '';
    await expect
      .poll(
        async () => {
          const items = await (
            await request.get(`/api/v1/items?libraryId=${library.id}`, { headers })
          ).json();
          itemId = items.items[0]?.id ?? '';
          return items.items.length;
        },
        { timeout: 15000 },
      )
      .toBe(1);
    await page.addInitScript((session) => {
      localStorage.setItem('jfe.language', 'en');
      localStorage.setItem('jfe.native-session', JSON.stringify({ state: session, version: 0 }));
    }, login);
    await page.goto(`/#/item/${itemId}`);
    const start = page.waitForResponse(
      (response) =>
        response.url().endsWith('/api/v1/playback') && response.request().method() === 'POST',
    );
    const fragment = page.waitForResponse(
      (response) => response.url().includes('/stream/stream.mp4') && response.status() === 206,
    );
    await page.getByRole('button', { name: 'Play now', exact: true }).click();
    const session = await (await start).json();
    expect(session.protocol).toBe('mp4');
    expect(session.decision).toMatchObject({
      mode: 'remux',
      videoAction: 'copy',
      audioAction: 'copy',
      subtitleAction: 'external',
    });
    const response = await fragment;
    expect(response.request().headers().range).toMatch(/^bytes=\d+-\d+$/);
    expect(response.headers()['content-range']).toMatch(/^bytes \d+-\d+\/\d+$/);
    const video = page.locator('.player video');
    await expect
      .poll(() => video.evaluate((el) => (el as HTMLVideoElement).currentTime), { timeout: 15000 })
      .toBeGreaterThan(1);
    await expect(page.locator('.player-subtitle-overlay')).toHaveText('External source subtitle');
    expect(await video.evaluate((el) => (el as HTMLVideoElement).currentSrc)).toMatch(/^blob:/);
    await expect(page.locator('.playback-indicator')).toHaveAttribute('data-method', 'remux');
    let restarts = 0;
    page.on('request', (req) => {
      if (req.url().endsWith('/api/v1/playback') && req.method() === 'POST') restarts++;
    });
    await page.getByRole('slider', { name: 'Playback position', exact: true }).press('ArrowRight');
    await expect(video).toHaveJSProperty('seeking', false);
    expect(restarts).toBe(0);
    const stopped = page.waitForResponse(
      (res) =>
        res.url().endsWith(`/api/v1/playback/${session.id}`) && res.request().method() === 'DELETE',
    );
    await page.getByRole('button', { name: 'Stop playback', exact: true }).click();
    await stopped;
    const expired = await request.get(`${session.url}?token=${session.streamToken}`, {
      headers: { Range: 'bytes=0-31' },
    });
    expect(expired.status()).toBe(403);
  } finally {
    await request.delete(`/api/v1/libraries/${library.id}`, { headers });
    await rm(root, { recursive: true, force: true });
  }
});
