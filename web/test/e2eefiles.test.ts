import { describe, expect, it } from 'vitest';
import { jpegSize } from '../src/e2ee/files';
import { byteRange, parseRange } from '../src/e2ee/stream';

/** A JPEG's start: SOI, an APP0 segment, a DHT one, then a frame header of width × height. */
function jpeg(width: number, height: number, frame = 0xc0): Uint8Array {
  return new Uint8Array([
    0xff, 0xd8,
    0xff, 0xe0, 0x00, 0x06, 0x4a, 0x46, 0x49, 0x46,
    0xff, 0xc4, 0x00, 0x04, 0x00, 0x00,
    0xff, frame, 0x00, 0x0b, 0x08, height >> 8, height & 0xff, width >> 8, width & 0xff, 0x01, 0x01, 0x11, 0x00,
    0xff, 0xd9,
  ]);
}

describe('sealed thumbnails', () => {
  it('reads the size a JPEG says it has, past other segments', () => {
    expect(jpegSize(jpeg(512, 384))).toEqual({ width: 512, height: 384 });
    expect(jpegSize(jpeg(300, 4000, 0xc2))).toEqual({ width: 300, height: 4000 });
  });

  it('refuses what is no JPEG', () => {
    expect(jpegSize(new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0, 0, 0, 0, 0, 0, 0, 0]))).toBeNull();
    expect(jpegSize(jpeg(512, 384).subarray(0, 18))).toBeNull();
    const broken = jpeg(512, 384);
    broken[10] = 0x00; // no marker where one belongs
    expect(jpegSize(broken)).toBeNull();
  });
});

describe('ranges of decrypted streams', () => {
  it('reads Range headers as the server does', () => {
    expect(parseRange(null, 100)).toBeNull();
    expect(parseRange('bytes=-', 100)).toBeNull();
    expect(parseRange('items=0-1', 100)).toBeNull();
    expect(parseRange('bytes=0-', 100)).toEqual({ start: 0, end: 100 });
    expect(parseRange('bytes=10-19', 100)).toEqual({ start: 10, end: 20 });
    expect(parseRange('bytes=90-200', 100)).toEqual({ start: 90, end: 100 });
    expect(parseRange('bytes=-30', 100)).toEqual({ start: 70, end: 100 });
    expect(parseRange('bytes=-300', 100)).toEqual({ start: 0, end: 100 });
    expect(parseRange('bytes=100-', 100)).toBe('unsatisfiable');
    expect(parseRange('bytes=20-10', 100)).toBe('unsatisfiable');
  });

  it('cuts a stream of parts to a span', async () => {
    const parts = [new Uint8Array([0, 1, 2]), new Uint8Array([3, 4]), new Uint8Array([5, 6, 7, 8])];
    const cut = async (skip: number, length: number) => {
      const out: number[] = [];
      const reader = new ReadableStream<Uint8Array>({
        start(c) {
          for (const p of parts) c.enqueue(p);
          c.close();
        },
      })
        .pipeThrough(byteRange(skip, length))
        .getReader();
      for (let r = await reader.read(); !r.done; r = await reader.read()) out.push(...r.value);
      return out;
    };
    expect(await cut(0, 9)).toEqual([0, 1, 2, 3, 4, 5, 6, 7, 8]);
    expect(await cut(2, 4)).toEqual([2, 3, 4, 5]);
    expect(await cut(5, 100)).toEqual([5, 6, 7, 8]);
    expect(await cut(3, 0)).toEqual([]);
  });
});
