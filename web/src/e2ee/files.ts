// Encrypted files, decrypted on this device (docs/e2ee-plan.md): thumbnails and photos into
// blob URLs, videos and downloads through the service worker, which decrypts while it streams
// and answers ranges, so a video seeks. Without a service worker the page decrypts into memory.
import { contentPath, thumbUrl, type FileEnc, type FileInfo } from '../api';
import { b64u, fromB64u, randomBytes } from './bytes';
import { ContentCipher, decryptFile } from './content';
import { openThumb, SealError } from './formats';
import { keyring } from './keyring';
import type { StreamOrder } from './stream';

type Encrypted = FileInfo & { enc: FileEnc };

/** What decrypting a file needs to know of it. */
export interface Sealed {
  id: string;
  folder: string;
  enc: FileEnc;
  size: number;
  name: string;
  mime: string;
}

export const isEncrypted = (f: FileInfo): f is Encrypted => !!f.enc;

/** The width and height a JPEG says it has, from its frame header; null for anything else.
 * A sealed thumbnail can't be checked by the server, so it is checked here before it is drawn. */
export function jpegSize(b: Uint8Array): { width: number; height: number } | null {
  if (b.length < 4 || b[0] !== 0xff || b[1] !== 0xd8) return null;
  let i = 2;
  while (i + 9 < b.length) {
    if (b[i] !== 0xff) return null;
    const marker = b[i + 1];
    if (marker === 0xd8 || marker === 0x01 || (marker >= 0xd0 && marker <= 0xd7)) {
      i += 2;
      continue;
    }
    const len = (b[i + 2] << 8) | b[i + 3];
    // Start of frame: baseline, progressive and the rest, but not DHT (c4), JPG (c8), DAC (cc).
    if (marker >= 0xc0 && marker <= 0xcf && marker !== 0xc4 && marker !== 0xc8 && marker !== 0xcc) {
      return { height: (b[i + 5] << 8) | b[i + 6], width: (b[i + 7] << 8) | b[i + 8] };
    }
    i += 2 + len;
  }
  return null;
}

/** The biggest thumbnail anyone makes: 512 pixels a side today, 1024 at most. */
const maxThumbSide = 1024;

const thumbUrls = new Map<string, Promise<string>>();

/** The decrypted thumbnail of an encrypted file, as a blob URL, kept for the page's life
 * (the newest few hundred). Throws while its folder's key isn't open here. */
export function thumbBlobUrl(f: Encrypted): Promise<string> {
  const id = `${f.id}:${f.updated_at}`;
  let url = thumbUrls.get(id);
  if (!url) {
    url = (async () => {
      const res = await fetch(thumbUrl(f), { credentials: 'same-origin' });
      if (!res.ok) throw new Error(`thumbnail: ${res.status}`);
      const jpeg = await openThumb(await keyring.fileKey(f), new Uint8Array(await res.arrayBuffer()));
      const size = jpegSize(jpeg);
      if (!size || size.width > maxThumbSide || size.height > maxThumbSide) throw new SealError('not a thumbnail');
      return URL.createObjectURL(new Blob([jpeg], { type: 'image/jpeg' }));
    })();
    url.catch(() => thumbUrls.delete(id));
    thumbUrls.set(id, url);
    if (thumbUrls.size > 400) {
      const [oldest, gone] = thumbUrls.entries().next().value!;
      thumbUrls.delete(oldest);
      gone.then(URL.revokeObjectURL, () => {});
    }
  }
  return url;
}

/** An encrypted file's contents, decrypted, e.g. a photo for the viewer. */
export async function decryptedBlob(f: Sealed, signal?: AbortSignal): Promise<Blob> {
  const res = await fetch(contentPath(f.id), { credentials: 'same-origin', signal });
  if (!res.ok) throw new Error(`download: ${res.status}`);
  const cipher = await ContentCipher.create(await keyring.fileKey(f), fromB64u(f.enc.header), f.size);
  const plain = await decryptFile(cipher, new Uint8Array(await res.arrayBuffer()));
  return new Blob([plain], { type: f.mime || 'application/octet-stream' });
}

/** Tells the service worker about a file and waits until it knows it. */
function order(sw: ServiceWorker, o: StreamOrder): Promise<boolean> {
  return new Promise((resolve) => {
    const ch = new MessageChannel();
    const timer = setTimeout(() => resolve(false), 3000);
    ch.port1.onmessage = () => {
      clearTimeout(timer);
      resolve(true);
    };
    sw.postMessage(o, [ch.port2]);
  });
}

let pinging: ReturnType<typeof setInterval> | null = null;

/** The browser stops an idle service worker after about 30 seconds, also while it streams a
 * long download or a video; a message now and then keeps it running while it says a stream is
 * open. */
function keepAwake(sw: ServiceWorker): void {
  if (pinging) return;
  pinging = setInterval(() => {
    const ch = new MessageChannel();
    ch.port1.onmessage = (e) => {
      if (e.data === 0 && pinging) {
        clearInterval(pinging);
        pinging = null;
      }
    };
    sw.postMessage({ type: 'e2ee-ping' }, [ch.port2]);
  }, 20_000);
}

/** An address that gives an encrypted file decrypted: for a video, it seeks; with attachment,
 * it downloads under its name. From the service worker when there is one, else a blob URL of
 * the whole file, decrypted in memory. */
export async function streamUrl(f: Sealed, attachment: boolean): Promise<string> {
  const key = await keyring.fileKey(f);
  const sw = navigator.serviceWorker?.controller;
  if (sw) {
    const token = b64u(randomBytes(18));
    const o: StreamOrder = { type: 'e2ee-stream', token, src: contentPath(f.id), size: f.size, header: fromB64u(f.enc.header), key, name: f.name, mime: f.mime, attachment };
    if (await order(sw, o)) {
      keepAwake(sw);
      return `/e2ee/${token}/${encodeURIComponent(f.name)}`;
    }
  }
  return URL.createObjectURL(await decryptedBlob(f));
}
