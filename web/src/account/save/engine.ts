// Saving files into a folder the person picked (Chrome and Edge, screen 27): a folder for each
// day, as the server keeps them; files already there are skipped; three at a time; and a
// broken connection is picked up where it stopped, for as long as the tab is open. The folder
// and the network come in from outside, so the tests can play both.

/** A file to save, with its path inside the folder: "2026-09-27/IMG_0001.jpg". */
export interface SaveItem {
  id: string;
  path: string;
  size: number;
}

export interface Writer {
  write(chunk: Uint8Array): Promise<void>;
  /** Keeps the file. */
  close(): Promise<void>;
  /** Throws the file away. */
  abort(): Promise<void>;
}

export interface Folder {
  /** The size of the file at path, or null when there is none. */
  sizeOf(path: string): Promise<number | null>;
  /** A new, empty file at path, with the folders it needs; one already there is replaced. */
  create(path: string): Promise<Writer>;
}

export interface Answer {
  status: number;
  etag: string | null;
  body: AsyncIterable<Uint8Array>;
}

/** Asks for a file from byte `from` on; with etag, the rest only if it is still the same file. */
export type FetchFile = (id: string, from: number, etag: string | null, signal: AbortSignal) => Promise<Answer>;

/** Why saving stopped before the end. */
export type Stop = 'full' | 'folder' | 'signedOut' | 'cancelled';

export interface SaveState {
  total: number;
  /** Saved, or there already. */
  done: number;
  skipped: number;
  failed: SaveItem[];
  bytesTotal: number;
  bytesDone: number;
  /** No connection: waiting to try again. */
  waiting: boolean;
  stopped: Stop | null;
  finished: boolean;
}

export interface SaveOptions {
  signal: AbortSignal;
  onChange: (s: SaveState) => void;
  parallel?: number;
  /** Waits, unless the signal ends it first. */
  sleep?: (ms: number, signal: AbortSignal) => Promise<void>;
}

/** "IMG (2).jpg": the name for another file of the same name, as the server numbers them. */
export function numbered(path: string, n: number): string {
  const slash = path.lastIndexOf('/');
  const name = path.slice(slash + 1);
  const dot = name.lastIndexOf('.');
  const stem = dot > 0 ? name.slice(0, dot) : name;
  const ext = dot > 0 ? name.slice(dot) : '';
  return `${path.slice(0, slash + 1)}${stem} (${n})${ext}`;
}

class FolderError extends Error {
  constructor(readonly full: boolean) {
    super(full ? 'full' : 'folder');
  }
}

class StatusError extends Error {
  constructor(readonly status: number) {
    super(`HTTP ${status}`);
  }
}

const defaultSleep = (ms: number, signal: AbortSignal) =>
  new Promise<void>((resolve) => {
    const id = setTimeout(resolve, ms);
    signal.addEventListener('abort', () => {
      clearTimeout(id);
      resolve();
    });
  });

export async function saveFiles(items: SaveItem[], folder: Folder, fetchFile: FetchFile, opts: SaveOptions): Promise<SaveState> {
  const { signal, onChange } = opts;
  const sleep = opts.sleep ?? defaultSleep;
  const state: SaveState = {
    total: items.length,
    done: 0,
    skipped: 0,
    failed: [],
    bytesTotal: items.reduce((s, f) => s + f.size, 0),
    bytesDone: 0,
    waiting: false,
    stopped: null,
    finished: false,
  };
  const changed = () => onChange({ ...state, failed: [...state.failed] });
  const stopAll = new AbortController();
  const stop = (why: Stop) => {
    state.stopped ??= why;
    stopAll.abort();
  };
  if (signal.aborted) stop('cancelled');
  else signal.addEventListener('abort', () => stop('cancelled'));
  const stopped = () => stopAll.signal.aborted;
  // Paths taken by files being saved now, so two files never pick the same free name.
  const claimed = new Set<string>();
  let waits = 0;

  /** Where a file goes: its own path, or "name (2)" and on if another file has that name. Null:
   * it is there already. An empty file of its name is what a closed tab left: it is replaced. */
  async function placeFor(item: SaveItem): Promise<string | null> {
    for (let n = 1; ; n++) {
      const path = n === 1 ? item.path : numbered(item.path, n);
      if (claimed.has(path)) continue;
      const size = await folder.sizeOf(path);
      if (size === item.size) return null;
      if (size === null || (size === 0 && item.size > 0)) {
        claimed.add(path);
        return path;
      }
    }
  }

  async function write(w: Writer, chunk: Uint8Array) {
    try {
      await w.write(chunk);
    } catch (e) {
      throw new FolderError(e instanceof DOMException && e.name === 'QuotaExceededError');
    }
  }

  async function saveOne(item: SaveItem): Promise<void> {
    let path: string | null;
    try {
      path = await placeFor(item);
    } catch {
      throw new FolderError(false);
    }
    if (path === null) {
      state.skipped++;
      state.done++;
      state.bytesDone += item.size;
      return changed();
    }
    let writer: Writer;
    try {
      writer = await folder.create(path);
    } catch (e) {
      throw new FolderError(e instanceof DOMException && e.name === 'QuotaExceededError');
    }
    let written = 0;
    let etag: string | null = null;
    let delay = 1000;
    try {
      for (;;) {
        if (stopped()) throw stopAll.signal.reason;
        try {
          const res = await fetchFile(item.id, written, etag, stopAll.signal);
          if (res.status === 401 || res.status === 403) {
            stop('signedOut');
            throw new StatusError(res.status);
          }
          if (res.status === 404 || res.status === 410 || res.status === 416) throw new StatusError(res.status);
          if (res.status === 200 && written > 0) {
            // A whole file instead of the rest: it changed meanwhile. Start it over.
            await writer.abort().catch(() => {});
            writer = await folder.create(path);
            state.bytesDone -= written;
            written = 0;
          } else if (res.status !== 200 && res.status !== 206) {
            throw new StatusError(res.status); // 5xx and the like: like a broken connection
          }
          etag = res.etag ?? etag;
          for await (const chunk of res.body) {
            if (stopped()) throw stopAll.signal.reason;
            await write(writer, chunk);
            written += chunk.length;
            state.bytesDone += chunk.length;
            changed();
          }
          if (written < item.size) throw new Error('cut off');
          await writer.close();
          state.done++;
          return changed();
        } catch (e) {
          if (stopped() || e instanceof FolderError) throw e;
          if (e instanceof StatusError && e.status < 500 && e.status !== 408 && e.status !== 429) {
            await writer.abort().catch(() => {});
            state.bytesDone -= written;
            state.failed.push(item);
            return changed();
          }
          // No connection: try again in a moment, then less and less often, up to once a minute.
          waits++;
          state.waiting = true;
          changed();
          await sleep(delay, stopAll.signal);
          delay = Math.min(delay * 2, 60_000);
          if (--waits === 0) state.waiting = false;
        }
      }
    } catch (e) {
      await writer.abort().catch(() => {});
      throw e;
    } finally {
      claimed.delete(path);
    }
  }

  let next = 0;
  async function worker() {
    while (!stopped() && next < items.length) {
      const item = items[next++];
      try {
        await saveOne(item);
      } catch (e) {
        if (e instanceof FolderError) stop(e.full ? 'full' : 'folder');
        else stop(state.stopped ?? 'cancelled');
      }
    }
  }
  changed();
  await Promise.all(Array.from({ length: Math.min(opts.parallel ?? 3, items.length) }, worker));
  state.waiting = false;
  state.finished = true;
  changed();
  return { ...state, failed: [...state.failed] };
}
