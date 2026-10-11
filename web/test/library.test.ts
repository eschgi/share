import { describe, expect, it } from 'vitest';
import type { FileInfo, FilePage, LibraryFilter, LibraryOverview } from '../src/api';
import { LibraryModel } from '../src/account/library/model';

function file(id: string, day: string, size = 100): FileInfo {
  return {
    id,
    folder: 'family',
    name: `${id}.jpg`,
    size,
    mime: 'image/jpeg',
    kind: 'photo',
    day,
    uploaded_at: `${day}T10:00:00Z`,
    taken_at: null,
    taken_day: null,
    updated_at: `${day}T10:00:00Z`,
    width: null,
    height: null,
    duration_ms: null,
    has_thumb: true,
    from: null,
    from_id: null,
    enc: null,
  };
}

/** A server whose answers the test hands out one at a time, to also answer out of order. */
class FakeServer {
  calls: { what: 'overview' | 'page'; filter: LibraryFilter; cursor?: string | null; limit?: number; answer: (v: unknown) => void; fail: () => void }[] = [];
  requests = {
    overview: (filter: LibraryFilter) =>
      new Promise<LibraryOverview>((resolve, reject) =>
        this.calls.push({ what: 'overview', filter, answer: resolve as (v: unknown) => void, fail: () => reject(new Error('offline')) }),
      ),
    page: (filter: LibraryFilter, cursor: string | null, limit: number) =>
      new Promise<FilePage>((resolve, reject) =>
        this.calls.push({ what: 'page', filter, cursor, limit, answer: resolve as (v: unknown) => void, fail: () => reject(new Error('offline')) }),
      ),
  };
  /** Answers the oldest open call. */
  async answer(v: LibraryOverview | FilePage) {
    this.calls.shift()!.answer(v);
    await settle();
  }
  async fail() {
    this.calls.shift()!.fail();
    await settle();
  }
}

const settle = () => new Promise((r) => setTimeout(r, 0));

const overview = (version: number, days: [string, number, number][]): LibraryOverview => ({
  version,
  groups: days.map(([key, count, bytes]) => ({ key, count, bytes, last_day: key, from: null })),
});

