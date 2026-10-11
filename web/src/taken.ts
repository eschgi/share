// When a photo or video was taken, read from the file before it is sent (docs/sort-plan.md): the
// EXIF DateTimeOriginal of a JPEG, HEIC or AVIF, or the creation time in an MP4 or MOV header.
// Also for a file into an encrypted folder: like its name and size, the date stays readable.

/** Unix ms when the file was taken, or null when it doesn't say. */
export async function takenAtOf(file: Blob): Promise<number | null> {
  try {
    const head = new DataView(await file.slice(0, 64 * 1024).arrayBuffer());
    if (head.byteLength >= 4 && head.getUint16(0) === 0xffd8) return jpegTaken(file);
    if (head.byteLength >= 12 && fourCC(head, 4) === 'ftyp') {
      const brand = fourCC(head, 8);
      return ['heic', 'heix', 'hevc', 'heim', 'heis', 'mif1', 'msf1', 'avif'].includes(brand) ? heifTaken(file) : movieTaken(file);
    }
  } catch {
    // An unreadable file has no date, and goes anyway.
  }
  return null;
}

function fourCC(v: DataView, at: number): string {
  return String.fromCharCode(v.getUint8(at), v.getUint8(at + 1), v.getUint8(at + 2), v.getUint8(at + 3));
}

async function bytes(file: Blob, from: number, length: number): Promise<DataView> {
  return new DataView(await file.slice(from, from + length).arrayBuffer());
}

/** A JPEG's EXIF is in an APP1 segment near the start. */
async function jpegTaken(file: Blob): Promise<number | null> {
  let at = 2;
  for (let i = 0; i < 64; i++) {
    const head = await bytes(file, at, 4);
    if (head.byteLength < 4 || head.getUint8(0) !== 0xff) return null;
    const marker = head.getUint8(1);
    const length = head.getUint16(2);
    if (marker === 0xda || marker === 0xd9) return null; // the picture begins: no EXIF before it
    if (marker === 0xe1) {
      const seg = await bytes(file, at + 4, length - 2);
      if (seg.byteLength >= 6 && fourCC(seg, 0) === 'Exif' && seg.getUint16(4) === 0) return tiffTaken(seg, 6);
    }
    at += 2 + length;
  }
  return null;
}

/** HEIC and AVIF keep EXIF as an item of the meta box: iinf names it, iloc says where it is. */
async function heifTaken(file: Blob): Promise<number | null> {
  const meta = await findBox(file, 0, file.size, 'meta');
  if (!meta) return null;
  const body = await bytes(file, meta.start + 4, Math.min(meta.size - 4, 256 * 1024)); // after version and flags
  let exifItem = -1;
  let iloc: DataView | null = null;
  for (const box of boxes(body, 0, body.byteLength)) {
    if (box.type === 'iinf') exifItem = exifItemId(body, box.start, box.end);
    if (box.type === 'iloc') iloc = new DataView(body.buffer, body.byteOffset + box.start, box.end - box.start);
  }
  if (exifItem < 0 || !iloc) return null;
  const place = ilocPlace(iloc, exifItem);
  if (!place) return null;
  const item = await bytes(file, place.offset, Math.min(place.length, 256 * 1024));
  if (item.byteLength < 4) return null;
  const skip = 4 + item.getUint32(0); // where the TIFF header starts, after "Exif\0\0" if any
  return tiffTaken(item, skip);
}

function exifItemId(v: DataView, start: number, end: number): number {
  const version = v.getUint8(start);
  let at = start + 4;
  const count = version === 0 ? v.getUint16(at) : v.getUint32(at);
  at += version === 0 ? 2 : 4;
  for (let i = 0; i < count && at + 8 <= end; i++) {
    const size = v.getUint32(at);
    if (fourCC(v, at + 4) === 'infe') {
      const ver = v.getUint8(at + 8);
      if (ver >= 2) {
        const id = ver === 2 ? v.getUint16(at + 12) : v.getUint32(at + 12);
        const typeAt = at + 12 + (ver === 2 ? 2 : 4) + 2;
        if (fourCC(v, typeAt) === 'Exif') return id;
      }
    }
    if (size < 8) break;
    at += size;
  }
  return -1;
}

function ilocPlace(v: DataView, item: number): { offset: number; length: number } | null {
  const version = v.getUint8(0);
  const sizes = v.getUint16(4);
  const offsetSize = sizes >> 12, lengthSize = (sizes >> 8) & 15, baseSize = (sizes >> 4) & 15, indexSize = version >= 1 ? sizes & 15 : 0;
  const read = (at: number, n: number) => (n === 0 ? 0 : n === 4 ? v.getUint32(at) : n === 8 ? Number(v.getBigUint64(at)) : n === 2 ? v.getUint16(at) : 0);
  let at = 6;
  const count = version < 2 ? v.getUint16(at) : v.getUint32(at);
  at += version < 2 ? 2 : 4;
  for (let i = 0; i < count; i++) {
    const id = version < 2 ? v.getUint16(at) : v.getUint32(at);
    at += version < 2 ? 2 : 4;
    if (version >= 1) at += 2; // construction method
    at += 2; // data reference index
    const base = read(at, baseSize);
    at += baseSize;
    const extents = v.getUint16(at);
    at += 2;
    let first: { offset: number; length: number } | null = null;
    for (let e = 0; e < extents; e++) {
      at += indexSize;
      const offset = read(at, offsetSize);
      at += offsetSize;
      const length = read(at, lengthSize);
      at += lengthSize;
      first ??= { offset: base + offset, length };
    }
    if (id === item) return first;
  }
  return null;
}

