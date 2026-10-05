import { describe, expect, it } from 'vitest';
import { canDirectPlay } from '../src/native/playback-support';
import type { DTO } from '../src/native/api';

function file(name: string, video: string, audio: string): DTO<'FileDTO'> {
  return {
    id: 'test',
    name,
    size: 1,
    duration: 1,
    width: 1280,
    height: 720,
    available: true,
    tracks: [
      { index: 0, type: 'video', codec: video, title: '', language: '' },
      { index: 1, type: 'audio', codec: audio, title: '', language: '' },
    ],
  };
}
describe('browser playback support', () => {
  it('asks about the source codec pair rather than a fixed H264 sample', () => {
    expect(
      canDirectPlay(file('movie.mp4', 'hevc', 'aac'), (mime) =>
        mime.includes('hvc1') ? 'probably' : '',
      ),
    ).toBe(true);
    expect(canDirectPlay(file('movie.mp4', 'hevc', 'aac'), () => '')).toBe(false);
    expect(
      canDirectPlay(file('movie.webm', 'vp9', 'opus'), (mime) =>
        mime.startsWith('video/webm') && mime.includes('opus') ? 'probably' : '',
      ),
    ).toBe(true);
  });
  it('does not advertise unsupported containers or audio', () => {
    expect(canDirectPlay(file('movie.mkv', 'h264', 'aac'), () => 'probably')).toBe(false);
    expect(canDirectPlay(file('movie.mp4', 'h264', 'dts'), () => 'probably')).toBe(false);
  });
});

describe('source-specific capabilities', () => {
  it('preserves Main10 and tests remux independently from the MKV container', async () => {
    const source = file('movie.mkv', 'hevc', 'truehd');
    Object.assign(source.tracks[0], {
      profile: 'Main 10',
      bitDepth: 10,
      level: 153,
      width: 3840,
      height: 2160,
    });
    const { playbackCapabilities } = await import('../src/native/playback-support');
    const support = await playbackCapabilities(
      source,
      -1,
      (mime) => (mime.includes('mp4a.40.2') ? 'probably' : ''),
      (mime) => mime.includes('hvc1.2.4.L153'),
    );
    expect(support.capabilities.containers).toEqual([]);
    expect(support.capabilities.video[0].bitDepth).toBe(10);
    expect(support.capabilities.remux).toBe(true);
    expect(support.remuxMime).toContain('mp4a.40.2');
    const rejected = await playbackCapabilities(
      source,
      -1,
      () => '',
      () => false,
    );
    expect(rejected.capabilities.video).toEqual([]);
  });
  it('uses decodingInfo to reject unsupported profiles and HDR', async () => {
    const source = file('movie.mp4', 'hevc', 'aac');
    Object.assign(source.tracks[0], {
      profile: 'Main 10',
      bitDepth: 10,
      level: 153,
      hdrFormat: 'hdr10',
    });
    const { playbackCapabilities } = await import('../src/native/playback-support');
    const supported = await playbackCapabilities(
      source,
      -1,
      () => 'probably',
      () => true,
      async (configuration) => {
        expect(configuration.video?.transferFunction).toBe('pq');
        expect(configuration.video?.contentType).toContain('hvc1.2');
        return { supported: true, smooth: true, powerEfficient: true, keySystemAccess: null };
      },
    );
    expect(supported.capabilities.video).toHaveLength(1);
    const rejected = await playbackCapabilities(
      source,
      -1,
      () => 'probably',
      () => true,
    );
    expect(rejected.capabilities.video).toEqual([]);
  });
});

it('separates native and MSE decode support instead of assuming they match', async () => {
  const source = file('movie.mp4', 'hevc', 'aac');
  Object.assign(source.tracks[0], { profile: 'Main 10', bitDepth: 10, level: 153 });
  const { playbackCapabilities } = await import('../src/native/playback-support');
  const support = await playbackCapabilities(
    source,
    -1,
    () => 'probably',
    () => true,
    async (configuration) => ({
      supported: configuration.type === 'media-source',
      smooth: true,
      powerEfficient: true,
      keySystemAccess: null,
    }),
  );
  expect(support.capabilities.containers).toEqual([]);
  expect(support.capabilities.video).toHaveLength(1);
  expect(support.capabilities.remux).toBe(true);
});