describe('LibraryModel', () => {
  it('loads the overview, then pages, until there are no more', async () => {
    const server = new FakeServer();
    const m = new LibraryModel(server.requests);
    void m.reload();
    expect(m.loading).toBe(true);
    await server.answer(overview(1, [['2026-10-02', 3, 900], ['2026-10-01', 1, 100]]));
    expect(server.calls[0]).toMatchObject({ what: 'page', cursor: null, limit: 200 });
    await server.answer({ files: [file('a', '2026-10-02'), file('b', '2026-10-02')], next_cursor: 'c1' });
    expect(m.complete).toBe(false);
    void m.more();
    expect(server.calls[0]).toMatchObject({ what: 'page', cursor: 'c1' });
    await server.answer({ files: [file('c', '2026-10-02'), file('d', '2026-10-01')], next_cursor: null });
    expect(m.complete).toBe(true);
    expect(m.files.map((f) => f.id)).toEqual(['a', 'b', 'c', 'd']);
    // The days' totals are the server's, also while not every file of a day is loaded.
    expect(m.sections.map((s) => [s.key, s.files.length, s.count, s.bytes])).toEqual([
      ['2026-10-02', 3, 3, 900],
      ['2026-10-01', 1, 1, 100],
    ]);
    void m.more();
    expect(server.calls).toEqual([]); // nothing more to load
  });

  it('drops answers for a list it has left', async () => {
    const server = new FakeServer();
    const m = new LibraryModel(server.requests);
    void m.reload();
    await server.answer(overview(1, [['2026-10-02', 1, 100]]));
    // Photos are chosen while the first page is still on its way.
    void m.setFilter({ folder: null, kind: 'photo', q: '' });
    await server.answer({ files: [file('old', '2026-10-02')], next_cursor: null });
    expect(m.files).toEqual([]);
    expect(m.loading).toBe(true);
    expect(server.calls[0]).toMatchObject({ what: 'overview', filter: { kind: 'photo' } });
    await server.answer(overview(1, [['2026-10-02', 1, 100]]));
    await server.answer({ files: [file('new', '2026-10-02')], next_cursor: null });
    expect(m.files.map((f) => f.id)).toEqual(['new']);
  });

  it('keeps a search that only differs in spaces', async () => {
    const server = new FakeServer();
    const m = new LibraryModel(server.requests);
    void m.setFilter({ folder: null, kind: null, q: 'img' });
    expect(server.calls).toHaveLength(1);
    void m.setFilter({ folder: null, kind: null, q: ' img ' });
    expect(server.calls).toHaveLength(1);
  });

  it('shows one folder, or all of them', async () => {
    const server = new FakeServer();
    const m = new LibraryModel(server.requests, { folder: 'wedding', kind: null, q: '' });
    void m.reload();
    expect(server.calls[0]).toMatchObject({ what: 'overview', filter: { folder: 'wedding' } });
    await server.answer(overview(1, []));
    await server.answer({ files: [], next_cursor: null });
    void m.setFilter({ folder: 'wedding', kind: null, q: '' });
    expect(server.calls).toEqual([]); // the same folder: nothing to load
    void m.setFilter({ folder: null, kind: null, q: '' });
    expect(server.calls[0]).toMatchObject({ what: 'overview', filter: { folder: null } });
  });

  it('starts over when files went away, such as to another folder', async () => {
    const server = new FakeServer();
    const m = new LibraryModel(server.requests);
    void m.reload();
    await server.answer(overview(1, [['2026-10-01', 2, 200]]));
    await server.answer({ files: [file('a', '2026-10-01'), file('b', '2026-10-01')], next_cursor: null });
    const changed = m.refreshIfChanged();
    await server.answer(overview(2, [['2026-10-01', 1, 100]]));
    expect(await changed).toBe(true);
    expect(server.calls[0]).toMatchObject({ what: 'overview' }); // the list starts over
    await server.answer(overview(2, [['2026-10-01', 1, 100]]));
    await server.answer({ files: [file('a', '2026-10-01')], next_cursor: null });
    expect(m.files.map((f) => f.id)).toEqual(['a']);
  });

  it('puts new files on top when the version grows, and keeps the rest', async () => {
    const server = new FakeServer();
    const m = new LibraryModel(server.requests);
    void m.reload();
    await server.answer(overview(1, [['2026-10-01', 2, 200]]));
    await server.answer({ files: [file('a', '2026-10-01'), file('b', '2026-10-01')], next_cursor: 'c1' });

    const same = m.refreshIfChanged();
    await server.answer(overview(1, [['2026-10-01', 2, 200]]));
    expect(server.calls).toEqual([]); // the same version: nothing to load
    expect(await same).toBe(false);

    const grown = m.refreshIfChanged();
    await server.answer(overview(2, [['2026-10-02', 1, 50], ['2026-10-01', 2, 200]]));
    expect(server.calls[0]).toMatchObject({ what: 'page', cursor: null, limit: 100 });
    await server.answer({ files: [file('n', '2026-10-02', 50), file('a', '2026-10-01')], next_cursor: 'x' });
    expect(m.files.map((f) => f.id)).toEqual(['n', 'a', 'b']);
    expect(m.overview?.version).toBe(2);
    expect(await grown).toBe(true);
    // The next page still follows the old list.
    void m.more();
    expect(server.calls[0]).toMatchObject({ cursor: 'c1' });
  });

  it('says when it fails, and starts over on reload', async () => {
    const server = new FakeServer();
    const m = new LibraryModel(server.requests);
    let changes = 0;
    m.subscribe(() => changes++);
    void m.reload();
    await server.fail();
    expect(m.failed).toBe(true);
    expect(m.loading).toBe(false);
    void m.reload();
    expect(m.failed).toBe(false);
    await server.answer(overview(1, []));
    await server.answer({ files: [], next_cursor: null });
    expect(m.complete).toBe(true);
    expect(changes).toBeGreaterThan(3);
  });

  it('keeps what it has when a later page fails', async () => {
    const server = new FakeServer();
    const m = new LibraryModel(server.requests);
    void m.reload();
    await server.answer(overview(1, [['2026-10-01', 2, 200]]));
    await server.answer({ files: [file('a', '2026-10-01')], next_cursor: 'c1' });
    void m.more();
    await server.fail();
    expect(m.failed).toBe(true);
    expect(m.files.map((f) => f.id)).toEqual(['a']);
    void m.more();
    expect(server.calls[0]).toMatchObject({ cursor: 'c1' });
  });
});

