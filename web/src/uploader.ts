// Uppy runs headless underneath our own screens: tus for resumable uploads in chunks the
// server chooses (below Cloudflare's 100 MB request limit), and Golden Retriever to bring the
// queue back after the page was closed.
import Uppy, { type Body, type Meta, type UppyFile } from '@uppy/core';
import GoldenRetriever, { type GoldenRetrieverOptions } from '@uppy/golden-retriever';
import Tus from '@uppy/tus';
import type { Info } from './api';
import { dedupeBatches, matchGhosts, unbatched } from './restore';
import { ThumbQueue } from './thumbs';

export type Kind = 'photo' | 'video' | 'document';
/** ghost: came back after the page closed, but without its data; it must be picked again. */
export type TileState = 'queued' | 'uploading' | 'done' | 'error' | 'ghost';

export interface Tile {
  id: string;
  name: string;
  type: string;
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
  ghosts: Tile[];
  bytesTotal: number;
  bytesDone: number;
}

export interface UploaderEvents {
  onChange(): void;
  onAllDone(files: number, bytes: number): void;
  /** The PIN session ended (401): everything is paused until a new unlock. */
  onSessionEnded(): void;
  onRejected(name: string): void;
  /** A queue the closed page interrupted came back and waits for continue(). */
  onRestored(): void;
}

const MiB = 1 << 20;
/** Golden Retriever says nothing when it finds no saved queue, so waiting for it ends after this. */
const restoreWait = 1500;

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

/**
 * Only one tab brings back and continues the saved queue: two tabs sending the same uploads,
 * or one starting over under the other, would spoil both. The lock is held while the page is
 * open. Other tabs send what is picked in them, without keeping it across a closed page.
 */
export function holdQueueLock(): Promise<boolean> {
  const locks = navigator.locks;
  if (!locks) return Promise.resolve(true);
  return new Promise((resolve) => {
    locks
      .request('share-queue', { ifAvailable: true }, (lock) => {
        resolve(lock !== null);
        return lock ? new Promise<void>(() => {}) : undefined;
      })
      .catch(() => resolve(true));
  });
}

export class Uploader {
  readonly uppy: Uppy<Meta, Body>;
  private sessionEnded = false;
  private doneReported = false;
  private frame = 0;
  /** Golden Retriever is still looking for a saved queue; files picked meanwhile wait. */
  private restoring: boolean;
  private pickedWhileRestoring: File[] = [];
  /** Files were picked on this page, not only restored. */
  private addedHere = false;
  /** A queue came back and continue() wasn't called yet: nothing may start meanwhile. */
  private waitingToContinue = false;
  /** Files picked on screen 5 that matched no ghost; they start with continue(). */
  private heldBack: File[] = [];
  private readonly thumbs = new ThumbQueue();

