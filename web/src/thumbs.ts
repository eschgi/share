// Thumbnails for the library, made in the browser right after a photo or video is sent. The
// server can't read videos, and the phone shows every photo format it can decode itself.
// They go to the server one at a time, and a failure only means the file has no thumbnail
// (for photos the server then makes one). An encrypted file's goes up sealed with its key.
import { sealedThumb, type FileSeal } from './e2ee/upload';

const side = 512; // the longer side, in pixels
const quality = 0.8;
// Phones take photos of up to 200 MP. A computer's browser decodes that many pixels in about a
// second, with 4 bytes each; Chrome on a phone decodes big photos smaller when memory is short.
const maxPixels = 200_000_000;
const videoTimeout = 15_000;

export interface Thumb {
  jpeg: Blob;
  width: number; // of the original
  height: number;
  durationMs?: number;
}

export type ThumbKind = 'photo' | 'video';

export function makeThumb(data: Blob, kind: ThumbKind): Promise<Thumb | null> {
  return kind === 'photo' ? photoThumb(data) : videoThumb(data);
}

async function photoThumb(data: Blob): Promise<Thumb | null> {
  const url = URL.createObjectURL(data);
  try {
    const img = new Image();
    img.decoding = 'async';
    await new Promise((resolve, reject) => {
      img.onload = resolve;
      img.onerror = reject;
      img.src = url;
    });
    // The browser turns the photo upright by its EXIF orientation, both here and when drawing.
    const { naturalWidth: w, naturalHeight: h } = img;
    if (!w || !h || w * h > maxPixels) return null;
    const jpeg = await draw(img, w, h);
    return jpeg && { jpeg, width: w, height: h };
  } catch {
    return null; // a format this browser can't show, e.g. HEIC on most phones
  } finally {
    URL.revokeObjectURL(url);
  }
}

async function videoThumb(data: Blob): Promise<Thumb | null> {
  const url = URL.createObjectURL(data);
  const video = document.createElement('video');
  try {
    video.muted = true;
    video.playsInline = true;
    video.preload = 'auto';
    video.src = url;
    await event(video, 'loadeddata');
    const durationMs = Number.isFinite(video.duration) ? Math.round(video.duration * 1000) : undefined;
    // A moment in, rather than the first frame, which is often black.
    const at = Number.isFinite(video.duration) ? Math.min(1, video.duration / 10) : 0;
    if (at > 0) {
      video.currentTime = at;
      await event(video, 'seeked');
    }
    const { videoWidth: w, videoHeight: h } = video;
    if (!w || !h) return null;
    const jpeg = await draw(video, w, h);
    return jpeg && { jpeg, width: w, height: h, durationMs };
  } catch {
    return null;
  } finally {
    video.removeAttribute('src');
    video.load(); // lets go of the file
    URL.revokeObjectURL(url);
  }
}

function event(el: HTMLMediaElement, name: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => done(new Error('timeout')), videoTimeout);
    const ok = () => done();
    const fail = () => done(new Error('error'));
    function done(err?: Error) {
      clearTimeout(timer);
      el.removeEventListener(name, ok);
      el.removeEventListener('error', fail);
      if (err) reject(err);
      else resolve();
    }
    el.addEventListener(name, ok);
    el.addEventListener('error', fail);
  });
}

function draw(source: CanvasImageSource, w: number, h: number): Promise<Blob | null> {
  const scale = Math.min(1, side / Math.max(w, h));
  const canvas = document.createElement('canvas');
  canvas.width = Math.max(1, Math.round(w * scale));
  canvas.height = Math.max(1, Math.round(h * scale));
  const ctx = canvas.getContext('2d');
  if (!ctx) return Promise.resolve(null);
  ctx.imageSmoothingQuality = 'high';
  ctx.drawImage(source, 0, 0, canvas.width, canvas.height);
  return new Promise((resolve) => canvas.toBlob(resolve, 'image/jpeg', quality));
}

/** Makes and sends thumbnails one after the other, so a big batch doesn't fill the memory. */
export class ThumbQueue {
  private tail: Promise<void> = Promise.resolve();

  /** seal: an encrypted file's, whose thumbnail goes up sealed with its key. */
  add(fileId: string, data: Blob, kind: ThumbKind, seal: FileSeal | null = null): void {
    this.tail = this.tail.then(() => this.send(fileId, data, kind, seal)).catch(() => {});
  }

  private async send(fileId: string, data: Blob, kind: ThumbKind, seal: FileSeal | null): Promise<void> {
    const thumb = await makeThumb(data, kind);
    if (!thumb) return;
    const q = new URLSearchParams({ width: String(thumb.width), height: String(thumb.height) });
    if (thumb.durationMs !== undefined) q.set('duration_ms', String(thumb.durationMs));
    await fetch(`/api/files/${encodeURIComponent(fileId)}/thumb?${q}`, {
      method: 'PUT',
      headers: { 'Content-Type': seal ? 'application/octet-stream' : 'image/jpeg' },
      body: seal ? await sealedThumb(seal, thumb.jpeg) : thumb.jpeg,
      credentials: 'same-origin',
    });
  }
}
