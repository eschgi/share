import { describe, expect, it } from 'vitest';
import { numbered, saveFiles, type Answer, type Folder, type SaveItem, type SaveState, type Writer } from '../src/account/save/engine';
import { wrongMarks } from '../src/account/save/marks';

/** A folder in memory. A file is there once its writer closes, as with the real thing. */
class FakeFolder implements Folder {
  files = new Map<string, Uint8Array>();
  open = 0;
  most = 0;
  /** After this many bytes in all, writing fails as on a full drive. */
  room = Infinity;
  async sizeOf(path: string) {
    return this.files.get(path)?.length ?? null;
  }
  async create(path: string): Promise<Writer> {
    const parts: Uint8Array[] = [];
    this.open++;
    this.most = Math.max(this.most, this.open);
    const done = () => {
      this.open--;
    };
    return {
      write: async (c) => {
        if ((this.room -= c.length) < 0) throw new DOMException('full', 'QuotaExceededError');
        parts.push(c);
      },
      close: async () => {
        const all = new Uint8Array(parts.reduce((s, p) => s + p.length, 0));
        let at = 0;
        for (const p of parts) all.set(p, (at += p.length) - p.length);
        this.files.set(path, all);
        done();
      },
      abort: async () => done(),
    };
  }
}

const bytes = (n: number, seed = 1) => Uint8Array.from({ length: n }, (_, i) => (i * seed) % 251);

/** A server with some files; it can cut the connection after a number of bytes, once. */
class FakeServer {
  calls: { id: string; from: number; etag: string | null }[] = [];
  cutAfter = new Map<string, number>();
  gone = new Set<string>();
  /** Answers a Range request with the whole file, as if it had changed. */
  ignoreRange = new Set<string>();
  constructor(readonly content: Map<string, Uint8Array>) {}
  fetch = async ({ id }: SaveItem, from: number, etag: string | null): Promise<Answer> => {
    this.calls.push({ id, from, etag });
    await Promise.resolve();
    if (this.gone.has(id)) return { status: 404, etag: null, body: (async function* () {})() };
    const data = this.content.get(id)!;
    const whole = from === 0 || this.ignoreRange.has(id);
    this.ignoreRange.delete(id);
    const rest = whole ? data : data.subarray(from);
    const cut = this.cutAfter.get(id);
    this.cutAfter.delete(id);
    async function* body() {
      for (let i = 0; i < rest.length; i += 10) {
        if (cut !== undefined && i >= cut) throw new TypeError('network error');
        yield rest.subarray(i, i + 10);
      }
    }
    return { status: whole ? 200 : 206, etag: `"${id}"`, body: body() };
  };
}

function setup(sizes: Record<string, number>) {
  const content = new Map(Object.entries(sizes).map(([id, n], i) => [id, bytes(n, i + 2)]));
  const items: SaveItem[] = Object.entries(sizes).map(([id, n]) => ({ id, path: `2026-10-01/${id}.jpg`, size: n }));
  return { items, content, folder: new FakeFolder(), server: new FakeServer(content) };
}

const noWait = async () => {};

async function run(items: SaveItem[], folder: Folder, server: FakeServer, signal = new AbortController().signal) {
  const seen: SaveState[] = [];
  const end = await saveFiles(items, folder, server.fetch, { signal, onChange: (s) => seen.push(s), sleep: noWait });
  return { end, seen };
}

describe('numbered', () => {
  it('numbers the name, not the folder or the extension', () => {
    expect(numbered('2026-10-01/IMG_0001.jpg', 2)).toBe('2026-10-01/IMG_0001 (2).jpg');
    expect(numbered('2026-10-01/README', 3)).toBe('2026-10-01/README (3)');
    expect(numbered('2026-10-01/.hidden', 2)).toBe('2026-10-01/.hidden (2)');
  });
});

