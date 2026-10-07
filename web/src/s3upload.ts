// Sending files straight into the bucket (storage s3), as Uppy's uploader. The server cuts a
// file into parts and hands out a link for each; the parts go to the bucket one after another,
// and the server puts them together. Between tries only the upload's id is kept, in the file's
// state, which Golden Retriever saves: which parts the bucket has always comes from the server,
// so a closed page, a lost network or an expired link carries on where the bucket is.
import { BasePlugin, EventManager, type Body, type DefinePluginOpts, type Meta, type PluginOpts, type Uppy, type UppyFile } from '@uppy/core';
import { filterFilesToEmitUploadStarted, filterFilesToUpload } from '@uppy/core/utils';
import type { S3NewUpload, S3PartUrl, S3PartUrls, S3UploadStatus } from './api';
import { bytesOf, encOf, sealOf, sizeOf } from './e2ee/upload';
import { doneBytes, missingParts, partSpan, s3FailureAction, Slots, stale } from './s3parts';

/** What a file keeps of its upload into the bucket, also across a closed page. */
export interface S3FileState {
  id: string;
  partSize: number;
  parts: number;
}

declare module '@uppy/core/utils' {
  export interface LocalUppyFile<M extends Meta, B extends Body> {
    s3?: S3FileState;
  }
  export interface RemoteUppyFile<M extends Meta, B extends Body> {
    s3?: S3FileState;
  }
}

/** How the uploader reaches the server and the bucket; the tests have their own. */
export interface S3Transport {
  /** A request to Share's API, with a JSON body; status 0 when no answer came. */
  api(method: 'GET' | 'POST' | 'DELETE', path: string, body?: unknown): Promise<{ status: number; body: unknown }>;
  /** A part to the bucket; status 0 when no answer came, or none the page may read. */
  put(url: string, data: Blob, onProgress: (sent: number) => void, signal: AbortSignal): Promise<number>;
  sleep(ms: number, signal: AbortSignal): Promise<void>;
  online(): boolean;
}

export interface S3UploadOptions extends PluginOpts {
  /** At most this many files at once; the parts of one file go one after another. */
  limit?: number;
  /** The pauses before trying again after a failure; when they run out, the file fails. */
  retryDelays?: number[];
  /** Hears a server answer that ends the file (a 401, a gone folder, …), as with tus. */
  refused?: (status: number, code: string | undefined) => void;
  transport?: S3Transport;
}

const defaultOptions = { limit: 3, retryDelays: [0, 1000, 3000, 5000, 10000, 20000, 30000, 60000, 60000, 60000] };

type Opts = DefinePluginOpts<S3UploadOptions, keyof typeof defaultOptions>;

/** A PUT that sends nothing for this long is given up and sent again. */
const stallMs = 60_000;

/** A failed request: who answered (status 0: nobody), and the server's error code. */
export class S3Failure extends Error {
  constructor(
    readonly source: 'bucket' | 'server',
    readonly status: number,
    readonly code?: string,
  ) {
    super(`${source} answered ${status}${code ? ' ' + code : ''}`);
  }
}

export const browserTransport: S3Transport = {
  async api(method, path, body) {
    try {
      const res = await fetch(path, {
        method,
        credentials: 'same-origin',
        // A browser's POST to the API must be JSON, even without anything in it.
        headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
        body: body === undefined ? undefined : JSON.stringify(body),
        keepalive: method === 'DELETE', // a cancel still goes out while the page closes
      });
      let data: unknown = null;
      try {
        data = await res.json();
      } catch {
        // No body (204), or Cloudflare's HTML error pages.
      }
      return { status: res.status, body: data };
    } catch {
      return { status: 0, body: null };
    }
  },
  put(url, data, onProgress, signal) {
    return new Promise((resolve) => {
      const xhr = new XMLHttpRequest();
      let stall: ReturnType<typeof setTimeout> | undefined;
      const watch = () => {
        clearTimeout(stall);
        stall = setTimeout(() => xhr.abort(), stallMs);
      };
      const abort = () => xhr.abort();
      const end = (status: number) => {
        clearTimeout(stall);
        signal.removeEventListener('abort', abort);
        resolve(status);
      };
      xhr.upload.onprogress = (e) => {
        watch();
        onProgress(e.loaded);
      };
      xhr.onload = () => end(xhr.status);
      xhr.onerror = xhr.onabort = xhr.ontimeout = () => end(0);
      signal.addEventListener('abort', abort);
      // No headers: the link signs only the host and the length, which the browser sets.
      xhr.open('PUT', url);
      watch();
      xhr.send(data);
    });
  },
  sleep(ms, signal) {
    return new Promise((resolve) => {
      const t = setTimeout(resolve, ms);
      signal.addEventListener(
        'abort',
        () => {
          clearTimeout(t);
          resolve();
        },
        { once: true },
      );
    });
  },
  online: () => navigator.onLine !== false,
};

