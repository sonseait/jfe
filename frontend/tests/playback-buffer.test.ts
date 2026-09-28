import { expect, it } from 'vitest';
import { createAdaptiveBuffer, INITIAL_BUFFER_CONFIG } from '../src/native/playback-buffer';

function player() {
  const config = { ...INITIAL_BUFFER_CONFIG };
  return { config, buffer: createAdaptiveBuffer(config) };
}

it('requires sustained fast transfers, then buffers up to five minutes', () => {
  const { config, buffer } = player();
  buffer.sample(256 * 1024, 4, 500);
  buffer.sample(256 * 1024, 4, 500);
  expect(config.maxBufferLength).toBe(60);
  buffer.sample(256 * 1024, 4, 500);
  expect(config.maxBufferLength).toBe(300);
  expect(config.maxMaxBufferLength).toBe(300);
});

it('reduces future loading immediately when the network slows and waits before growing again', () => {
  const { config, buffer } = player();
  for (let i = 0; i < 3; i++) buffer.sample(256 * 1024, 4, 500);
  buffer.sample(256 * 1024, 4, 6000);
  expect(config.maxBufferLength).toBe(60);
  for (let i = 0; i < 2; i++) buffer.sample(256 * 1024, 4, 500);
  expect(config.maxBufferLength).toBe(60);
  buffer.sample(256 * 1024, 4, 500);
  expect(config.maxBufferLength).toBe(300);
});

it('accounts for playback speed and moderate network headroom', () => {
  const { config, buffer } = player();
  for (let i = 0; i < 3; i++) buffer.sample(256 * 1024, 4, 1200);
  expect(config.maxBufferLength).toBe(180);
  buffer.sample(256 * 1024, 4, 1200, 2);
  expect(config.maxBufferLength).toBe(60);
});

it('bounds high-bitrate targets using the back buffer and conservative recent segment size', () => {
  const { config, buffer } = player();
  for (let i = 0; i < 3; i++) buffer.sample(8 * 1024 * 1024, 4, 500);
  expect(config.maxBufferLength).toBe(34);
  buffer.sample(256 * 1024, 4, 500);
  expect(config.maxBufferLength).toBe(34);
});

it('ignores invalid transfers and starts fresh for each playback session', () => {
  const { config, buffer } = player();
  for (const value of [0, -1, NaN, Infinity]) {
    buffer.sample(value, 4, 500);
    buffer.sample(1024, value, 500);
    buffer.sample(1024, 4, value);
    buffer.sample(1024, 4, 500, value);
  }
  buffer.sample(256 * 1024, 4, 500);
  expect(config.maxBufferLength).toBe(60);
  expect(player().config.maxBufferLength).toBe(60);
});

it('never undoes hls.js quota recovery or an explicit suspension', () => {
  for (const quotaError of [false, true]) {
    const { config, buffer } = player();
    if (quotaError) buffer.suspend();
    else config.maxMaxBufferLength = 20;
    for (let i = 0; i < 6; i++) buffer.sample(256 * 1024, 4, 500);
    expect(config.maxMaxBufferLength).toBe(quotaError ? 60 : 20);
  }
});