/** MP4 and MOV: moov holds mvhd, whose creation time counts seconds from 1904 in UTC. moov may sit
 * at the end of the file. */
async function movieTaken(file: Blob): Promise<number | null> {
  const moov = await findBox(file, 0, file.size, 'moov');
  if (!moov) return null;
  const body = await bytes(file, moov.start, Math.min(moov.size, 64 * 1024));
  for (const box of boxes(body, 0, body.byteLength)) {
    if (box.type !== 'mvhd') continue;
    const version = body.getUint8(box.start);
    const seconds = version === 1 ? Number(body.getBigUint64(box.start + 4)) : body.getUint32(box.start + 4);
    if (seconds === 0) return null; // left empty
    return (seconds - 2082844800) * 1000;
  }
  return null;
}

/** The boxes inside a view: their type, and where their content starts and ends. */
function* boxes(v: DataView, start: number, end: number): Generator<{ type: string; start: number; end: number }> {
  let at = start;
  while (at + 8 <= end) {
    let size = v.getUint32(at);
    let head = 8;
    if (size === 1 && at + 16 <= end) {
      size = Number(v.getBigUint64(at + 8));
      head = 16;
    } else if (size === 0) {
      size = end - at;
    }
    if (size < head) return;
    yield { type: fourCC(v, at + 4), start: at + head, end: Math.min(at + size, end) };
    at += size;
  }
}

/** Finds a top-level box of the file by reading only the box headers. */
async function findBox(file: Blob, start: number, end: number, type: string): Promise<{ start: number; size: number } | null> {
  let at = start;
  for (let i = 0; i < 1000 && at + 8 <= end; i++) {
    const head = await bytes(file, at, 16);
    let size = head.getUint32(0);
    let headSize = 8;
    if (size === 1) {
      size = Number(head.getBigUint64(8));
      headSize = 16;
    } else if (size === 0) {
      size = end - at;
    }
    if (size < headSize) return null;
    if (fourCC(head, 4) === type) return { start: at + headSize, size: size - headSize };
    at += size;
  }
  return null;
}

/** The date in a TIFF structure (EXIF) starting at tiff: DateTimeOriginal, else DateTimeDigitized,
 * with OffsetTimeOriginal when there is one; without, the time counts as this browser's. */
function tiffTaken(v: DataView, tiff: number): number | null {
  if (tiff + 8 > v.byteLength) return null;
  const order = v.getUint16(tiff);
  if (order !== 0x4949 && order !== 0x4d4d) return null;
  const little = order === 0x4949;
  const u16 = (at: number) => v.getUint16(tiff + at, little);
  const u32 = (at: number) => v.getUint32(tiff + at, little);
  const ascii = (entry: number) => {
    const count = u32(entry + 4);
    const at = count <= 4 ? entry + 8 : u32(entry + 8);
    let s = '';
    for (let i = 0; i < count - 1 && tiff + at + i < v.byteLength; i++) s += String.fromCharCode(v.getUint8(tiff + at + i));
    return s;
  };
  const entries = (ifd: number) => {
    const out = new Map<number, number>();
    if (tiff + ifd + 2 > v.byteLength) return out;
    const n = u16(ifd);
    for (let i = 0; i < n; i++) {
      const entry = ifd + 2 + i * 12;
      if (tiff + entry + 12 > v.byteLength) break;
      out.set(u16(entry), entry);
    }
    return out;
  };
  const ifd0 = entries(u32(4));
  const exifPointer = ifd0.get(0x8769);
  if (exifPointer === undefined) return null;
  const exif = entries(u32(exifPointer + 8));
  const when = exif.get(0x9003) ?? exif.get(0x9004);
  if (when === undefined) return null;
  const offsetEntry = exif.get(0x9011);
  return parseExifDate(ascii(when), offsetEntry === undefined ? null : ascii(offsetEntry));
}

/** "2025:06:14 18:32:10" with an offset such as "+02:00", or as this browser's time without. */
export function parseExifDate(text: string, offset: string | null): number | null {
  const m = /^(\d{4}):(\d{2}):(\d{2}) (\d{2}):(\d{2}):(\d{2})/.exec(text);
  if (!m) return null;
  const [y, mo, d, h, mi, s] = m.slice(1).map(Number);
  if (y < 1900 || mo < 1 || mo > 12 || d < 1 || d > 31) return null;
  const o = offset ? /^([+-])(\d{2}):(\d{2})$/.exec(offset) : null;
  if (o) {
    const minutes = (Number(o[2]) * 60 + Number(o[3])) * (o[1] === '-' ? -1 : 1);
    return Date.UTC(y, mo - 1, d, h, mi, s) - minutes * 60_000;
  }
  return new Date(y, mo - 1, d, h, mi, s).getTime();
}
