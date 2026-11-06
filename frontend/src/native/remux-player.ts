import { createAdaptiveBuffer, INITIAL_BUFFER_CONFIG } from './playback-buffer';

const MIN_CHUNK = 256 * 1024;
const MAX_CHUNK = 4 * 1024 * 1024;

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
  signal.throwIfAborted();
  const requests = new AbortController();
  const media = new MediaSource();
  const objectURL = URL.createObjectURL(media);
  const stop = () => {
    requests.abort(signal.reason);
    URL.revokeObjectURL(objectURL);
  };
  signal.addEventListener('abort', stop, { once: true });
  try {
    video.src = objectURL;
    await new Promise<void>((resolve, reject) => {
      const clean = () => {
        media.removeEventListener('sourceopen', opened);
        signal.removeEventListener('abort', aborted);
      };
      const opened = () => {
        clean();
        resolve();
      };
      const aborted = () => {
        clean();
        reject(signal.reason);
      };
      media.addEventListener('sourceopen', opened, { once: true });
      signal.addEventListener('abort', aborted, { once: true });
    });
    let buffer: SourceBuffer;
    try {
      buffer = media.addSourceBuffer(mime);
    } catch {
      throw new RemuxDecodeError('Remux decoder unavailable');
    }
    const update = (action: () => void) =>
      new Promise<void>((resolve, reject) => {
        signal.throwIfAborted();
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
        signal.throwIfAborted();
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
    const config = { ...INITIAL_BUFFER_CONFIG };
    const adaptive = createAdaptiveBuffer(config);
    let chunkSize = 1024 * 1024;
    const ahead = () =>
      buffer.buffered.length
        ? Math.max(0, buffer.buffered.end(buffer.buffered.length - 1) - video.currentTime)
        : 0;
    const download = async (offset: number, size: number) => {
      const started = performance.now();
      const response = await fetch(url, {
        signal: requests.signal,
        cache: 'no-store',
        headers: { Range: `bytes=${offset}-${offset + size - 1}` },
      });
      const complete = response.headers.get('X-Playback-Complete') === 'true';
      if (response.status === 416) {
        const range = /^bytes \*\/(\d+)$/.exec(response.headers.get('Content-Range') ?? '');
        const total = range ? Number(range[1]) : NaN;
        if (complete && (!Number.isSafeInteger(total) || total !== offset))
          throw new Error('Remux end offset mismatch');
        return { chunk: null, complete, total, elapsed: 0 };
      }
      if (response.status !== 206) throw new Error('Remux range unavailable');
      const range = /^bytes (\d+)-(\d+)\/(\d+)$/.exec(response.headers.get('Content-Range') ?? '');
      const [start, end, total] = range ? range.slice(1).map(Number) : [];
      if (
        ![start, end, total].every(Number.isSafeInteger) ||
        start !== offset ||
        end < start ||
        end >= total ||
        end - start + 1 > size
      )
        throw new Error('Remux range mismatch');
      const chunk = await response.arrayBuffer();
      if (chunk.byteLength !== end - start + 1) throw new Error('Remux range incomplete');
      const ended = performance.now();
      onSample?.(chunk.byteLength, started, ended);
      return { chunk, complete, total, elapsed: ended - started };
    };
    // One request may overlap MSE append/eviction. Capture rejection immediately:
    // quota recovery can leave a prefetched request waiting for several seconds.
    const prefetch = (offset: number) =>
      download(offset, chunkSize).then(
        (value) => ({ value }),
        (error: unknown) => ({ error }),
      );
    let pending: ReturnType<typeof prefetch> | undefined;
    let offset = 0;
    let sampledEnd = 0;
    let sampleBytes = 0;
    let sampleElapsed = 0;
    while (!signal.aborted) {
      // Evict history even while the forward target is full (including after seek).
      const trim = video.currentTime - config.backBufferLength;
      if (trim > 0 && buffer.buffered.length && buffer.buffered.start(0) < trim) {
        await update(() => buffer.remove(0, trim));
      }
      if (ahead() >= config.maxBufferLength) {
        await pause();
        continue;
      }
      const result = await (pending ?? prefetch(offset));
      pending = undefined;
      if ('error' in result) throw result.error;
      signal.throwIfAborted();
      const { chunk, complete, total, elapsed } = result.value;
      if (!chunk) {
        if (complete) {
          media.endOfStream();
          return;
        }
        await pause();
        continue;
      }
      offset += chunk.byteLength;
      // Aim for roughly one second per full response, bounded to 256 KiB–4 MiB.
      // Short responses at the growing-file edge do not shrink the request size.
      if (chunk.byteLength === chunkSize && elapsed > 0) {
        const desired = Math.ceil((chunk.byteLength * 1000) / elapsed / MIN_CHUNK) * MIN_CHUNK;
        chunkSize = Math.max(MIN_CHUNK, Math.min(MAX_CHUNK, chunkSize * 2, desired));
      }
      const finished = complete && offset === total;
      if (!finished && ahead() < config.maxBufferLength) pending = prefetch(offset);
      for (;;) {
        try {
          await update(() => buffer.appendBuffer(chunk));
          break;
        } catch (error) {
          if (!(error instanceof DOMException) || error.name !== 'QuotaExceededError') throw error;
          // Quota recovery owns this session's target; never grow it again.
          adaptive.suspend();
          config.maxBufferLength = config.maxMaxBufferLength = Math.max(
            6,
            Math.min(30, config.maxBufferLength / 2),
          );
          chunkSize = MIN_CHUNK;
          const trim = video.currentTime - 1;
          if (trim > 0 && buffer.buffered.length && buffer.buffered.start(0) < trim) {
            await update(() => buffer.remove(0, trim));
          } else {
            await pause();
          }
        }
      }
      sampleBytes += chunk.byteLength;
      sampleElapsed += elapsed;
      const end = buffer.buffered.length ? buffer.buffered.end(buffer.buffered.length - 1) : 0;
      // Partial MP4 boxes can span responses. Account for all their bytes/time
      // only when append actually advances the buffered media timeline.
      if (end > sampledEnd) {
        adaptive.sample(sampleBytes, end - sampledEnd, sampleElapsed, video.playbackRate);
        sampledEnd = end;
        sampleBytes = sampleElapsed = 0;
      }
      if (finished) {
        media.endOfStream();
        return;
      }
    }
  } finally {
    requests.abort();
    signal.removeEventListener('abort', stop);
    // Keep the object URL attached until playback is stopped, including at EOF.
    if (signal.aborted) stop();
    else signal.addEventListener('abort', stop, { once: true });
  }
}
