import { expect, test } from '@playwright/test';
import { copyFile, mkdir, rm, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

test('native setup/login, scan, playback, fullscreen, resume and admin', async ({ page }, info) => {
  test.setTimeout(90000);
  await page.addInitScript(() => localStorage.setItem('jfe.language', 'en'));
  await page.goto('/');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('native-admin');
  await page.getByLabel(/^Password/).fill('native-testing-password');
  await page.locator('form').getByRole('button').last().click();
  await expect(page.locator('.sidebar')).toBeAttached();
  await page.goto('/#/admin/libraries');
  await page
    .locator('.library-management-heading')
    .getByRole('button', { name: 'Add library', exact: true })
    .click();
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill(`Films ${info.project.name}`);
  await page
    .getByRole('combobox', { name: 'Media folders', exact: true })
    .fill(process.env.JFE_TEST_MEDIA_ROOT!);
  await page.getByRole('combobox', { name: 'Media folders', exact: true }).press('Enter');
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  const library = page.locator('.admin-library').filter({ hasText: `Films ${info.project.name}` });
  await library.getByRole('checkbox', { name: 'Force metadata refresh', exact: true }).check();
  const scanRequest = page.waitForRequest(
    (request) => request.method() === 'POST' && request.url().includes('/scan?forceMetadata=true'),
  );
  await library.getByRole('button', { name: 'Scan libraries', exact: true }).click();
  await scanRequest;
  await page.goto('/#/admin/jobs');
  await expect(page.getByText('Completed', { exact: true }).first()).toBeVisible({
    timeout: 20000,
  });
  await page.goto('/#/admin/libraries');
  await expect(library.getByText('1 media files', { exact: true })).toBeVisible();
  await expect(library.getByText(/1\/1 · 100%/)).toBeVisible();
  await page.getByRole('textbox', { name: 'Find a library', exact: true }).fill('missing-library');
  await expect(page.locator('.managed-library')).toHaveCount(0);
  await page.getByRole('textbox', { name: 'Find a library', exact: true }).fill('');
  await library.getByRole('button', { name: 'Settings', exact: true }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toHaveValue(
    `Films ${info.project.name}`,
  );
  await page
    .getByRole('textbox', { name: 'Scan interval (hours, 0 for manual)', exact: true })
    .fill('12');
  await expect
    .poll(() =>
      page
        .locator('.library-editor-modal .mantine-ScrollArea-viewport')
        .evaluate((el) => el.scrollWidth <= el.clientWidth + 1),
    )
    .toBe(true);
  await page.screenshot({
    path: `test-results/${info.project.name}-library-editor.png`,
    animations: 'disabled',
  });
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(library.getByText('Every 12 hours', { exact: true })).toBeVisible();
  expect(await page.locator('body').evaluate((el) => el.scrollWidth)).toBe(
    await page.evaluate(() => innerWidth),
  );
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.screenshot({
    path: `test-results/${info.project.name}-admin-libraries.png`,
    fullPage: true,
    animations: 'disabled',
  });

  await page.goto('/#/');
  await expect(
    page.locator('.discover-spotlight').getByRole('heading', { name: 'Quiet Horizon' }),
  ).toBeVisible();
  await expect(
    page.locator('.discover-spotlight').getByRole('button', { name: /^(Play now|Resume)$/ }),
  ).toBeVisible();
  await expect(
    page.locator('.discover-section').getByRole('heading', { name: 'A film for every mood' }),
  ).toBeVisible();
  expect(await page.locator('body').evaluate((el) => el.scrollWidth)).toBe(
    await page.evaluate(() => innerWidth),
  );
  await page.screenshot({
    path: `test-results/${info.project.name}-discover.png`,
    fullPage: true,
    animations: 'disabled',
  });
  await page
    .locator('.discover-spotlight')
    .getByRole('link', { name: 'View details', exact: true })
    .click();
  await expect(
    page.getByRole('heading', { name: 'Quiet Horizon', exact: true, level: 1 }),
  ).toBeVisible();
  const actorCard = page.getByRole('button', { name: 'Test Actor Lead' });
  await expect(actorCard.locator('img')).toBeVisible();
  await expect
    .poll(() =>
      actorCard.locator('img').evaluate((image) => (image as HTMLImageElement).naturalWidth),
    )
    .toBeGreaterThan(0);
  await expect(actorCard.locator('strong')).toHaveCSS('white-space', 'nowrap');
  await expect(actorCard.locator('strong')).toHaveCSS('font-size', '12px');
  const longName = page
    .getByRole('button', { name: 'An Actor With A Very Long Display Name Supporting' })
    .locator('strong');
  expect(await longName.evaluate((el) => el.scrollWidth > el.clientWidth)).toBe(true);
  await expect(longName).toHaveAttribute('title', 'An Actor With A Very Long Display Name');
  await page.screenshot({
    path: `test-results/${info.project.name}-cast-portrait.png`,
    fullPage: true,
    animations: 'disabled',
  });
  await actorCard.click();
  const castDialog = page.getByRole('dialog', { name: 'Test Actor', exact: true });
  await expect(castDialog.getByText('Featuring this actor in your libraries')).toBeVisible();
  await expect(castDialog.getByRole('link', { name: /Quiet Horizon/ }).first()).toBeVisible();
  expect(await page.locator('body').evaluate((el) => el.scrollWidth)).toBe(
    await page.evaluate(() => innerWidth),
  );
  await page.screenshot({
    path: `test-results/${info.project.name}-cast.png`,
    animations: 'disabled',
  });
  await castDialog
    .getByRole('link', { name: /Quiet Horizon/ })
    .first()
    .click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await page.goto('/#/libraries');
  await expect(page.locator('.archive-tile')).toHaveCount(info.project.name === 'chromium' ? 1 : 2);
  await page.screenshot({
    path: `test-results/${info.project.name}-libraries.png`,
    fullPage: true,
    animations: 'disabled',
  });
  await page.getByRole('link', { name: `Films ${info.project.name}`, exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Quiet Horizon', exact: true })).toBeVisible();
  await page
    .locator('.mantine-SegmentedControl-label')
    .filter({ hasText: 'Group by title' })
    .click();
  const group = page.locator('.title-group-heading').first();
  await expect(group).toHaveAttribute('aria-expanded', 'true');
  await group.click();
  await expect(page.locator('.poster').first()).toBeHidden();
  await page.getByRole('button', { name: 'Expand all', exact: true }).click();
  await expect(page.locator('.poster').first()).toBeVisible();
  await page.screenshot({
    path: `test-results/${info.project.name}-grouped-library.png`,
    fullPage: true,
    animations: 'disabled',
  });
  expect(await page.locator('body').evaluate((el) => el.scrollWidth)).toBe(
    await page.evaluate(() => innerWidth),
  );
  await page.locator('.poster').first().click();
  await page.getByRole('button', { name: 'Edit metadata', exact: true }).click();
  const metadataDialog = page.getByRole('dialog');
  await expect(metadataDialog.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue(
    'Quiet Horizon',
  );
  await metadataDialog
    .getByRole('textbox', { name: 'The story', exact: true })
    .fill('A quiet journey through a changing world.');
  await page.screenshot({
    path: `test-results/${info.project.name}-metadata-editor.png`,
    fullPage: true,
    animations: 'disabled',
  });
  // Provider candidates are isolated to this UI check; worker matching is tested against PostgreSQL separately.
  await page.route('**/api/v1/metadata/search?*', (route) =>
    route.fulfill({
      json: {
        search: 'Quiet Horizon',
        year: 2025,
        items: [
          {
            id: 101,
            title: 'Quiet Horizon',
            year: '2025-01-01',
            overview: 'A quiet journey.',
            score: 100,
            recommended: true,
          },
          {
            id: 102,
            title: 'Quiet Horizon',
            year: '1980-01-01',
            overview: 'An earlier film.',
            score: 60,
            recommended: false,
          },
        ],
      },
    }),
  );
  await metadataDialog.getByRole('tab', { name: 'Find a match', exact: true }).click();
  await expect(
    metadataDialog.getByRole('textbox', { name: 'Title or filename', exact: true }),
  ).toHaveValue('Quiet Horizon (2025).mp4');
  await expect(metadataDialog.locator('.metadata-match').first()).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await metadataDialog.locator('.metadata-match').nth(1).click();
  await expect(metadataDialog.locator('.metadata-match').nth(1)).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await page.screenshot({
    path: `test-results/${info.project.name}-metadata-matching.png`,
    fullPage: true,
    animations: 'disabled',
  });
  await metadataDialog.getByRole('tab', { name: 'Edit details', exact: true }).click();
  await page.unroute('**/api/v1/metadata/search?*');
  await metadataDialog.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(metadataDialog).toHaveCount(0);
  await expect(page.locator('.film-synopsis')).toContainText(
    'A quiet journey through a changing world.',
  );
  await page.getByRole('button', { name: 'Add to favorites', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Synopsis', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Picture & sound', exact: true })).toBeVisible();
  await expect(page.locator('.film-specs')).toContainText('320 × 180');
  await expect(page.locator('.film-track-group')).toContainText(['h264', 'aac', 'No tracks found']);
  expect(await page.locator('body').evaluate((el) => el.scrollWidth)).toBe(
    await page.evaluate(() => innerWidth),
  );
  await page.screenshot({
    path: `test-results/${info.project.name}-film-detail.png`,
    fullPage: true,
    animations: 'disabled',
  });
  await page.getByRole('button', { name: 'Play now', exact: true }).click();
  const player = page.locator('.player');
  await expect(player.locator('video')).toHaveJSProperty('paused', false, { timeout: 20000 });
  await expect
    .poll(() => player.locator('video').evaluate((el) => (el as HTMLVideoElement).currentTime), {
      timeout: 15000,
    })
    .toBeGreaterThan(1);
  await expect(player.locator('.playback-indicator')).toHaveAttribute('data-method', 'direct');
  await player.getByRole('button', { name: 'Playback stream', exact: true }).click();
  await expect(player.locator('.playback-stream-details')).toContainText('Video bitrate');
  await expect(player.locator('.playback-stream-details')).toContainText('Mbps');
  await player.getByRole('button', { name: 'Playback stream', exact: true }).click();
  // Exercise recovery when the browser rejects a direct-play source.
  const fallback = page.waitForResponse(
    (r) => r.url().endsWith('/api/v1/playback') && r.request().method() === 'POST',
  );
  await player.locator('video').evaluate((el) => el.dispatchEvent(new Event('error')));
  expect((await (await fallback).json()).method).toBe('remux');
  await expect(player.locator('video')).toHaveJSProperty('paused', false, { timeout: 30000 });
  await expect(player.locator('.player-error')).toHaveCount(0);
  await expect(player.locator('.playback-indicator')).toHaveAttribute('data-method', 'remux');
  await expect(player.locator('.playback-indicator')).toContainText('320 × 180');
  await expect
    .poll(async () =>
      Number(await player.locator('.player-seek').getAttribute('data-buffered-ranges')),
    )
    .toBeGreaterThan(0);
  await expect(player.locator('.player-seek')).toHaveAttribute('style', /linear-gradient/);
  await player.locator('video').evaluate((el) => el.dispatchEvent(new Event('waiting')));
  await expect(player.locator('.player-download-rate')).toContainText(/(?:B|KiB|MiB|GiB)\/s/);
  await player.locator('video').evaluate((el) => el.dispatchEvent(new Event('playing')));

  await expect(player.getByRole('button', { name: /^Audio:/ })).toBeVisible();
  await expect(player.getByRole('button', { name: /^Speed:/ })).toBeVisible();
  await expect(player.getByRole('button', { name: /^Quality:/ })).toBeDisabled();
  await expect(player.getByRole('button', { name: /^Subtitles:/ })).toBeEnabled();
  const optionRows = await player
    .locator('.playback-option')
    .evaluateAll((buttons) =>
      buttons.map((button) => Math.round(button.getBoundingClientRect().top)),
    );
  expect(new Set(optionRows).size).toBe(1);
  const playBounds = await player.locator('.play-toggle').boundingBox();
  const optionBounds = await player.locator('.playback-option').first().boundingBox();
  expect(
    Math.abs(optionBounds!.y + optionBounds!.height / 2 - (playBounds!.y + playBounds!.height / 2)),
  ).toBeLessThan(10);
  await player.getByRole('button', { name: /^Speed:/ }).click();
  await player.getByRole('menuitemradio', { name: '1.5x', exact: true }).click();
  await expect(player.locator('video')).toHaveJSProperty('playbackRate', 1.5);
  await player.getByRole('button', { name: /^Speed:/ }).click();
  await player.getByRole('menuitemradio', { name: '1x', exact: true }).click();
  await player.getByRole('button', { name: /^Subtitles:/ }).click();
  const stageBeforeUpload = await player.locator('.media-stage').boundingBox();
  const chooserPromise = page.waitForEvent('filechooser');
  await player.getByRole('menuitem', { name: 'Upload subtitles', exact: true }).click();
  const chooser = await chooserPromise;
  await chooser.setFiles({
    name: 'personal.en.srt',
    mimeType: 'application/x-subrip',
    buffer: Buffer.from('1\n00:00:00,000 --> 00:00:30,000\nSaved subtitle sample\n'),
  });
  await expect(player.locator('.player-subtitle-overlay')).toHaveText('Saved subtitle sample');
  await expect(
    player.getByRole('menuitemradio', { name: 'personal.en.srt', exact: true }),
  ).toHaveAttribute('aria-checked', 'true');
  await player.getByRole('button', { name: /^Subtitles:/ }).click();
  const stageBeforeTiming = await player.locator('.media-stage').boundingBox();
  expect(stageBeforeTiming!.height).toBe(stageBeforeUpload!.height);
  await player.getByRole('button', { name: 'Subtitle timing', exact: true }).click();
  const timing = player.getByRole('textbox', { name: 'Subtitle delay (seconds)', exact: true });
  await timing.fill('600');
  await expect(player.locator('.player-subtitle-overlay')).toHaveCount(0);
  await player.getByRole('button', { name: 'Reset timing', exact: true }).click();
  await expect(player.locator('.player-subtitle-overlay')).toHaveText('Saved subtitle sample');
  expect((await player.locator('.media-stage').boundingBox())!.height).toBe(
    stageBeforeTiming!.height,
  );
  await timing.press('Escape');
  await expect(timing).not.toBeVisible();
  let seekSessions = 0;
  await page.route('**/api/v1/playback', async (route) => {
    if (route.request().method() === 'POST') {
      seekSessions++;
      await new Promise((resolve) => setTimeout(resolve, 1200));
    }
    await route.continue();
  });
  const streamBeforeSeek = await player
    .locator('video')
    .evaluate((el) => (el as HTMLVideoElement).currentSrc);
  await player.getByRole('slider', { name: 'Playback position', exact: true }).focus();
  await player.getByRole('slider', { name: 'Playback position', exact: true }).press('ArrowRight');
  await expect(player.locator('video')).toHaveJSProperty('seeking', false);
  expect(seekSessions).toBe(0);
  await expect(player.locator('video')).toHaveJSProperty('currentSrc', streamBeforeSeek);
  // Before the current session's start there are no reusable segments.
  await player.getByRole('slider', { name: 'Playback position', exact: true }).press('Home');
  await expect.poll(() => seekSessions).toBe(1);
  await expect(player.locator('.native-player-loading')).toBeVisible();
  await expect(player.locator('.player-frozen-frame')).toHaveJSProperty('hidden', false);
  const loadingBounds = await player.locator('.native-player-loading').boundingBox();
  expect(loadingBounds!.height).toBeLessThan(70);
  await page.screenshot({
    path: `test-results/${info.project.name}-player-seeking.png`,
    animations: 'disabled',
  });
  await expect(player.locator('video')).toHaveJSProperty('paused', false, { timeout: 30000 });
  await page.unroute('**/api/v1/playback');

  await expect
    .poll(() => player.locator('video').evaluate((el) => (el as HTMLVideoElement).currentTime), {
      timeout: 15000,
    })
    .toBeGreaterThan(0.5);
  await expect(player.locator('.player-error')).toHaveCount(0);
  await expect(player.getByRole('button', { name: /^Audio:/ })).toBeVisible();
  await expect(player.getByRole('button', { name: /^Speed:/ })).toBeVisible();
  await player.getByRole('button', { name: 'Minimize player', exact: true }).click();
  await player.getByRole('button', { name: 'Fullscreen', exact: true }).click();
  await expect.poll(() => player.evaluate((el) => document.fullscreenElement === el)).toBe(true);
  const bounds = await player.boundingBox();
  expect(bounds?.width).toBe(await page.evaluate(() => innerWidth));
  await player.getByRole('button', { name: /^Speed:/ }).click();
  await expect(player.getByRole('menuitemradio', { name: '1x', exact: true })).toBeVisible();
  await player.getByRole('menuitemradio', { name: '1x', exact: true }).click();
  await player.getByRole('button', { name: 'Subtitle timing', exact: true }).click();
  await expect(timing).toBeVisible();
  await timing.press('Escape');
  await player.getByRole('button', { name: 'Exit fullscreen', exact: true }).click();
  await page.goto('/#/favorites');
  await expect(player).toBeVisible();
  await player.getByRole('button', { name: 'Stop playback', exact: true }).click();
  await expect(player).toHaveCount(0);
  await page.locator('.poster').first().click();
  await page.getByRole('button', { name: /^(Play now|Resume)/ }).click();
  await expect(player.locator('video')).toHaveJSProperty('paused', false);
  await expect
    .poll(() => player.locator('video').evaluate((el) => (el as HTMLVideoElement).currentTime), {
      timeout: 15000,
    })
    .toBeGreaterThan(1);
  await player.getByRole('button', { name: 'Stop playback', exact: true }).click();
  await page.goto('/#/');
  await expect(page.locator('.discover-resume').first()).toBeVisible();
  await page
    .locator('.discover-resume')
    .first()
    .getByRole('button', { name: 'Resume Quiet Horizon', exact: true })
    .click();
  await expect(player.locator('video')).toHaveJSProperty('paused', false, { timeout: 20000 });
  await player.getByRole('button', { name: 'Stop playback', exact: true }).click();
  await page.goto('/#/admin/jobs');
  await expect(page.locator('.task-target').first()).toBeVisible();
  await expect(page.locator('.task-facts').first()).toContainText('Attempts');
  await page.screenshot({ path: `test-results/${info.project.name}-tasks.png`, fullPage: true });
  await page.goto('/#/admin/users');
  await page.getByRole('button', { name: 'Add user', exact: true }).click();
  const userDialog = page.getByRole('dialog');
  await userDialog
    .getByRole('textbox', { name: 'Username', exact: true })
    .fill(`viewer-${info.project.name}`);
  await userDialog.locator('input[type=password]').fill('viewer-testing-password');
  await userDialog.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(userDialog).toHaveCount(0);
  await page
    .getByRole('textbox', { name: 'Search users', exact: true })
    .fill(`viewer-${info.project.name}`);
  await expect(page.locator('.user-card')).toHaveCount(1);
  await expect(page.locator('.user-card')).toContainText('No libraries assigned');
  await page.screenshot({ path: `test-results/${info.project.name}-users.png`, fullPage: true });
  await page.goto('/#/admin/encoding');
  const mode = page.getByRole('combobox', { name: 'Video transcoding', exact: true });
  await expect(mode).toHaveValue('Disabled');
  await expect(
    page.getByRole('textbox', { name: 'NVENC quality (CQ)', exact: true }),
  ).toBeDisabled();
  await mode.click();
  await expect(page.getByRole('option')).toHaveCount(2);
  await page.getByRole('option', { name: 'NVIDIA NVENC', exact: true }).click();
  await expect(
    page.getByRole('textbox', { name: 'NVENC quality (CQ)', exact: true }),
  ).toBeEnabled();
  const saved = page.waitForResponse(
    (r) => r.url().endsWith('/api/v1/admin/encoding') && r.request().method() === 'PUT',
  );
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  expect((await saved).status()).toBe(200);
  await page.reload();
  await expect(mode).toHaveValue('NVIDIA NVENC');
  await mode.click();
  await page.getByRole('option', { name: 'Disabled', exact: true }).click();
  const disabled = page.waitForResponse(
    (r) => r.url().endsWith('/api/v1/admin/encoding') && r.request().method() === 'PUT',
  );
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  expect((await disabled).status()).toBe(200);
  await page.screenshot({
    path: `test-results/${info.project.name}-native-admin.png`,
    fullPage: true,
    animations: 'disabled',
  });
  const showsPath = join(process.env.JFE_TEST_MEDIA_ROOT!, `shows-${info.project.name}`);
  await mkdir(showsPath);
  const headers = {
    Authorization: `Bearer ${await page.evaluate(() => JSON.parse(localStorage.getItem('jfe.native-session')!).state.token)}`,
  };
  let showsId = '';
  try {
    for (const name of [
      'Quiet Series/S01/ep1.mp4',
      'Quiet Series/Session 01/Different Name S01E02.mp4',
      'Other Series/Season 01/Quiet Series S01E01.mp4',
    ]) {
      await mkdir(join(showsPath, name, '..'), { recursive: true });
      await copyFile(
        join(process.env.JFE_TEST_MEDIA_ROOT!, 'Quiet Horizon (2025).mp4'),
        join(showsPath, name),
      );
    }
    await writeFile(
      join(showsPath, 'Quiet Series/tvshow.nfo'),
      `<tvshow><title>Quiet Series</title>${Array.from({ length: 12 }, (_, i) => `<actor><name>Series Actor ${i + 1}</name><role>Character ${i + 1}</role></actor>`).join('')}</tvshow>`,
    );
    const created = await page.request.post('/api/v1/libraries', {
      headers,
      data: {
        name: `Shows ${info.project.name}`,
        kind: 'series',
        paths: [showsPath],
        scanIntervalHours: 0,
      },
    });
    expect(created.ok()).toBe(true);
    showsId = (await created.json()).id;
    expect((await page.request.post(`/api/v1/libraries/${showsId}/scan`, { headers })).ok()).toBe(
      true,
    );
    await expect
      .poll(
        async () => {
          const response = await page.request.get(
            `/api/v1/items?libraryId=${showsId}&kind=episode`,
            { headers },
          );
          return (await response.json()).items.length;
        },
        { timeout: 20000 },
      )
      .toBe(3);
    // A stale episode filter in the URL must not flatten a series library.
    await page.goto(`/#/library/${showsId}?kind=episode`);
    await expect(page.locator('.poster')).toHaveCount(2);
    await expect(page.locator('.poster').filter({ hasText: 'Quiet Series' })).toHaveCount(1);
    await page.locator('.poster').filter({ hasText: 'Quiet Series' }).click();
    await expect(page.locator('.poster')).toHaveCount(2);
    await expect(page.locator('.poster').first()).toContainText('S1');
    await expect(page.getByRole('heading', { name: 'Episodes', exact: true })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Season 1', exact: true })).toBeVisible();
    await expect(page.locator('.film-cast-person')).toHaveCount(12);
    const detailBody = await page.locator('.film-detail-body').boundingBox();
    const synopsis = await page.locator('.film-synopsis').boundingBox();
    expect(Math.abs(detailBody!.width - synopsis!.width)).toBeLessThan(2);
    const cast = await page.locator('.film-cast').boundingBox();
    const episodes = await page.locator('.film-episodes').boundingBox();
    expect(episodes!.y - (cast!.y + cast!.height)).toBeGreaterThanOrEqual(40);
    expect(
      await page
        .locator('.film-cast .mantine-ScrollArea-viewport')
        .evaluate((el) => el.scrollWidth > el.clientWidth),
    ).toBe(true);
    expect(await page.locator('body').evaluate((el) => el.scrollWidth)).toBe(
      await page.evaluate(() => innerWidth),
    );
    await page.screenshot({
      path: `test-results/${info.project.name}-series-detail.png`,
      fullPage: true,
      animations: 'disabled',
    });
    const seriesURL = page.url();
    await page.locator('.film-episodes .poster').first().click();
    await page.reload();
    await expect(page.getByRole('link', { name: 'Back to series', exact: true })).toBeVisible();
    await expect(page.locator('.film-media-info')).toBeVisible();
    await page.getByRole('link', { name: 'Back to series', exact: true }).click();
    await expect(page).toHaveURL(seriesURL);
    await expect(page.getByRole('heading', { name: 'Episodes', exact: true })).toBeVisible();
    // Old episode-filter URLs must still show only movies and series in search.
    await page.goto('/#/search?q=Quiet&kind=episode');
    await expect(page.locator('.poster').filter({ hasText: 'Quiet Series' })).toHaveCount(1);
    await expect(page.locator('.poster').filter({ hasText: 'Quiet Horizon' })).toHaveCount(
      info.project.name === 'chromium' ? 1 : 2,
    );
    await expect(page.locator('.poster').filter({ hasText: /S01E/ })).toHaveCount(0);
    const filter = page.getByRole('combobox', { name: 'Filters', exact: true });
    await expect(filter).toHaveValue('All');
    await filter.click();
    await expect(page.getByRole('option', { name: 'Episodes', exact: true })).toHaveCount(0);
    await page.getByRole('option', { name: 'Series', exact: true }).click();
    await expect(page.locator('.poster')).toHaveCount(1);
    await page.locator('.poster').click();
    await expect(page.getByRole('heading', { name: 'Episodes', exact: true })).toBeVisible();
    await expect(page.locator('.film-episodes .poster')).toHaveCount(2);
  } finally {
    if (showsId) await page.request.delete(`/api/v1/libraries/${showsId}`, { headers });
    await rm(showsPath, { recursive: true, force: true });
  }
  const bufferPath = join(process.env.JFE_TEST_MEDIA_ROOT!, `.buffer-${info.project.name}`);
  await mkdir(bufferPath);
  let bufferLibrary = '';
  try {
    await copyFile(process.env.JFE_TEST_BUFFER_FIXTURE!, join(bufferPath, 'Buffer Test.mp4'));
    const created = await page.request.post('/api/v1/libraries', {
      headers,
      data: { name: 'Buffer test', kind: 'movies', paths: [bufferPath], scanIntervalHours: 0 },
    });
    expect(created.ok()).toBe(true);
    bufferLibrary = (await created.json()).id;
    expect(
      (await page.request.post(`/api/v1/libraries/${bufferLibrary}/scan`, { headers })).ok(),
    ).toBe(true);
    let bufferItem = '';
    await expect
      .poll(
        async () => {
          const response = await page.request.get(`/api/v1/items?libraryId=${bufferLibrary}`, {
            headers,
          });
          bufferItem = (await response.json()).items[0]?.id ?? '';
          return bufferItem;
        },
        { timeout: 20000 },
      )
      .not.toBe('');
    await page.goto(`/#/item/${bufferItem}`);
    await page.getByRole('button', { name: 'Play now', exact: true }).click();
    await expect(player.locator('video')).toHaveJSProperty('paused', false, { timeout: 20000 });
    const remux = page.waitForResponse(
      (response) =>
        response.url().endsWith('/api/v1/playback') && response.request().method() === 'POST',
    );
    await player.locator('video').evaluate((el) => el.dispatchEvent(new Event('error')));
    expect((await (await remux).json()).method).toBe('remux');
    // A fast local network should fill well beyond the initial 60-second target.
    await expect
      .poll(
        () =>
          player.locator('video').evaluate((el) => {
            const video = el as HTMLVideoElement;
            for (let i = 0; i < video.buffered.length; i++) {
              if (
                video.buffered.start(i) <= video.currentTime &&
                video.buffered.end(i) > video.currentTime
              ) {
                return video.buffered.end(i) - video.currentTime;
              }
            }
            return 0;
          }),
        { timeout: 30000 },
      )
      .toBeGreaterThan(120);
    await expect(player.locator('.player-error')).toHaveCount(0);
    await player.getByRole('button', { name: 'Stop playback', exact: true }).click();
  } finally {
    if (bufferLibrary) await page.request.delete(`/api/v1/libraries/${bufferLibrary}`, { headers });
    await rm(bufferPath, { recursive: true, force: true });
  }
  await page.goto('/#/admin/general');
  await expect(page.getByRole('heading', { name: 'General settings', exact: true })).toBeVisible();
  await expect(page.getByText('TMDB not configured', { exact: true })).toBeVisible();
  await expect(
    page.getByRole('checkbox', { name: 'Automatically identify new titles', exact: true }),
  ).toBeDisabled();
  const serverName = page.getByRole('textbox', { name: 'Server name', exact: true });
  const watchedAt = page.getByRole('textbox', { name: 'Mark watched at', exact: true });
  await serverName.fill(`Cinema ${info.project.name}`);
  await watchedAt.fill('80');
  const generalSaved = page.waitForResponse(
    (response) =>
      response.url().endsWith('/api/v1/admin/settings') && response.request().method() === 'PATCH',
  );
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  const savedGeneral = await generalSaved;
  expect(savedGeneral.status()).toBe(200);
  expect(savedGeneral.request().postDataJSON()).toEqual({
    serverName: `Cinema ${info.project.name}`,
    watchedPercent: 80,
  });
  await page.reload();
  await expect(serverName).toHaveValue(`Cinema ${info.project.name}`);
  await expect(watchedAt).toHaveValue('80%');
  await serverName.fill('Unsaved name');
  await page.getByRole('button', { name: 'Discard changes', exact: true }).click();
  await expect(serverName).toHaveValue(`Cinema ${info.project.name}`);
  await page.screenshot({
    path: `test-results/${info.project.name}-general-settings.png`,
    fullPage: true,
    animations: 'disabled',
  });
  expect(await page.locator('body').evaluate((el) => el.scrollWidth)).toBe(
    await page.evaluate(() => innerWidth),
  );
  await serverName.fill('JFE');
  await watchedAt.fill('95');
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  await page.goto('/#/settings/account');
  await expect(
    page.getByRole('button', { name: /reducedMotion|Reduce animations|Reduce motion/ }),
  ).toHaveCount(0);
  await page.getByRole('combobox', { name: 'Language', exact: true }).click();
  await page.getByRole('option', { name: 'Tiếng Việt', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Cài đặt', exact: true })).toBeVisible();
  expect(await page.locator('body').evaluate((el) => el.scrollWidth)).toBe(
    await page.evaluate(() => innerWidth),
  );
});
