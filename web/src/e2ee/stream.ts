// What a page tells the service worker to stream an encrypted file decrypted (sw.ts,
// e2ee/files.ts), and the byte ranges both sides cut streams to.
import type { Bytes } from './bytes';

export interface StreamOrder {
  type: 'e2ee-stream';
  token: string;
  /** Where the encrypted bytes are: the file's /content, which answers ranges. */
  src: string;
  /** The plain size. */
  size: number;
  header: Bytes;
  /** The file key. */
  key: Bytes;
  name: string;
  mime: string;
  attachment: boolean;
}

/** Drops skip bytes, passes length bytes, and drops the rest. */
export function byteRange(skip: number, length: number): TransformStream<Uint8Array, Uint8Array> {
  let pos = 0;
  return new TransformStream({
    transform(part, out) {
      const from = Math.max(0, skip - pos);
      const to = Math.min(part.length, skip + length - pos);
      if (to > from) out.enqueue(part.subarray(from, to));
      pos += part.length;
    },
  });
}

/** A Range header's span of a file of size bytes: [start, end), or null for none or a bad one. */
export function parseRange(header: string | null, size: number): { start: number; end: number } | null | 'unsatisfiable' {
  const m = /^bytes=(\d*)-(\d*)$/.exec(header ?? '');
  if (!m || (m[1] === '' && m[2] === '')) return null;
  if (m[1] === '') return { start: Math.max(0, size - Number(m[2])), end: size };
  const start = Number(m[1]);
  const end = m[2] === '' ? size : Math.min(size, Number(m[2]) + 1);
  if (start >= size || start >= end) return 'unsatisfiable';
  return { start, end };
}
