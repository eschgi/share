// Uppy runs headless underneath our own screens: tus for resumable uploads in chunks the
// server chooses (below Cloudflare's 100 MB request limit), or with the files in a bucket our
// S3 uploader, which sends the parts straight there; and Golden Retriever to bring the queue
// back after the page was closed.
import Uppy, { type Body, type Meta, type UppyFile } from '@uppy/core';
import GoldenRetriever, { type GoldenRetrieverOptions } from '@uppy/golden-retriever';
import Tus from '@uppy/tus';
import { errorCode, type Info } from './api';
import { SendRefused, type FolderPublicKey } from './e2ee/trust';
import { encOf, encryptingReader, newSeal, parseSeal, remember } from './e2ee/upload';
import type { Shared } from './inbox';
import S3Upload, { abortS3Upload } from './s3upload';
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
  /**
   * The PIN session ended (401): everything is paused until a new unlock. lost: the server
   * got no session at all (the browser lost or never kept its cookie), not one whose PIN ended.
   */
  onSessionEnded(lost: boolean): void;
  onRejected(name: string): void;
  /** A queue the closed page interrupted came back and waits for continue(). */
  onRestored(): void;
  /** A file shared from another app was sent, or given up: it can leave the inbox. */
  onSharedGone?(key: number): void;
  /** The key a folder's new files are encrypted for (signed in: the folder said; a PIN: its
   * own), signed by the root; null while they go plain. Throws SendRefused when nothing may go
   * into the folder: what the server says about its keys can't be checked. */
  encryptFor?(folder: string | undefined): Promise<FolderPublicKey | null>;
  /** Files weren't sent: what the server says about their folder's keys can't be checked. */
  onRefused?(): void;
  /** The folder's key changed meanwhile (a new version, or it was encrypted): ask again. */
  refreshKeys?(): Promise<void>;
  /** Nothing is on its way any more, and some files didn't go. */
  onFailed?(failed: number): void;
  /** Signed in: the folder a file was to go into is gone, or the person doesn't see it any more. */
  onFolderGone?(): void;
}

/** What a file's upload says about it besides its name and type: the inbox key of a file
 * shared from another app, and the folder it goes into (signed in; a PIN has its own). enc
 * stays empty unless the file goes into an encrypted folder: tus sends every allowed field. */