/** Cancels an unfinished upload: the server drops it and its parts. It also goes out while the
 * page closes. */
export function abortS3Upload(id: string): void {
  void browserTransport.api('DELETE', `/api/s3/uploads/${encodeURIComponent(id)}`);
}

export default class S3Upload<M extends Meta, B extends Body> extends BasePlugin<Opts, M, B> {
  private readonly slots: Slots;
  private readonly transport: S3Transport;

  constructor(uppy: Uppy<M, B>, opts?: S3UploadOptions) {
    super(uppy, { ...defaultOptions, ...opts });
    this.id = this.opts.id ?? 'S3Upload';
    this.type = 'uploader';
    this.slots = new Slots(this.opts.limit);
    this.transport = this.opts.transport ?? browserTransport;
  }

  install(): void {
    // Pausing and resuming single files is up to the uploader; Uppy asks it this way.
    this.uppy.setState({ capabilities: { ...this.uppy.getState().capabilities, resumableUploads: true } });
    this.uppy.addUploader(this.handleUpload);
  }

  uninstall(): void {
    this.uppy.setState({ capabilities: { ...this.uppy.getState().capabilities, resumableUploads: false } });
    this.uppy.removeUploader(this.handleUpload);
  }

  private handleUpload = async (fileIDs: string[]): Promise<void> => {
    const files = filterFilesToUpload(this.uppy.getFilesByIds(fileIDs));
    // Uppy pauses and resumes only files that were started.
    this.uppy.emit('upload-start', filterFilesToEmitUploadStarted(files));
    await Promise.allSettled(files.map((f) => this.upload(f)));
  };

