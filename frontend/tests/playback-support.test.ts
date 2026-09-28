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
