// Uppy runs headless underneath our own screens: tus for resumable uploads in chunks the
// server chooses (below Cloudflare's 100 MB request limit).
import Uppy, { type Body, type Meta, type UppyFile } from '@uppy/core';
import Tus from '@uppy/tus';
import type { Info } from './api';

export type Kind = 'photo' | 'video' | 'document';
export type TileState = 'queued' | 'uploading' | 'done' | 'error';

export interface Tile {
  id: string;
  name: string;
  kind: Kind;
  ext: string;
  size: number;
  uploaded: number;
  state: TileState;
}

export interface Snapshot {
  tiles: Tile[];
  total: number;
  done: number;
  failed: number;
  bytesTotal: number;
  bytesDone: number;
}

export interface UploaderEvents {
  onChange(): void;
  onAllDone(files: number, bytes: number): void;
  /** The PIN session ended (401): everything is paused until a new unlock. */
  onSessionEnded(): void;
  onRejected(name: string): void;
}

const videoExt = /\.(mp4|m4v|mov|3gp|mkv|webm|avi)$/i;
const photoExt = /\.(jpe?g|png|gif|webp|heic|heif|avif|bmp|tiff?|dng)$/i;

export function kindOf(name: string, type: string): Kind {
  if (type.startsWith('image/') || photoExt.test(name)) return 'photo';
  if (type.startsWith('video/') || videoExt.test(name)) return 'video';
  return 'document';
}

export function extOf(name: string): string {
  const dot = name.lastIndexOf('.');
  return dot > 0 && name.length - dot <= 6 ? name.slice(dot + 1).toUpperCase() : '';
}

export class Uploader {
  readonly uppy: Uppy<Meta, Body>;
  private sessionEnded = false;
  private doneReported = false;
  private frame = 0;

  constructor(info: Info, private readonly events: UploaderEvents) {
    this.uppy = new Uppy<Meta, Body>({
      autoProceed: true,
      allowMultipleUploadBatches: true,
      restrictions: { maxFileSize: info.max_file_size_bytes > 0 ? info.max_file_size_bytes : null },
    });
    this.uppy.use(Tus, {
      endpoint: new URL('/tus/', location.href).href,
      chunkSize: info.chunk_size_bytes,
      limit: 3,
      retryDelays: [0, 1000, 3000, 5000, 10000, 20000, 30000, 60000, 60000, 60000],
      removeFingerprintOnSuccess: true,
      allowedMetaFields: ['name', 'type', 'lastModified'],
      onShouldRetry: (err, _attempt, _opts, next) => {
        const status = err.originalResponse?.getStatus() ?? 0;
        if (status === 401) {
          this.endSession();
          return false;
        }
        // Full drive, too large, too many: retrying won't help.
        if (status === 403 || status === 413) return false;
        return next(err);
      },
    });

    this.uppy.on('file-added', (file) => {
      this.doneReported = false;
      const data = file.data;
      if (data instanceof File) this.uppy.setFileMeta(file.id, { lastModified: String(data.lastModified) });
    });
    this.uppy.on('restriction-failed', (file) => events.onRejected(file?.name ?? ''));
    this.uppy.on('state-update', () => this.changed());
    this.uppy.on('upload-success', () => this.checkDone());
    this.uppy.on('complete', () => this.checkDone());
  }

  /** Queues files; they start right away. */
  add(files: File[]): void {
    for (const f of files) {
      try {
        this.uppy.addFile({ name: f.name, type: f.type, data: f, source: 'Local' });
      } catch {
        // Restrictions (too large) and duplicates are reported through events or ignored.
      }
    }
  }

  /** After a new PIN: continue where the uploads stopped. */
  resume(): void {
    this.sessionEnded = false;
    // The upload that got the 401 has failed, the others were paused. resumeAll() also clears
    // the error of started uploads (so retryAll() would find nothing), so note the failed first.
    const failed = this.uppy.getFiles().filter((f) => f.error).map((f) => f.id);
    this.uppy.resumeAll();
    for (const id of failed) this.uppy.retryUpload(id).catch(() => {});
  }

  retryFailed(): void {
    void this.uppy.retryAll();
  }

  /** Forgets finished files, for "Send more files". */
  clear(): void {
    this.doneReported = false;
    this.uppy.cancelAll();
  }

  snapshot(): Snapshot {
    const tiles = this.uppy.getFiles().map(tileOf);
    let bytesTotal = 0;
    let bytesDone = 0;
    let done = 0;
    let failed = 0;
    for (const t of tiles) {
      bytesTotal += t.size;
      bytesDone += t.uploaded;
      if (t.state === 'done') done++;
      if (t.state === 'error') failed++;
    }
    return { tiles, total: tiles.length, done, failed, bytesTotal, bytesDone };
  }

  private endSession(): void {
    if (this.sessionEnded) return;
    this.sessionEnded = true;
    this.uppy.pauseAll();
    this.events.onSessionEnded();
  }

  private changed(): void {
    if (this.frame) return;
    this.frame = requestAnimationFrame(() => {
      this.frame = 0;
      this.events.onChange();
    });
  }

  private checkDone(): void {
    const files = this.uppy.getFiles();
    if (this.doneReported || files.length === 0 || !files.every((f) => f.progress.uploadComplete)) return;
    this.doneReported = true;
    this.events.onAllDone(files.length, files.reduce((sum, f) => sum + (f.size ?? 0), 0));
  }
}

function tileOf(f: UppyFile<Meta, Body>): Tile {
  const p = f.progress;
  const size = f.size ?? 0;
  const uploaded = p.uploadComplete ? size : typeof p.bytesUploaded === 'number' ? p.bytesUploaded : 0;
  const state: TileState = f.error ? 'error' : p.uploadComplete ? 'done' : p.uploadStarted ? 'uploading' : 'queued';
  const name = f.name ?? '';
  return { id: f.id, name, kind: kindOf(name, f.type ?? ''), ext: extOf(name), size, uploaded, state };
}