describe('saveFiles', () => {
  it('saves every file into its day folder, three at a time', async () => {
    const { items, content, folder, server } = setup({ a: 25, b: 7, c: 31, d: 0, e: 12 });
    const { end } = await run(items, folder, server);
    expect(end).toMatchObject({ total: 5, done: 5, skipped: 0, failed: [], stopped: null, finished: true, bytesDone: 75, bytesTotal: 75 });
    for (const [id, data] of content) expect(folder.files.get(`2026-10-01/${id}.jpg`)).toEqual(data);
    expect(folder.most).toBe(3);
  });

  it('skips files already there, and numbers others with the same name', async () => {
    const { items, folder, server } = setup({ a: 20, b: 20 });
    folder.files.set('2026-10-01/a.jpg', bytes(20)); // the same size: already saved
    folder.files.set('2026-10-01/b.jpg', bytes(5)); // another file of that name
    const { end } = await run(items, folder, server);
    expect(end).toMatchObject({ done: 2, skipped: 1 });
    expect(folder.files.get('2026-10-01/b.jpg')).toHaveLength(5);
    expect(folder.files.get('2026-10-01/b (2).jpg')).toHaveLength(20);
    expect(server.calls.map((c) => c.id)).toEqual(['b']);
    // Saving again finds both, also the numbered one.
    const again = await run(items, folder, server);
    expect(again.end).toMatchObject({ done: 2, skipped: 2 });
  });

  it('replaces an empty file of its name, which a closed tab leaves behind', async () => {
    const { items, folder, server } = setup({ a: 20 });
    folder.files.set('2026-10-01/a.jpg', new Uint8Array());
    await run(items, folder, server);
    expect(folder.files.get('2026-10-01/a.jpg')).toHaveLength(20);
    expect(folder.files.has('2026-10-01/a (2).jpg')).toBe(false);
  });

  it('goes on from where a broken connection stopped, if the file is the same', async () => {
    const { items, content, folder, server } = setup({ a: 45 });
    server.cutAfter.set('a', 20);
    const { end, seen } = await run(items, folder, server);
    expect(server.calls).toEqual([
      { id: 'a', from: 0, etag: null },
      { id: 'a', from: 20, etag: '"a"' },
    ]);
    expect(seen.some((s) => s.waiting)).toBe(true);
    expect(end).toMatchObject({ done: 1, waiting: false, bytesDone: 45 });
    expect(folder.files.get('2026-10-01/a.jpg')).toEqual(content.get('a'));
  });

  it('starts a file over when the server sends it whole', async () => {
    const { items, content, folder, server } = setup({ a: 45 });
    server.cutAfter.set('a', 20);
    server.ignoreRange.add('a');
    const { end } = await run(items, folder, server);
    expect(end.bytesDone).toBe(45);
    expect(folder.files.get('2026-10-01/a.jpg')).toEqual(content.get('a'));
  });

  it('fails a file that is gone from the server, and saves the rest', async () => {
    const { items, folder, server } = setup({ a: 10, b: 10, c: 10 });
    server.gone.add('b');
    const { end } = await run(items, folder, server);
    expect(end.failed.map((f) => f.id)).toEqual(['b']);
    expect(end).toMatchObject({ done: 2, stopped: null, bytesDone: 20 });
    expect(folder.files.has('2026-10-01/b.jpg')).toBe(false);
  });

  it('stops everything when the drive is full, keeping no half files', async () => {
    const { items, folder, server } = setup({ a: 30, b: 30, c: 30, d: 30 });
    folder.room = 50;
    const { end } = await run(items, folder, server);
    expect(end.stopped).toBe('full');
    expect(end.done).toBeLessThan(4);
    for (const data of folder.files.values()) expect(data).toHaveLength(30);
  });

  it('stops when cancelled', async () => {
    const { items, folder, server } = setup({ a: 30, b: 30, c: 30, d: 30, e: 30 });
    const ctl = new AbortController();
    const seen: SaveState[] = [];
    const end = await saveFiles(items, folder, server.fetch, {
      signal: ctl.signal,
      sleep: noWait,
      onChange: (s) => {
        seen.push(s);
        if (s.done === 1) ctl.abort();
      },
    });
    expect(end.stopped).toBe('cancelled');
    expect(end.done).toBeLessThan(5);
    expect(folder.open).toBe(0);
  });

  it('stops when this browser was signed out', async () => {
    const { items, folder } = setup({ a: 10, b: 10 });
    const fetch = async (): Promise<Answer> => ({ status: 401, etag: null, body: (async function* () {})() });
    const end = await saveFiles(items, folder, fetch, { signal: new AbortController().signal, onChange: () => {}, sleep: noWait });
    expect(end.stopped).toBe('signedOut');
    expect(folder.files.size).toBe(0);
  });

  it('asks again when the bucket refuses a link, and fails only that file in the end', async () => {
    const { items, content, folder, server } = setup({ a: 10, b: 10 });
    const refusals = new Map([
      ['a', 2], // a link that ran out, twice: fresh ones follow
      ['b', 9], // a bucket that never lets it through
    ]);
    const fetch = async (item: SaveItem, from: number, etag: string | null): Promise<Answer> => {
      const id = item.id;
      const left = refusals.get(id) ?? 0;
      if (left > 0) {
        refusals.set(id, left - 1);
        return { status: 403, etag: null, body: (async function* () {})(), remote: true };
      }
      return server.fetch(item, from, etag);
    };
    const end = await saveFiles(items, folder, fetch, { signal: new AbortController().signal, onChange: () => {}, sleep: noWait });
    expect(end.stopped).toBeNull();
    expect(end.done).toBe(1);
    expect(end.failed.map((f) => f.id)).toEqual(['b']);
    expect(folder.files.get('2026-10-01/a.jpg')).toEqual(content.get('a'));
    expect(refusals.get('b')).toBe(5); // the first answer and three more
  });

  it('tells which files are in the folder, saved or there already, and where', async () => {
    const { items, folder, server } = setup({ a: 20, b: 20, c: 20 });
    folder.files.set('2026-10-01/a.jpg', bytes(20));
    folder.files.set('2026-10-01/b.jpg', bytes(5));
    server.gone.add('c');
    const saved: [string, string][] = [];
    const onSaved = (item: SaveItem, path: string) => saved.push([item.id, path]);
    await saveFiles(items, folder, server.fetch, { signal: new AbortController().signal, onChange: () => {}, sleep: noWait, onSaved });
    expect(saved.sort()).toEqual([
      ['a', '2026-10-01/a.jpg'],
      ['b', '2026-10-01/b (2).jpg'],
    ]);
  });
});

describe('marks', () => {
  it('are wrong for files missing from the folder or changed there', async () => {
    const folder = new FakeFolder();
    folder.files.set('2026-10-01/a.jpg', bytes(20));
    folder.files.set('2026-10-01/b.jpg', bytes(7));
    const marks = new Map([
      ['a', { path: '2026-10-01/a.jpg', size: 20 }],
      ['b', { path: '2026-10-01/b.jpg', size: 20 }],
      ['c', { path: '2026-10-01/c.jpg', size: 20 }],
    ]);
    expect(await wrongMarks(marks, folder)).toEqual(['b', 'c']);
  });
});