export function uploadMeta(shareKey: number | undefined, folder: string | undefined): Meta {
  const meta: Meta = { enc: '' };
  if (shareKey !== undefined) meta.shareKey = String(shareKey);
  if (folder !== undefined) meta.folder = folder;
  return meta;
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
  /** How many failed files were last reported, so each change is told once. */
  private failedReported = 0;
  /** A redraw is due with the next frame (or timer, see changed()). */
  private redrawDue = false;
  /** Golden Retriever is still looking for a saved queue; files picked meanwhile wait. */
  private restoring: boolean;
  private pickedWhileRestoring: File[] = [];
  /** Files were picked on this page, not only restored. */
  private addedHere = false;
  /** A queue came back and continue() wasn't called yet: nothing may start meanwhile. */
  private waitingToContinue = false;
  /** Files picked on screen 5 that matched no ghost; they start with continue(). */
  private heldBack: File[] = [];
  /** Files shared from other apps, with their key in the inbox. */
  private readonly shared = new WeakMap<File, number>();
  /** Signed in: the folder each file goes into. */
  private readonly folders = new WeakMap<File, string>();
  /** Files whose folder was gone when they started; trying again needs another folder. */
  private readonly folderless = new Set<string>();
  /** A refusal for a gone folder just came in, for the next upload-error. */
  private folderGone = false;
  /** A refusal because the folder's key changed came in: the file gets a new seal. */
  private reseal = false;
  /** How often each file was sealed anew, which ends after a few tries. */
  private resealed = new Map<string, number>();
  private readonly thumbs = new ThumbQueue();

  /** keepQueue: this tab keeps the queue across a closed page (see holdQueueLock). */
  constructor(info: Info, private readonly events: UploaderEvents, keepQueue: boolean) {
    this.uppy = new Uppy<Meta, Body>({
      id: 'share',
      autoProceed: true,
      allowMultipleUploadBatches: true,
      restrictions: { maxFileSize: info.max_file_size_bytes > 0 ? info.max_file_size_bytes : null },
    });
    const retryDelays = [0, 1000, 3000, 5000, 10000, 20000, 30000, 60000, 60000, 60000];
    if (info.storage === 's3') {
      this.uppy.use(S3Upload, { limit: 3, retryDelays, refused: (status, code) => this.refused(status, code) });
    } else {
      this.uppy.use(Tus, {
        endpoint: new URL('/tus/', location.href).href,
        chunkSize: info.chunk_size_bytes,
        limit: 3,
        retryDelays,
        removeFingerprintOnSuccess: true,
        allowedMetaFields: ['name', 'type', 'lastModified', 'folder', 'enc'],
        // The encrypted stream for files into an encrypted folder (e2ee/upload.ts).
        fileReader: encryptingReader,
        onShouldRetry: (err, _attempt, _opts, next) => {
          const status = err.originalResponse?.getStatus() ?? 0;
          return this.refused(status, errorCode(err.originalResponse?.getBody())) ? false : next(err);
        },
      });
    }

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

    // Into an encrypted folder, each file gets its key just before its upload starts, and keeps
    // it across tries and a closed page. A file whose folder's keys can't be checked fails here,
    // and leaves the batch; trying it again checks again.
    this.uppy.addPreProcessor(async (ids, uploadID) => {
      const refused: string[] = [];
      for (const id of ids) {
        const f = this.uppy.getFile(id);
        if (!f || !(f.data instanceof Blob)) continue;
        let seal = parseSeal(f.meta.e2ee);
        if (!seal) {
          let target: FolderPublicKey | null = null;
          try {
            target = (await events.encryptFor?.(typeof f.meta.folder === 'string' ? f.meta.folder : undefined)) ?? null;
          } catch (e) {
            if (!(e instanceof SendRefused)) throw e;
            refused.push(id);
            continue;
          }
          if (!target) continue;
          seal = await newSeal(target, f.data.size, String(f.meta.lastModified ?? ''));
          this.uppy.setFileMeta(id, { e2ee: JSON.stringify(seal), enc: JSON.stringify(encOf(seal)) });
        }
        remember(f.data, seal);
      }
      if (refused.length === 0) return;
      const { currentUploads } = this.uppy.getState();
      const upload = currentUploads[uploadID];
      if (upload) this.uppy.setState({ currentUploads: { ...currentUploads, [uploadID]: { ...upload, fileIDs: upload.fileIDs.filter((id) => !refused.includes(id)) } } });
      for (const id of refused) {
        const f = this.uppy.getFile(id);
        if (f) this.uppy.emit('upload-error', f, new Error("the folder's keys can't be checked"));
      }
      events.onRefused?.();
    });

    this.uppy.on('file-added', (file) => {
      this.doneReported = false;
      const data = file.data;
      if (data instanceof File) this.uppy.setFileMeta(file.id, { lastModified: String(data.lastModified) });
    });
    this.uppy.on('restriction-failed', (file) => {
      // Duplicates come this way too: a file picked or dropped twice is simply skipped.
      const max = info.max_file_size_bytes;
      if (file && max > 0 && (file.size ?? 0) > max) {
        events.onRejected(file.name ?? '');
        const key = file.data instanceof File ? this.shared.get(file.data) : undefined;
        if (key !== undefined) events.onSharedGone?.(key);
      }
    });
    this.uppy.on('state-update', () => this.changed());
    this.uppy.on('upload-success', (file, response) => {
      if (file) this.thumbnail(file, response.uploadURL);
      const key = file?.meta.shareKey;
      if (typeof key === 'string') events.onSharedGone?.(Number(key));
      this.checkDone();
    });
    this.uppy.on('complete', () => {
      this.checkDone();
      this.checkFailed();
    });
    this.uppy.on('upload-error', (file) => {
      if (this.folderGone && file) {
        this.folderGone = false;
        this.folderless.add(file.id);
        events.onFolderGone?.();
      }
      const tries = file ? (this.resealed.get(file.id) ?? 0) : 0;
      if (this.reseal && file && tries < 3) {
        // Sealed for an older key, or plain into a folder now encrypted: a new seal, a new upload.
        this.reseal = false;
        this.resealed.set(file.id, tries + 1);
        this.uppy.setFileMeta(file.id, { e2ee: undefined, enc: '' });
        this.uppy.setFileState(file.id, { s3: undefined, tus: undefined });
        void (events.refreshKeys?.() ?? Promise.resolve()).then(() => this.uppy.retryUpload(file.id).catch(() => {}));
        return;
      }
      this.reseal = false;
      this.checkFailed();
    });
  }

  /** Queues files shared from other apps; each leaves the inbox once it is sent. */
  addShared(files: Shared[], folder?: string): void {
    for (const s of files) this.shared.set(s.file, s.key);
    this.add(files.map((s) => s.file), folder);
  }

  /** Queues files, into a folder when signed in; they start right away. A file matching a
   * ghost continues its upload. */
  add(files: File[], folder?: string): void {
    if (files.length === 0) return;
    if (folder !== undefined) for (const f of files) this.folders.set(f, folder);
    if (this.restoring) {
      this.pickedWhileRestoring.push(...files);
      return;
    }
    const ghosts = this.uppy.getFiles().filter((f) => f.isGhost);
    const { matched: found, rest } = matchGhosts(
      ghosts.map((g) => ({ id: g.id, name: g.name ?? '', size: g.size ?? 0, type: g.type ?? '' })),
      files,
    );
    // An encrypted upload goes on only with the very file it began with: with the same key, a
    // changed file would spoil both. One changed since starts over.
    const matched: typeof found = [];
    for (const [id, file] of found) {
      const seal = parseSeal(this.uppy.getFile(id)?.meta.e2ee);
      if (seal && seal.lastModified !== String(file.lastModified)) {
        const ghost = this.uppy.getFile(id);
        if (ghost) this.terminate(ghost);
        this.uppy.removeFile(id);
        rest.push(file);
      } else {
        matched.push([id, file]);
      }
    }
    for (const [id, file] of matched) {
      this.uppy.setFileState(id, { data: file, isGhost: false, error: null });
      this.markShared(id, file);
      // Before continue() its restored batch takes it along. Afterwards that batch failed it
      // for lack of data, so it starts again, at the offset the server has.
      if (!this.waitingToContinue) this.uppy.retryUpload(id).catch(() => {});
    }
    if (this.waitingToContinue) {
      this.heldBack.push(...rest);
      return;
    }
    if (rest.length === 0) return;
    this.addedHere = true;
    try {
      // All at once: one by one, a dropped folder of thousands of files gets slow, as each one
      // copies the whole list. Files that are too large or already there are left out.
      this.uppy.addFiles(rest.map((f) => ({ name: f.name, type: f.type, data: f, source: 'Local', meta: this.metaOf(f) })));
    } catch {
      // Only for errors other than restrictions; then none of the files were added.
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
    for (const f of this.uppy.getFiles()) {
      this.terminate(f);
      const key = f.meta.shareKey;
      if (typeof key === 'string' && !f.progress.uploadComplete) this.events.onSharedGone?.(Number(key));
    }
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

  /** Tries the failed files again; those whose folder was gone go into folder now. */
  retryFailed(folder?: string): void {
    for (const f of this.uppy.getFiles()) {
      if (!f.error || f.isGhost) continue;
      if (this.folderless.delete(f.id) && folder !== undefined) {
        this.uppy.setFileMeta(f.id, { folder });
        this.uppy.setFileState(f.id, { s3: undefined }); // a bucket's upload belongs to its folder
      }
      this.uppy.retryUpload(f.id).catch(() => {});
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

  /** A file's meta, which also comes back after a closed page: its inbox key if it was
   * shared, and its folder. */
  private metaOf(f: File): Meta {
    return uploadMeta(this.shared.get(f), this.folders.get(f));
  }

  private markShared(id: string, f: File): void {
    const key = this.shared.get(f);
    if (key !== undefined) this.uppy.setFileMeta(id, { shareKey: String(key) });
  }

  /** Removes an unfinished upload from the server. */
  private terminate(f: UppyFile<Meta, Body>): void {
    if (f.progress.uploadComplete) return;
    if (f.s3) {
      abortS3Upload(f.s3.id);
      return;
    }
    const url = f.tus?.uploadUrl;
    if (!url) return;
    fetch(url, { method: 'DELETE', headers: { 'Tus-Resumable': '1.0.0' }, credentials: 'same-origin', keepalive: true }).catch(
      () => {},
    );
  }

  /** An answer that ends a file, over tus or into the bucket: true when trying again won't
   * help. A 401 ends the session for every file; a gone folder asks for another one. */
  private refused(status: number, code: string | undefined): boolean {
    if (status === 401) {
      this.endSession(code !== 'session_ended');
      return true;
    }
    if (status === 404 && code === 'folder_gone') {
      this.folderGone = true;
      return true;
    }
    if (status === 409 && (code === 'key_outdated' || code === 'encryption_required' || code === 'not_encrypted')) {
      this.reseal = true;
      return true;
    }
    // Full drive, too large, too many: retrying won't help.
    return status === 403 || status === 413;
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
    if ((kind === 'photo' || kind === 'video') && id && file.data instanceof Blob) this.thumbs.add(id, file.data, kind, parseSeal(file.meta.e2ee));
  }

  private endSession(lost: boolean): void {
    if (this.sessionEnded) return;
    this.sessionEnded = true;
    this.uppy.pauseAll();
    this.events.onSessionEnded(lost);
  }

  private changed(): void {
    if (this.redrawDue) return;
    this.redrawDue = true;
    const redraw = () => {
      if (!this.redrawDue) return;
      this.redrawDue = false;
      this.events.onChange();
    };
    // With the next frame. Frames stop while the tab is in the background, where its title
    // still shows the progress, so a timer stands in.
    requestAnimationFrame(redraw);
    setTimeout(redraw, 1000);
  }

  private checkFailed(): void {
    const files = this.uppy.getFiles().filter((f) => !f.isGhost);
    const failed = files.filter((f) => f.error).length;
    if (failed === 0) this.failedReported = 0;
    if (failed === 0 || failed === this.failedReported || !files.every((f) => f.progress.uploadComplete || f.error)) return;
    this.failedReported = failed;
    this.events.onFailed?.(failed);
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