  /** One file, until it is sent, removed or failed. A pause stops it; a resume goes on. */
  private upload(file: UppyFile<M, B>): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      const events = new EventManager(this.uppy);
      let run: AbortController | null = null;
      const stop = () => run?.abort();
      const end = () => {
        stop();
        events.remove();
      };
      const start = () => {
        if (run && !run.signal.aborted) return; // on its way already
        const current = new AbortController();
        run = current;
        this.send(file.id, current.signal).then(
          (sent) => {
            if (!sent) return; // stopped; resuming starts it again
            end();
            resolve();
          },
          (err: unknown) => {
            if (current.signal.aborted) return;
            end();
            this.uppy.emit('upload-error', this.uppy.getFile(file.id), err instanceof Error ? err : new Error(String(err)));
            reject(err);
          },
        );
      };
      const cancel = (s3: S3FileState | undefined) => {
        end();
        if (s3) void this.transport.api('DELETE', `/api/s3/uploads/${encodeURIComponent(s3.id)}`);
        resolve();
      };
      events.on('file-removed', (removed) => {
        if (removed.id === file.id) cancel(removed.s3);
      });
      events.onCancelAll(file.id, () => cancel(this.uppy.getFile(file.id)?.s3));
      events.onPause(file.id, (isPaused) => (isPaused ? stop() : start()));
      events.onPauseAll(file.id, stop);
      events.onResumeAll(file.id, start);
      if (!this.uppy.getFile(file.id)?.isPaused) start();
    });
  }

  /** Sends a file, in a slot of its own; true when it is in the library, false when stopped. */
  private async send(id: string, signal: AbortSignal): Promise<boolean> {
    const give = await this.slots.take();
    try {
      return signal.aborted ? false : await this.sendParts(id, signal);
    } finally {
      give();
    }
  }

  private async sendParts(id: string, signal: AbortSignal): Promise<boolean> {
    const file = this.uppy.getFile(id);
    const data = file?.data;
    if (!file || !(data instanceof Blob)) throw new Error('The file has to be picked again.');
    // Into an encrypted folder the parts are pieces of the encrypted stream (e2ee/upload.ts).
    const size = sizeOf(data);
    const seal = sealOf(data);
    let state = file.s3;
    let done: Set<number> | null = state ? null : new Set(); // null: ask the server
    let links = new Map<number, S3PartUrl>();
    let linkedAt = -Infinity;
    let attempt = 0;
    let refreshes = 0;
    let resyncs = 0;
    let restarts = 0;
    const stopped = () => signal.aborted || !this.uppy.getFile(id);
    for (;;) {
      if (stopped()) return false;
      try {
        if (!state) {
          const lastModified = data instanceof File ? data.lastModified : Number(file.meta.lastModified) || undefined;
          const folder = typeof file.meta.folder === 'string' ? file.meta.folder : undefined;
          const plan = await this.api<S3NewUpload>('POST', '/api/s3/uploads', {
            name: file.name,
            size,
            last_modified_ms: lastModified,
            folder,
            enc: seal ? encOf(seal) : undefined,
          });
          state = { id: plan.id, partSize: plan.part_size, parts: plan.parts };
          this.uppy.setFileState(id, { s3: state });
          links = byNumber(plan.urls);
          linkedAt = performance.now();
          done = new Set();
        }
        if (!done) {
          const status = await this.api<S3UploadStatus>('GET', `/api/s3/uploads/${encodeURIComponent(state.id)}`);
          if (status.state === 'complete') return this.succeeded(id, state.id);
          done = new Set(status.done_parts);
        }
        const upload = state;
        const have = done;
        this.progress(id, doneBytes(have, size, upload.partSize), size);
        for (const n of missingParts(upload.parts, have)) {
          if (stopped()) return false;
          let link = links.get(n);
          if (!link || stale(linkedAt)) {
            const next = missingParts(upload.parts, have).filter((m) => m >= n).slice(0, 100);
            const fresh = await this.api<S3PartUrls>('POST', `/api/s3/uploads/${encodeURIComponent(upload.id)}/parts`, { parts: next });
            links = byNumber(fresh.urls);
            linkedAt = performance.now();
            link = links.get(n);
            if (!link) throw new S3Failure('server', 0);
          }
          const [start, end] = partSpan(n, size, upload.partSize);
          const before = doneBytes(have, size, upload.partSize);
          // Without a type, so no Content-Type goes along: only the length is signed.
          const part = seal ? new Blob([await bytesOf(data, start, end)]) : data.slice(start, end);
          const status = await this.transport.put(link.url, part, (sent) => this.progress(id, before + sent, size), signal);
          if (stopped()) return false;
          if (status < 200 || status >= 300) throw new S3Failure('bucket', status);
          have.add(n);
          attempt = refreshes = 0;
        }
        await this.api('POST', `/api/s3/uploads/${encodeURIComponent(upload.id)}/complete`, {});
        return this.succeeded(id, upload.id);
      } catch (e) {
        if (stopped()) return false;
        if (!(e instanceof S3Failure)) throw e;
        let action = s3FailureAction(e.source, e.status, e.code);
        // A fresh link that doesn't help, a state that doesn't settle or an upload that keeps
        // going away: wait, or give up.
        if (action === 'refresh' && ++refreshes > 1) action = 'retry';
        if (action === 'resync' && ++resyncs > 2) action = 'retry';
        if (action === 'restart' && ++restarts > 1) action = 'refuse';
        switch (action) {
          case 'refresh':
            links.clear();
            break;
          case 'resync':
            done = null;
            break;
          case 'restart':
            state = undefined;
            this.uppy.setFileState(id, { s3: undefined });
            break;
          case 'refuse':
            this.opts.refused?.(e.status, e.code);
            throw e;
          case 'retry':
            if (!this.transport.online()) {
              while (!this.transport.online() && !stopped()) await this.transport.sleep(2000, signal);
              break;
            }
            if (attempt >= this.opts.retryDelays.length) throw e;
            await this.transport.sleep(this.opts.retryDelays[attempt++], signal);
        }
      }
    }
  }

  private async api<T>(method: 'GET' | 'POST', path: string, body?: unknown): Promise<T> {
    const res = await this.transport.api(method, path, body);
    if (res.status >= 200 && res.status < 300) return res.body as T;
    const code = (res.body as { error?: { code?: unknown } } | null)?.error?.code;
    throw new S3Failure('server', res.status, typeof code === 'string' ? code : undefined);
  }

  private progress(id: string, bytes: number, total: number): void {
    const file = this.uppy.getFile(id);
    if (!file) return;
    this.uppy.emit('upload-progress', file, { uploadStarted: file.progress.uploadStarted ?? Date.now(), bytesUploaded: bytes, bytesTotal: total });
  }

  /** The file is in the library; its address ends in its id, as with tus. */
  private succeeded(id: string, fileID: string): true {
    this.uppy.emit('upload-success', this.uppy.getFile(id), { uploadURL: `/api/files/${fileID}`, status: 200, body: {} as B });
    return true;
  }
}

function byNumber(urls: S3PartUrl[]): Map<number, S3PartUrl> {
  return new Map(urls.map((u) => [u.number, u]));
}