describe('LibraryModel.remove', () => {
  it('takes deleted files out of the list and the days', async () => {
    const server = new FakeServer();
    const m = new LibraryModel(server.requests);
    void m.reload();
    await server.answer(overview(1, [['2026-10-02', 2, 300], ['2026-10-01', 1, 100]]));
    await server.answer({ files: [file('a', '2026-10-02', 100), file('b', '2026-10-02', 200), file('c', '2026-10-01', 100)], next_cursor: null });
    m.remove(['b', 'c']);
    expect(m.files.map((f) => f.id)).toEqual(['a']);
    expect(m.overview?.groups).toEqual([{ key: '2026-10-02', count: 1, bytes: 100, last_day: '2026-10-02', from: null }]);
  });
});

describe('sorted otherwise', () => {
  it('groups by the day taken, else the day it came, and by sender, as the server keys them', async () => {
    const server = new FakeServer();
    const m = new LibraryModel(server.requests, { folder: null, kind: null, q: '', sort: 'taken' });
    void m.reload();
    await server.answer({
      version: 1,
      groups: [
        { key: '2026-10-02', count: 1, bytes: 100, last_day: '2026-10-02', from: null },
        { key: '2025-06-14', count: 1, bytes: 100, last_day: '2026-10-02', from: null },
      ],
    });
    const old = { ...file('a', '2026-10-02'), taken_at: '2025-06-14T16:32:00Z', taken_day: '2025-06-14' };
    await server.answer({ files: [file('doc', '2026-10-02'), old], next_cursor: null });
    expect(m.sections.map((s) => [s.key, s.files.map((f) => f.id), s.lastDay])).toEqual([
      ['2026-10-02', ['doc'], '2026-10-02'],
      ['2025-06-14', ['a'], '2026-10-02'],
    ]);

    void m.setFilter({ ...m.filter, sort: 'sender' });
    await server.answer({
      version: 1,
      groups: [
        { key: 'u1', count: 1, bytes: 100, last_day: '2026-10-02', from: 'Maria' },
        { key: 'pin', count: 1, bytes: 100, last_day: '2026-10-01', from: null },
      ],
    });
    await server.answer({ files: [{ ...file('m', '2026-10-02'), from: 'Maria', from_id: 'u1' }, file('g', '2026-10-01')], next_cursor: null });
    expect(m.sections.map((s) => [s.key, s.from, s.files.length])).toEqual([
      ['u1', 'Maria', 1],
      ['pin', null, 1],
    ]);
  });

  it('starts over when the library changes, since new files may belong anywhere', async () => {
    const server = new FakeServer();
    const m = new LibraryModel(server.requests, { folder: null, kind: null, q: '', order: 'oldest' });
    void m.reload();
    await server.answer(overview(1, [['2026-10-01', 1, 100]]));
    await server.answer({ files: [file('a', '2026-10-01')], next_cursor: null });
    void m.refreshIfChanged();
    await server.answer(overview(2, [['2026-10-01', 1, 100], ['2026-10-02', 1, 100]]));
    expect(server.calls[0]).toMatchObject({ what: 'overview' }); // the list starts over
    expect(m.files).toEqual([]);
  });

  it('a different sort or order is a different list', async () => {
    const { sameFilter } = await import('../src/account/library/model');
    const base: LibraryFilter = { folder: null, kind: null, q: '' };
    expect(sameFilter(base, { ...base, sort: 'received', order: 'newest' })).toBe(true);
    expect(sameFilter(base, { ...base, sort: 'taken' })).toBe(false);
    expect(sameFilter(base, { ...base, order: 'oldest' })).toBe(false);
  });

  it('each browser remembers its choice', async () => {
    const { storedSort } = await import('../src/account/library/SortMenu');
    const store = new Map<string, string>();
    globalThis.localStorage = { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => void store.set(k, v) } as Storage;
    expect(storedSort()).toEqual({ sort: 'received', order: 'newest' });
    store.set('share.librarySort', JSON.stringify({ sort: 'taken', order: 'oldest' }));
    expect(storedSort()).toEqual({ sort: 'taken', order: 'oldest' });
    store.set('share.librarySort', '{"sort": "size"}');
    expect(storedSort()).toEqual({ sort: 'received', order: 'newest' });
  });
});
