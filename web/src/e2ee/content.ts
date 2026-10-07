// A file's contents, encrypted in chunks of 64 KiB after a 16-byte header
// (contract/crypto/content.json). The same file key, header and contents always give the same
// bytes, so an upload can send any range of the encrypted file again, and a reader can decrypt any
// range from the chunks around it.
import { concat, randomBytes, type Bytes } from './bytes';
import { hkdf, purposes, SealError } from './formats';

export const chunkSize = 65536;
export const headerSize = 16;
const tagSize = 16;
const sealedChunk = chunkSize + tagSize;

export function newHeader(): Bytes {
  const h = new Uint8Array(headerSize);
  h.set([0x53, 0x48, 0x45, 0x31]); // SHE1
  new DataView(h.buffer).setUint32(4, chunkSize);
  h.set(randomBytes(7), 8);
  return h;
}

export function isHeader(h: Uint8Array): boolean {
  return (
    h.length === headerSize &&
    h[0] === 0x53 &&
    h[1] === 0x48 &&
    h[2] === 0x45 &&
    h[3] === 0x31 &&
    new DataView(h.buffer, h.byteOffset + 4, 4).getUint32(0) === chunkSize &&
    h[15] === 0
  );
}

export const chunksOf = (plainSize: number) => Math.max(1, Math.ceil(plainSize / chunkSize));

export const encryptedSize = (plainSize: number) => headerSize + plainSize + chunksOf(plainSize) * tagSize;

/** Where chunk i starts in the encrypted file. */
export const chunkStart = (i: number) => headerSize + i * sealedChunk;

/** Encrypts and decrypts one file's chunks. */
export class ContentCipher {
  readonly chunks: number;

  private constructor(
    private readonly key: CryptoKey,
    readonly header: Bytes,
    readonly plainSize: number,
  ) {
    this.chunks = chunksOf(plainSize);
  }

  static async create(fileKey: Bytes, header: Bytes, plainSize: number): Promise<ContentCipher> {
    if (!isHeader(header)) throw new SealError('not the header of an encrypted file');
    const raw = await hkdf(fileKey, header, purposes.content);
    const key = await crypto.subtle.importKey('raw', raw, 'AES-GCM', false, ['encrypt', 'decrypt']);
    return new ContentCipher(key, header, plainSize);
  }

  private nonce(i: number): Bytes {
    const n = new Uint8Array(12);
    n.set(this.header.subarray(8, 15));
    new DataView(n.buffer).setUint32(7, i);
    if (i === this.chunks - 1) n[11] = 1;
    return n;
  }

  /** The plaintext's bytes that chunk i holds. */
  plainSpan(i: number): [number, number] {
    return [i * chunkSize, Math.min((i + 1) * chunkSize, this.plainSize)];
  }

  async encrypt(i: number, plain: Uint8Array): Promise<Bytes> {
    return new Uint8Array(await crypto.subtle.encrypt({ name: 'AES-GCM', iv: this.nonce(i) }, this.key, plain as Bytes));
  }

  async decrypt(i: number, ct: Uint8Array): Promise<Bytes> {
    if (i < 0 || i >= this.chunks) throw new SealError('no such chunk');
    try {
      return new Uint8Array(await crypto.subtle.decrypt({ name: 'AES-GCM', iv: this.nonce(i) }, this.key, ct as Bytes));
    } catch {
      throw new SealError("a chunk can't be decrypted");
    }
  }
}

/** Bytes [start, end) of the encrypted file, from plain, the file itself. */
export async function encryptedSlice(cipher: ContentCipher, plain: Blob, start: number, end: number): Promise<Bytes> {
  end = Math.min(end, encryptedSize(cipher.plainSize));
  const parts: Uint8Array[] = [];
  if (start < headerSize) parts.push(cipher.header.subarray(start, Math.min(end, headerSize)));
  if (end > headerSize) {
    const first = Math.floor((Math.max(start, headerSize) - headerSize) / sealedChunk);
    const last = Math.floor((end - 1 - headerSize) / sealedChunk);
    const from = first * chunkSize;
    const data = new Uint8Array(await plain.slice(from, cipher.plainSpan(last)[1]).arrayBuffer());
    for (let i = first; i <= last; i++) {
      const [ps, pe] = cipher.plainSpan(i);
      const ct = await cipher.encrypt(i, data.subarray(ps - from, pe - from));
      const at = chunkStart(i);
      parts.push(ct.subarray(Math.max(0, start - at), Math.min(ct.length, end - at)));
    }
  }
  return concat(...parts);
}

/** The encrypted bytes that hold plain bytes [start, end): whole chunks from first on, and how
 * many decrypted bytes to drop before start. */
export function cipherRange(plainSize: number, start: number, end: number): { first: number; from: number; to: number; skip: number } {
  const first = Math.floor(start / chunkSize);
  const last = Math.max(first, Math.floor((Math.max(end, 1) - 1) / chunkSize));
  return { first, from: chunkStart(first), to: Math.min(chunkStart(last + 1), encryptedSize(plainSize)), skip: start - first * chunkSize };
}

/** Decrypts whole chunks from first on. */
export async function decryptChunks(cipher: ContentCipher, first: number, ct: Uint8Array): Promise<Bytes> {
  const parts: Uint8Array[] = [];
  for (let i = first, at = 0; at < ct.length; i++, at += sealedChunk) parts.push(await cipher.decrypt(i, ct.subarray(at, at + sealedChunk)));
  return concat(...parts);
}

/** Decrypts a whole encrypted file. */
export async function decryptFile(cipher: ContentCipher, data: Uint8Array): Promise<Bytes> {
  if (data.length !== encryptedSize(cipher.plainSize)) throw new SealError('not the whole file');
  return decryptChunks(cipher, 0, data.subarray(headerSize));
}

/** Decrypts a stream of encrypted bytes that holds chunks first to end (not included; the last
 * chunk when not given), giving plaintext. It fails if the stream holds less or more. */
export function decryptStream(cipher: ContentCipher, first: number, end = cipher.chunks): TransformStream<Uint8Array, Uint8Array> {
  let buf = new Uint8Array(0);
  let i = first;
  const length = (i: number) => (i < cipher.chunks - 1 ? sealedChunk : cipher.plainSize - i * chunkSize + tagSize);
  return new TransformStream({
    async transform(part, out) {
      buf = concat(buf, part);
      while (i < end && buf.length >= length(i)) {
        out.enqueue(await cipher.decrypt(i, buf.subarray(0, length(i))));
        buf = buf.slice(length(i++));
      }
      if (i === end && buf.length > 0) throw new SealError('more than the file');
    },
    flush() {
      if (i < end || buf.length > 0) throw new SealError('the file was cut short');
    },
  });
}
