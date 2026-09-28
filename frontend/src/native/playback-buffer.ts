export const INITIAL_BUFFER_CONFIG = {
  maxBufferLength: 60,
  maxMaxBufferLength: 60,
  // Use the time target below, rather than hls.js's independent byte-based floor.
  maxBufferSize: 0,
  backBufferLength: 30,
};

const BUFFER_BUDGET = 128 * 1024 * 1024;
type BufferConfig = typeof INITIAL_BUFFER_CONFIG;

export function createAdaptiveBuffer(config: BufferConfig) {
  const samples: { headroom: number; bytesPerSecond: number }[] = [];
  let applied = config.maxMaxBufferLength;
  let suspended = false;
  return {
    suspend() {
      suspended = true;
    },
    sample(bytes: number, duration: number, elapsedMS: number, playbackRate = 1) {
      // Leave quota-error recovery in control once hls.js reduces its own limit.
      if (suspended || config.maxMaxBufferLength < applied) {
        suspended = true;
        return;
      }
      if (
        ![bytes, duration, elapsedMS, playbackRate].every(
          (value) => Number.isFinite(value) && value > 0,
        )
      )
        return;
      samples.push({ headroom: (duration * 1000) / elapsedMS, bytesPerSecond: bytes / duration });
      if (samples.length > 3) samples.shift();
      const headroom = Math.min(...samples.map((sample) => sample.headroom)) / playbackRate;
      // Require three consistently fast segments before growing; slow transfers
      // lower the target immediately without flushing media already buffered.
      const seconds = samples.length < 3 || headroom < 2 ? 60 : headroom < 5 ? 180 : 300;
      const mediaRate = Math.max(...samples.map((sample) => sample.bytesPerSecond));
      const memorySeconds = BUFFER_BUDGET / mediaRate - config.backBufferLength;
      applied = Math.max(6, Math.min(seconds, memorySeconds));
      config.maxBufferLength = applied;
      config.maxMaxBufferLength = applied;
    },
  };
}
