// Sending into an encrypted folder (docs/e2ee-plan.md). Each file gets a key and a header of its
// own, and its key sealed for the folder key's newest version, just before its upload starts.
// They stay with the upload, in its meta, which also comes back after a closed page, until it
// is done: a piece sent again is then the one sent before. tus and the S3 uploader send the
// encrypted stream, which is a little longer than the file.
import { b64u, fromB64u, randomBytes, type Bytes } from './bytes';
import { ContentCipher, encryptedSize, encryptedSlice, newHeader } from './content';
import { folderContext, purposes, sealKey, sealThumb } from './formats';
import type { FolderPublicKey } from './keyring';

/** What an upload keeps of its encryption, as JSON in its meta (e2ee): the file key, never sent,
 * and what the server gets. lastModified tells whether a file picked again is the same one. */
export interface FileSeal {
  key: string;
  header: string;
  folder: string;
  version: number;
  sealed: string;
  size: number;
  lastModified: string;
}

/** A new key and header for a file, its key sealed for the folder's newest key. */
export async function newSeal(target: FolderPublicKey, size: number, lastModified: string): Promise<FileSeal> {
  const key = randomBytes(32);
  const sealed = await sealKey(target.publicKey, purposes.file, folderContext(target.folder, target.version), key);
  return { key: b64u(key), header: b64u(newHeader()), folder: target.folder, version: target.version, sealed: b64u(sealed), size, lastModified };
}

/** The enc the server takes with the upload: tus's metadata, S3's field. */
export function encOf(s: FileSeal): { version: number; key: string; header: string; plain_size: number } {
  return { version: s.version, key: s.sealed, header: s.header, plain_size: s.size };
}

export function parseSeal(meta: unknown): FileSeal | null {
  if (typeof meta !== 'string' || !meta) return null;
  try {
    return JSON.parse(meta) as FileSeal;
  } catch {
    return null;
  }
}

/** The bytes that go up for a file being encrypted. */
export const sealedSize = (s: FileSeal) => encryptedSize(s.size);

const ciphers = new WeakMap<Blob, { seal: FileSeal; cipher: Promise<ContentCipher> }>();

/** Notes that a file's data goes up encrypted, with seal. */
export function remember(data: Blob, seal: FileSeal): void {
  const known = ciphers.get(data);
  if (known?.seal.key === seal.key) return;
  ciphers.set(data, { seal, cipher: ContentCipher.create(fromB64u(seal.key), fromB64u(seal.header), seal.size) });
}

export function sealOf(data: Blob): FileSeal | null {
  return ciphers.get(data)?.seal ?? null;
}

/** Bytes [start, end) of what goes up for data: the encrypted stream when it is encrypted. */
export async function bytesOf(data: Blob, start: number, end: number): Promise<Blob | Bytes> {
  const e = ciphers.get(data);
  if (!e) return data.slice(start, end);
  return encryptedSlice(await e.cipher, data, start, end);
}

/** How many bytes go up for data. */
export function sizeOf(data: Blob): number {
  const e = ciphers.get(data);
  return e ? sealedSize(e.seal) : data.size;
}

/** tus-js-client's file reader: the file as it is, or its encrypted stream. Slices are Blobs,
 * which tus measures by their size. */
export const encryptingReader = {
  async openFile(input: Blob) {
    const size = sizeOf(input);
    const blob = async (start: number, end: number) => {
      const b = await bytesOf(input, start, end);
      return b instanceof Blob ? b : new Blob([b]);
    };
    return {
      size,
      slice: async (start: number, end: number) => ({ value: await blob(start, Math.min(end, size)), done: end >= size }),
      close() {},
    };
  },
};

/** A file's thumbnail, sealed with a key from the file's key. */
export async function sealedThumb(seal: FileSeal, jpeg: Blob): Promise<Blob> {
  const sealed = await sealThumb(fromB64u(seal.key), new Uint8Array(await jpeg.arrayBuffer()));
  return new Blob([sealed], { type: 'application/octet-stream' });
}
