import { afterEach, describe, expect, it, vi } from 'vitest';
afterEach(() => vi.restoreAllMocks());
import {
  measurePlaybackRate,
  createAutoQuality,
  preferredSubtitle,
  selectAutoQuality,
} from '../src/native/playback-preferences';
import type { DTO } from '../src/native/api';

const track = (index: number, language: string, title = ''): DTO<'TrackDTO'> => ({
  index,
  language,
  title,
  type: 'subtitle',
  codec: 'subrip',
});
const file: DTO<'FileDTO'> = {
  id: 'file',
  name: 'movie.mp4',
  size: 3000000000,
  duration: 1000,
  width: 3840,
  height: 2160,
  available: true,
  tracks: [],
};

describe('language subtitle preference', () => {
  it.each(['vi', 'vie', 'VI-vn', 'Vietnamese', 'Tiếng Việt'])(
    'selects %s for Vietnamese UI',
    (language) => {
      expect(preferredSubtitle([track(2, 'eng'), track(3, language)], [], 'vi-VN', true)).toBe('3');
    },
  );
  it('matches track titles and uploaded filenames without matching substrings', () => {
    expect(preferredSubtitle([track(2, 'und', 'Tiếng Việt')], [], 'vi', true)).toBe('2');
    expect(
      preferredSubtitle([], [{ id: 'sub', name: 'Film.vi.srt', cueCount: 1 }], 'vi', false),
    ).toBe('upload:sub');
    expect(preferredSubtitle([], [{ id: 'sub', name: 'Civil.srt', cueCount: 1 }], 'vi', true)).toBe(
      '-1',
    );
  });
  it('prefers an overlay, capability-gates embedded subtitles and keeps unmatched subtitles off', () => {
    const tracks = [track(2, 'eng'), track(3, 'vie')];
    expect(
      preferredSubtitle(tracks, [{ id: 'sub', name: 'Vietnamese.vtt', cueCount: 1 }], 'vi', true),
    ).toBe('upload:sub');
    expect(preferredSubtitle(tracks, [], 'vi', false)).toBe('-1');
    expect(preferredSubtitle(tracks, [], 'en-US', true)).toBe('2');
    expect(preferredSubtitle([track(3, 'vie')], [], 'en', true)).toBe('-1');
  });
});

describe('network quality selection', () => {
  it('selects bitrate and resolution budgets with network headroom', () => {
    const slow = selectAutoQuality(file, 1000000);
    expect(slow).toEqual({ maxHeight: 720, maxBitrate: 5000000 });
    expect(selectAutoQuality(file, 2000000)).toEqual({ maxHeight: 1080, maxBitrate: 10000000 });
    expect(selectAutoQuality({ ...file, size: 10000000000 }, 5000000)).toEqual({
      maxHeight: 0,
      maxBitrate: 25500000,
    });
    expect(selectAutoQuality(file, 10000000)).toEqual({ maxHeight: 0, maxBitrate: 0 });
  });
  it('keeps source quality with unknown bandwidth and caps bitrate without upscaling', () => {
    for (const invalid of [null, 0, -1, NaN, Infinity]) {
      expect(selectAutoQuality(file, invalid)).toEqual({ maxHeight: 0, maxBitrate: 0 });
    }
    expect(selectAutoQuality({ ...file, width: 640, height: 480 }, 1000000)).toEqual({
      maxHeight: 0,
      maxBitrate: 5000000,
    });
    expect(selectAutoQuality(file, 1000000, 2).maxBitrate).toBeLessThan(
      selectAutoQuality(file, 1000000).maxBitrate,
    );
    expect(selectAutoQuality(file, 1).maxBitrate).toBe(100000);
  });
  it('adapts when successive measurements vary instead of requiring identical rates', () => {
    const controller = createAutoQuality({ maxHeight: 0, maxBitrate: 0 });
    expect(controller.sample({ maxHeight: 720, maxBitrate: 4000000 }, 0)).toBeNull();
    expect(controller.sample({ maxHeight: 720, maxBitrate: 3500000 }, 1000)).toEqual({
      maxHeight: 720,
      maxBitrate: 3500000,
    });
  });
  it('requires sustained samples and a cooldown before switching again', () => {
    const controller = createAutoQuality({ maxHeight: 0, maxBitrate: 0 });
    const low = { maxHeight: 720 as const, maxBitrate: 4000000 };
    const high = { maxHeight: 1080 as const, maxBitrate: 8000000 };
    expect(controller.sample(low, 0)).toBeNull();
    expect(controller.sample(low, 1000)).toEqual(low);
    for (let n = 0; n < 4; n++) expect(controller.sample(high, 2000 + n * 1000)).toBeNull();
    expect(controller.sample(high, 32000)).toEqual(high);
    expect(controller.sample(low, 63000)).toBeNull();
    expect(controller.sample(low, 64000)).toEqual(low);
  });
});

it('measures only a bounded authorized range and cancels ignored Range responses', async () => {
  const cancel = vi.fn().mockResolvedValue(undefined);
  const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
    status: 200,
    body: { cancel },
  } as unknown as Response);
  const abort = new AbortController();
  expect(await measurePlaybackRate('/stream?token=short-lived', abort.signal)).toBeNull();
  expect(fetch).toHaveBeenCalledWith(
    '/stream?token=short-lived',
    expect.objectContaining({
      headers: { Range: 'bytes=0-262143' },
      cache: 'no-store',
      signal: expect.any(AbortSignal),
    }),
  );
  expect(cancel).toHaveBeenCalled();
  fetch.mockResolvedValue(new Response(new Uint8Array(262144), { status: 206 }));
  expect(await measurePlaybackRate('/stream', abort.signal)).toBeGreaterThan(0);
  abort.abort();
  expect(await measurePlaybackRate('/stream', abort.signal)).toBeNull();
});
