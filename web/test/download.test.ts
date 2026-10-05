import { describe, expect, it } from 'vitest';
import { downloadOneByOne, eachLimit } from '../src/account/library/actions';

describe('downloadOneByOne', () => {
  it('starts each file from its own link, a second apart', async () => {
    const started: string[] = [];
    const waits: number[] = [];
    const told: string[] = [];
    const n = await downloadOneByOne(['a', 'b', 'c'], {
      start: (href) => started.push(href),
      wait: async (ms) => {
        waits.push(ms);
      },
      onStart: (k, total) => told.push(`${k}/${total}`),
    });
    expect(n).toBe(3);
    expect(started).toEqual(['/api/files/a/content', '/api/files/b/content', '/api/files/c/content']);
    expect(waits).toEqual([1000, 1000]);
    expect(told).toEqual(['1/3', '2/3', '3/3']);
  });

  it('stops when asked, and takes at most a hundred', async () => {
    const stop = new AbortController();
    const started: string[] = [];
    const n = await downloadOneByOne(['a', 'b', 'c'], {
      signal: stop.signal,
      start: (href) => started.push(href),
      wait: async () => stop.abort(),
    });
    expect(n).toBe(1);
    expect(started).toHaveLength(1);

    const many = Array.from({ length: 150 }, (_, i) => `f${i}`);
    expect(await downloadOneByOne(many, { start: () => {}, wait: async () => {} })).toBe(eachLimit);
  });
});