  /** keepQueue: this tab keeps the queue across a closed page (see holdQueueLock). */
  constructor(info: Info, private readonly events: UploaderEvents, keepQueue: boolean) {
    this.uppy = new Uppy<Meta, Body>({
      id: 'share',
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

    this.restoring = keepQueue;
    if (keepQueue) {
      // IndexedDB keeps photos and documents up to 20 MiB across a closed page; bigger files
      // (most videos) survive only in the service worker's memory, and otherwise have to be
      // picked again. The service worker store waits for a controlling worker, so it is only
      // used when one controls the page (not on the first visit or after a hard reload).
      const serviceWorker = !!navigator.serviceWorker?.controller;
      const indexedDB = { maxFileSize: 20 * MiB, maxTotalSize: 500 * MiB };
      this.uppy.use(GoldenRetriever, {
        serviceWorker,
        expires: 7 * 24 * 60 * 60 * 1000, // as long as the server keeps unfinished uploads
        // Passed on to the IndexedDB store; the type only lists some of its options.
        indexedDB: indexedDB as GoldenRetrieverOptions['indexedDB'],
      });
      const stopWaiting = setTimeout(() => this.restored(false), restoreWait);
      this.uppy.on('restored', () => {
        clearTimeout(stopWaiting);
        this.restored(!!this.uppy.getState().recoveredState);
      });
      if (serviceWorker) this.keepServiceWorkerAwake();
    }

    this.uppy.on('file-added', (file) => {
      this.doneReported = false;
      const data = file.data;
      if (data instanceof File) this.uppy.setFileMeta(file.id, { lastModified: String(data.lastModified) });
    });
    this.uppy.on('restriction-failed', (file) => events.onRejected(file?.name ?? ''));
    this.uppy.on('state-update', () => this.changed());
    this.uppy.on('upload-success', (file, response) => {
      if (file) this.thumbnail(file, response.uploadURL);
      this.checkDone();
    });
    this.uppy.on('complete', () => this.checkDone());
  }

  /** Queues files; they start right away. A file matching a ghost continues its upload. */
  add(files: File[]): void {
    if (files.length === 0) return;
    if (this.restoring) {
      this.pickedWhileRestoring.push(...files);
      return;
    }
    const ghosts = this.uppy.getFiles().filter((f) => f.isGhost);
    const { matched, rest } = matchGhosts(
      ghosts.map((g) => ({ id: g.id, name: g.name ?? '', size: g.size ?? 0, type: g.type ?? '' })),
      files,
    );
    for (const [id, file] of matched) {
      this.uppy.setFileState(id, { data: file, isGhost: false, error: null });
      // Before continue() its restored batch takes it along. Afterwards that batch failed it
      // for lack of data, so it starts again, at the offset the server has.
      if (!this.waitingToContinue) this.uppy.retryUpload(id).catch(() => {});
    }
    if (this.waitingToContinue) {
      this.heldBack.push(...rest);
      return;
    }
    if (rest.length > 0) this.addedHere = true;
    for (const f of rest) {
      try {
        this.uppy.addFile({ name: f.name, type: f.type, data: f, source: 'Local' });
      } catch {
        // Restrictions (too large) and duplicates are reported through events or ignored.
      }
    }
  }

  /** Screen 5: continue the queue that came back. */
  continue(): void {
    this.waitingToContinue = false;
    this.uppy.setState({ currentUploads: dedupeBatches(this.uppy.getState().currentUploads) });
    this.uppy.emit('restore-confirmed');
    // Files that failed before the page closed are in no batch any more, and Golden
    // Retriever's resumeAll() (or picking them again) cleared their error, so nothing else
    // would start them.
    for (const id of unbatched(this.uppy.getFiles(), this.uppy.getState().currentUploads)) {
      this.uppy.retryUpload(id).catch(() => {});
    }
    const held = this.heldBack;
    this.heldBack = [];
    this.add(held);
  }

  /** Screen 5: drop the queue that came back, and its unfinished parts on the server. */
  startOver(): void {
    for (const f of this.uppy.getFiles()) this.terminate(f);
    this.clear();
  }

  /** Gives up the files that would have to be picked again; the rest carries on. */
  skipGhosts(): void {
    for (const f of this.uppy.getFiles()) {
      if (!f.isGhost) continue;
      this.terminate(f);
      this.uppy.removeFile(f.id);
    }
    this.checkDone();
  }

  /** After a new PIN: continue where the uploads stopped. */
  resume(): void {
    this.sessionEnded = false;
    if (this.waitingToContinue) return; // they came back after the page closed: screen 5 decides
    // The upload that got the 401 has failed, the others were paused. resumeAll() also clears
    // the error of started uploads (so retryAll() would find nothing), so note the failed first.
    const failed = this.uppy.getFiles().filter((f) => f.error && !f.isGhost).map((f) => f.id);
    this.uppy.resumeAll();
    for (const id of failed) this.uppy.retryUpload(id).catch(() => {});
  }

  retryFailed(): void {
    for (const f of this.uppy.getFiles()) {
      if (f.error && !f.isGhost) this.uppy.retryUpload(f.id).catch(() => {});
    }
  }

  /** Forgets all files, for "Send more files". */
  clear(): void {
    this.doneReported = false;
    this.waitingToContinue = false;
    this.heldBack = [];
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
    const ghosts = tiles.filter((t) => t.state === 'ghost');
    return { tiles, total: tiles.length, done, failed, ghosts, bytesTotal, bytesDone };
  }

  /** Golden Retriever finished looking for a saved queue, or waiting for it ended. */
  private restored(found: boolean): void {
    const waited = this.restoring;
    this.restoring = false;
    const picked = this.pickedWhileRestoring;
    this.pickedWhileRestoring = [];
    if (found && (this.addedHere || picked.length > 0)) {
      // New files are chosen already: send them together with the queue that came back.
      this.heldBack.push(...picked);
      this.continue();
    } else if (found) {
      this.waitingToContinue = true;
      this.events.onRestored();
    } else if (waited) {
      this.add(picked);
    }
  }

  /** Removes an unfinished upload from the server. */
  private terminate(f: UppyFile<Meta, Body>): void {
    const url = f.tus?.uploadUrl;
    if (!url || f.progress.uploadComplete) return;
    fetch(url, { method: 'DELETE', headers: { 'Tus-Resumable': '1.0.0' }, credentials: 'same-origin', keepalive: true }).catch(
      () => {},
    );
  }

  /**
   * The service worker keeps big files in memory across a reload, but the browser stops an
   * idle worker after about 30 seconds, and on a slow line one 20 MiB piece takes longer. A
   * message now and then keeps it running while files are unfinished. It asks Golden
   * Retriever's part of the worker for an unused store, which only answers.
   */
  private keepServiceWorkerAwake(): void {
    setInterval(() => {
      if (!this.uppy.getFiles().some((f) => !f.progress.uploadComplete)) return;
      navigator.serviceWorker.controller?.postMessage({ type: 'uppy/GET_FILES', store: 'share-keepalive' });
    }, 20_000);
  }

  private thumbnail(file: UppyFile<Meta, Body>, uploadURL: string | undefined): void {
    const kind = kindOf(file.name ?? '', file.type ?? '');
    const id = uploadURL?.split('/').pop();
    if ((kind === 'photo' || kind === 'video') && id && file.data instanceof Blob) this.thumbs.add(id, file.data, kind);
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
  const state: TileState = f.isGhost
    ? 'ghost'
    : f.error
      ? 'error'
      : p.uploadComplete
        ? 'done'
        : p.uploadStarted
          ? 'uploading'
          : 'queued';
  const name = f.name ?? '';
  const type = f.type ?? '';
  return { id: f.id, name, type, kind: kindOf(name, type), ext: extOf(name), size, uploaded, state };
}
