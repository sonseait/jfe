import { describe, expect, it, vi } from 'vitest';
import { playRemux } from '../src/native/remux-player';

describe('fragmented MP4 reader', () => {
  it.each([false, true])(
    'appends bounded ranges, handles quota (%s), and waits for worker EOF',
    async (quota) => {
      const appends: ArrayBuffer[] = [];
      const removals: number[] = [];
      let exhausted = quota;
      class Buffer extends EventTarget {
        buffered = { length: quota ? 1 : 0, start: () => 0, end: () => 10 };
        remove(_start: number, end: number) {
          removals.push(end);
          this.buffered.length = 0;
          queueMicrotask(() => this.dispatchEvent(new Event('updateend')));
        }
        appendBuffer(data: ArrayBuffer) {
          if (exhausted) {
            exhausted = false;
            throw new DOMException('buffer quota', 'QuotaExceededError');
          }
          appends.push(data);
          queueMicrotask(() => this.dispatchEvent(new Event('updateend')));
        }
      }
      const ended = vi.fn();
      class Source extends EventTarget {
        constructor() {
          super();
          queueMicrotask(() => this.dispatchEvent(new Event('sourceopen')));
        }
        addSourceBuffer(mime: string) {
          expect(mime).toContain('avc1');
          return new Buffer();
        }
        endOfStream = ended;
      }
      vi.stubGlobal('MediaSource', Source);
      const revoke = vi.fn();
      vi.stubGlobal('URL', { createObjectURL: () => 'blob:remux', revokeObjectURL: revoke });
      const fetcher = vi
        .fn()
        .mockResolvedValueOnce(
          new Response(new Uint8Array([1, 2]), {
            status: 206,
            headers: { 'Content-Range': 'bytes 0-1/2' },
          }),
        )
        .mockResolvedValueOnce(
          new Response(new Uint8Array([3]), {
            status: 206,
            headers: { 'Content-Range': 'bytes 2-2/3', 'X-Playback-Complete': 'true' },
          }),
        );
      vi.stubGlobal('fetch', fetcher);
      const video = { currentTime: quota ? 5 : 0, src: '' } as HTMLVideoElement;
      const controller = new AbortController();
      try {
        await playRemux(
          video,
          '/stream?token=x',
          'video/mp4; codecs="avc1.640028"',
          controller.signal,
        );
        expect(video.src).toBe('blob:remux');
        expect(fetcher.mock.calls[0][1].headers.Range).toBe('bytes=0-262143');
        expect(fetcher.mock.calls[1][1].headers.Range).toBe('bytes=2-262145');
        expect(appends.map((a) => a.byteLength)).toEqual([2, 1]);
        expect(ended).toHaveBeenCalledOnce();
        expect(removals).toEqual(quota ? [4] : []);
        controller.abort();
        expect(revoke).toHaveBeenCalledWith('blob:remux');
      } finally {
        vi.unstubAllGlobals();
      }
    },
  );
});
