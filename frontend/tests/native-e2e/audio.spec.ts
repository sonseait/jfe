import { expect, test } from '@playwright/test';
import { copyFile, mkdir } from 'node:fs/promises';
import { join } from 'node:path';

test('audio scan, tags written to disk, persistent player and import tasks', async ({
  page,
  request,
}, info) => {
  test.setTimeout(60000);
  const system = await (await request.get('/api/v1/system')).json();
  const loginResponse = await request.post(
    system.setupRequired ? '/api/v1/auth/setup' : '/api/v1/auth/login',
    { data: { username: 'native-admin', password: 'native-testing-password' } },
  );
  expect(loginResponse.ok()).toBeTruthy();
  const login = await loginResponse.json();
  const headers = { Authorization: `Bearer ${login.token}` };
  const root = join(process.env.JFE_TEST_MEDIA_ROOT!, `Audio-${info.project.name}`);
  await mkdir(root, { recursive: true });
  await copyFile(join(process.env.JFE_TEST_MEDIA_ROOT!, 'Music/01.flac'), join(root, '01.flac'));
  const library = await (
    await request.post('/api/v1/libraries', {
      headers,
      data: {
        name: `Audio ${info.project.name}`,
        kind: 'music',
        paths: [root],
        scanIntervalHours: 0,
      },
    })
  ).json();
  try {
    await request.post(`/api/v1/libraries/${library.id}/scan`, { headers });
    let albumId = '';
    await expect
      .poll(async () => {
        const response = await (
          await request.get(`/api/v1/items?libraryId=${library.id}`, { headers })
        ).json();
        albumId = response.items[0]?.id ?? '';
        return response.items.length;
      })
      .toBe(1);
    let album = await (await request.get(`/api/v1/items/${albumId}`, { headers })).json();
    await expect
      .poll(async () => {
        album = await (await request.get(`/api/v1/items/${albumId}`, { headers })).json();
        return album.children?.length ?? 0;
      })
      .toBe(1);
    const trackId = album.children[0].id;
    await page.addInitScript((value) => {
      localStorage.setItem('jfe.language', 'en');
      localStorage.setItem('jfe.native-session', JSON.stringify({ state: value, version: 0 }));
    }, login);
    await page.goto(`/#/item/${albumId}`);
    await expect(page.getByRole('heading', { name: 'Test Album', exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Edit file tags', exact: true }).click();
    await expect(page.getByRole('textbox', { name: 'Title', exact: true })).toHaveValue(
      'Audio Fixture',
    );
    await page
      .getByRole('textbox', { name: 'Title', exact: true })
      .fill(`Edited ${info.project.name}`);
    await page.getByRole('button', { name: 'Review changes', exact: true }).click();
    await page.getByRole('button', { name: 'Write tags to files', exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect
      .poll(async () => {
        const detail = await (await request.get(`/api/v1/items/${trackId}`, { headers })).json();
        return detail.item.title;
      })
      .toBe(`Edited ${info.project.name}`);
    // A fresh scan must read the persisted file edit, without creating another track.
    await request.post(`/api/v1/libraries/${library.id}/scan`, { headers });
    await page.goto(`/#/item/${trackId}`);
    await expect(
      page.getByRole('heading', { name: `Edited ${info.project.name}`, exact: true }),
    ).toBeVisible();
    await page.locator('.audio-hero').getByRole('button', { name: 'Listen', exact: true }).click();
    await expect(page.locator('.audio-dock')).toBeVisible();
    await expect
      .poll(() =>
        page.locator('.audio-dock audio').evaluate((a: HTMLAudioElement) => a.currentTime),
      )
      .toBeGreaterThan(0);
    await page
      .locator('.audio-page')
      .getByRole('link', { name: 'Your libraries', exact: true })
      .click();
    await expect(page.locator('.audio-dock')).toBeVisible();
    await page
      .locator('.audio-dock')
      .getByRole('button', { name: 'Listening queue', exact: true })
      .click();
    await expect(page.getByRole('combobox', { name: 'Sleep timer', exact: true })).toBeVisible();
    await page.keyboard.press('Escape');
    expect(await page.locator('body').evaluate((el) => el.scrollWidth)).toBe(
      await page.evaluate(() => innerWidth),
    );
    await page.screenshot({
      path: `test-results/${info.project.name}-audio-player.png`,
      fullPage: true,
    });
    await page.locator('.audio-dock').getByRole('button', { name: 'Close', exact: true }).click();
    await page.goto('/#/imports');
    await expect(page.getByRole('heading', { name: 'Import audio', exact: true })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Audio tasks', exact: true })).toBeVisible();
    await expect(page.getByText('Write file tags', { exact: true }).first()).toBeVisible();
    await expect(page.getByText('Completed', { exact: true }).first()).toBeVisible();
    await page.screenshot({
      path: `test-results/${info.project.name}-audio-imports.png`,
      fullPage: true,
    });
  } finally {
    expect(
      (await request.delete(`/api/v1/libraries/${library.id}`, { headers })).ok(),
    ).toBeTruthy();
  }
});
