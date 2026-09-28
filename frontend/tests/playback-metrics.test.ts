import { describe, expect, it } from 'vitest';
import {
  bufferedRanges,
  transferRate,
  formatTransferRate,
  containsTime,
  hasSegment,
} from '../src/native/playback-metrics';

function ranges(values: [number, number][]): TimeRanges {
  return { length: values.length, start: (i) => values[i][0], end: (i) => values[i][1] };
}

describe('playback metrics', () => {
  it('reuses only buffered or published segments, respecting gaps and session offsets', () => {
    const buffered = ranges([
      [0, 4],
      [8, 12],
    ]);
    expect(containsTime(buffered, 2)).toBe(true);
    expect(containsTime(buffered, 6)).toBe(false);
    const published = [
      { start: 0, duration: 4 },
      { start: 4, duration: 4 },
      { start: 8, duration: 4, gap: true },
    ];
    expect(hasSegment(published, 6)).toBe(true);
    expect(hasSegment(published, 9)).toBe(false);
    expect(hasSegment(published, 12)).toBe(false);
    expect(hasSegment(published, 105 - 100)).toBe(true);
    expect(hasSegment(published, 99 - 100)).toBe(false);
  });
  it('preserves holes and maps HLS session ranges onto the full movie timeline', () => {
    expect(
      bufferedRanges(
        ranges([
          [0, 10],
          [20, 40],
        ]),
        50,
        100,
      ),
    ).toEqual([
      [50, 60],
      [70, 90],
    ]);
    expect(
      bufferedRanges(
        ranges([
          [0, 30],
          [40, 50],
        ]),
        90,
        100,
      ),
    ).toEqual([[90, 100]]);
    expect(bufferedRanges(ranges([[0, 10]]), 0, 0)).toEqual([]);
    expect(bufferedRanges(ranges([]), 0, 100)).toEqual([]);
  });
  it('measures bytes per second rather than bitrate or decoded video size', () => {
    expect(transferRate(1024, 1000, 1500)).toBe(2048);
    expect(formatTransferRate(2048, 'en')).toBe('2 KiB/s');
    expect(formatTransferRate(0, 'en')).toBe('0 B/s');
    expect(transferRate(0, 1000, 1500)).toBeNull();
    expect(transferRate(1024, 1000, 1000)).toBeNull();
    expect(transferRate(1024, 1000, Infinity)).toBeNull();
  });
});
