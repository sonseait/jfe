export class RemuxDecodeError extends Error {}

// Read a growing worker-owned fMP4 file in bounded chunks. Range offsets follow
// received bytes; MSE retains partial boxes until the next append completes them.
export async function playRemux(
  video: HTMLVideoElement,
  url: string,
  mime: string,
  signal: AbortSignal,
  onSample?: (bytes: number, start: number, end: number) => void,
) {
  const media = new MediaSource();
  const objectURL = URL.createObjectURL(media);
  const stop = () => {
    URL.revokeObjectURL(objectURL);
  };
  signal.addEventListener('abort', stop, { once: true });
  try {
    video.src = objectURL;
    await new Promise<void>((resolve, reject) => {
      media.addEventListener('sourceopen', () => resolve(), { once: true });
      signal.addEventListener('abort', () => reject(signal.reason), { once: true });
    });
    let buffer: SourceBuffer;
    try {
      buffer = media.addSourceBuffer(mime);
    } catch {
      throw new RemuxDecodeError('Remux decoder unavailable');
    }
    const update = (action: () => void) =>
      new Promise<void>((resolve, reject) => {
        const clean = () => {
          buffer.removeEventListener('updateend', done);
          buffer.removeEventListener('error', failed);
          signal.removeEventListener('abort', failed);
        };
        const done = () => {
          clean();
          resolve();
        };
        const failed = () => {
          clean();
          reject(new RemuxDecodeError('Remux append failed'));
        };
        buffer.addEventListener('updateend', done, { once: true });
        buffer.addEventListener('error', failed, { once: true });
        signal.addEventListener('abort', failed, { once: true });
        try {
          action();
        } catch (error) {
          clean();
          reject(error);
        }
      });
    const pause = () =>
      new Promise<void>((resolve, reject) => {
        const abort = () => {
          clearTimeout(timer);
          reject(signal.reason);
        };
        const timer = window.setTimeout(() => {
          signal.removeEventListener('abort', abort);
          resolve();
        }, 250);
        signal.addEventListener('abort', abort, { once: true });
      });
    let offset = 0;
    while (!signal.aborted) {
      if (
        buffer.buffered.length &&
        buffer.buffered.end(buffer.buffered.length - 1) - video.currentTime > 30
      ) {
        await pause();
        continue;
      }
      if (
        video.currentTime > 60 &&
        buffer.buffered.length &&
        buffer.buffered.start(0) < video.currentTime - 60
      ) {
        await update(() => buffer.remove(0, video.currentTime - 60));
      }
      const started = performance.now();
      const response = await fetch(url, {
        signal,
        cache: 'no-store',
        headers: { Range: `bytes=${offset}-${offset + 256 * 1024 - 1}` },
      });
      const complete = response.headers.get('X-Playback-Complete') === 'true';
      if (response.status === 416) {
        if (complete) {
          media.endOfStream();
          return;
        }
        await pause();
        continue;
      }
      if (response.status !== 206) throw new Error('Remux range unavailable');
      const chunk = await response.arrayBuffer();
      onSample?.(chunk.byteLength, started, performance.now());
      if (!chunk.byteLength) {
        await pause();
        continue;
      }
      for (;;) {
        try {
          await update(() => buffer.appendBuffer(chunk));
          break;
        } catch (error) {
          if (!(error instanceof DOMException) || error.name !== 'QuotaExceededError') throw error;
          // Large HEVC frames can exceed the browser's quota before the time
          // ceiling. Evict history and wait for playback rather than encode video.
          const trim = video.currentTime - 1;
          if (trim > 0 && buffer.buffered.length && buffer.buffered.start(0) < trim) {
            await update(() => buffer.remove(0, trim));
          } else {
            await pause();
          }
        }
      }
      offset += chunk.byteLength;
      const total = Number(response.headers.get('Content-Range')?.split('/')[1]);
      if (complete && offset >= total) {
        media.endOfStream();
        return;
      }
    }
  } finally {
    signal.removeEventListener('abort', stop);
    // Keep the object URL attached until playback is stopped, including at EOF.
    if (signal.aborted) stop();
    else signal.addEventListener('abort', stop, { once: true });
  }
}
