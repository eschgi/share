import { describe, expect, it } from 'vitest';
import { parseExifDate, takenAtOf } from '../src/taken';

// Small files made by hand, with only what the reader looks at.

function concat(...parts: Uint8Array[]): Uint8Array<ArrayBuffer> {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let at = 0;
  for (const p of parts) {
    out.set(p, at);
    at += p.length;
  }
  return out;
}

const ascii = (s: string) => new TextEncoder().encode(s);
const u16 = (n: number, little = false) => {
  const b = new Uint8Array(2);
  new DataView(b.buffer).setUint16(0, n, little);
  return b;
};
const u32 = (n: number, little = false) => {
  const b = new Uint8Array(4);
  new DataView(b.buffer).setUint32(0, n, little);
  return b;
};

/** A little-endian TIFF with IFD0 pointing to an EXIF IFD that has DateTimeOriginal, and maybe its offset. */
function tiff(date: string, offset?: string): Uint8Array {
  const le = true;
  const dateBytes = concat(ascii(date), new Uint8Array([0]));
  const offBytes = offset ? concat(ascii(offset), new Uint8Array([0])) : new Uint8Array();
  const ifd0At = 8;
  const ifd0 = concat(u16(1, le), u16(0x8769, le), u16(4, le), u32(1, le), u32(ifd0At + 2 + 12 + 4, le), u32(0, le));
  const exifAt = ifd0At + ifd0.length;
  const n = offset ? 2 : 1;
  const exifSize = 2 + n * 12 + 4;
  const dateAt = exifAt + exifSize;
  const offAt = dateAt + dateBytes.length;
  const entries = [concat(u16(0x9003, le), u16(2, le), u32(dateBytes.length, le), u32(dateAt, le))];
  if (offset) entries.push(concat(u16(0x9011, le), u16(2, le), u32(offBytes.length, le), u32(offAt, le)));
  const exif = concat(u16(n, le), ...entries, u32(0, le));
  return concat(ascii('II'), u16(42, le), u32(ifd0At, le), ifd0, exif, dateBytes, offBytes);
}

function jpeg(date: string, offset?: string): Blob {
  const app1Body = concat(ascii('Exif'), new Uint8Array([0, 0]), tiff(date, offset));
  const app0 = concat(new Uint8Array([0xff, 0xe0]), u16(16), ascii('JFIF'), new Uint8Array(10));
  const app1 = concat(new Uint8Array([0xff, 0xe1]), u16(app1Body.length + 2), app1Body);
  return new Blob([concat(new Uint8Array([0xff, 0xd8]), app0, app1, new Uint8Array([0xff, 0xda, 0, 2, 1, 2, 3]))]);
}

const box = (type: string, ...content: Uint8Array[]) => {
  const body = concat(...content);
  return concat(u32(body.length + 8), ascii(type), body);
};

function movie(creation: number): Blob {
  // ftyp, a big mdat, and moov at the end, as phones write them.
  const mvhd = box('mvhd', new Uint8Array([0, 0, 0, 0]), u32(creation), u32(creation), u32(1000), u32(5000), new Uint8Array(80));
  return new Blob([concat(box('ftyp', ascii('isom'), u32(512), ascii('isommp42')), box('mdat', new Uint8Array(200_000)), box('moov', mvhd))]);
}

function heic(date: string): Blob {
  const exifPayload = concat(u32(6), ascii('Exif'), new Uint8Array([0, 0]), tiff(date, '+02:00'));
  const infe = box('infe', new Uint8Array([2, 0, 0, 0]), u16(7), u16(0), ascii('Exif'), new Uint8Array([0]));
  const iinf = box('iinf', new Uint8Array([0, 0, 0, 0]), u16(1), infe);
  const ftyp = box('ftyp', ascii('heic'), u32(0), ascii('mif1heic'));
  // iloc version 0: offset size 4, length size 4, base offset size 0; one item, one extent.
  const ilocFor = (offset: number) => box('iloc', new Uint8Array([0, 0, 0, 0]), u16(0x4400), u16(1), u16(7), u16(0), u16(1), u32(offset), u32(exifPayload.length));
  const metaFor = (offset: number) => box('meta', new Uint8Array([0, 0, 0, 0]), iinf, ilocFor(offset));
  const before = ftyp.length + metaFor(0).length + 8; // the payload sits in mdat, after its header
  return new Blob([concat(ftyp, metaFor(before), box('mdat', exifPayload))]);
}

describe('the date a photo or video was taken', () => {
  it('comes from a JPEG\'s EXIF, with its offset', async () => {
    expect(await takenAtOf(jpeg('2025:06:14 18:32:10', '+02:00'))).toBe(Date.UTC(2025, 5, 14, 16, 32, 10));
  });

  it('counts as this browser\'s time without an offset', async () => {
    expect(await takenAtOf(jpeg('2025:06:14 18:32:10'))).toBe(new Date(2025, 5, 14, 18, 32, 10).getTime());
  });

  it('comes from the header of an MP4, with moov at the end', async () => {
    const seconds = Date.UTC(2026, 8, 26, 21, 50, 0) / 1000 + 2082844800;
    expect(await takenAtOf(movie(seconds))).toBe(Date.UTC(2026, 8, 26, 21, 50, 0));
    expect(await takenAtOf(movie(0))).toBeNull();
  });

  it('comes from a HEIC\'s EXIF item', async () => {
    expect(await takenAtOf(heic('2024:12:24 19:05:00'))).toBe(Date.UTC(2024, 11, 24, 17, 5, 0));
  });

  it('is null for anything else, or a broken file', async () => {
    expect(await takenAtOf(new Blob([ascii('%PDF-1.7 hello')]))).toBeNull();
    expect(await takenAtOf(new Blob([new Uint8Array([0xff, 0xd8, 0xff, 0xe1, 0xff, 0xff])]))).toBeNull();
    expect(await takenAtOf(new Blob([]))).toBeNull();
  });

  it('reads EXIF\'s way of writing dates', () => {
    expect(parseExifDate('2025:06:14 18:32:10', '-05:00')).toBe(Date.UTC(2025, 5, 14, 23, 32, 10));
    expect(parseExifDate('0000:00:00 00:00:00', null)).toBeNull();
    expect(parseExifDate('yesterday', null)).toBeNull();
  });
});
