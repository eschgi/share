import { describe, expect, it } from 'vitest';
import type { FileInfo, FilePage, LibraryFilter, LibraryOverview } from '../src/api';
import { LibraryModel } from '../src/account/library/model';

function file(id: string, day: string, size = 100): FileInfo {
  return {
    id,
    name: `${id}.jpg`,
    size,
    mime: 'image/jpeg',
    kind: 'photo',
    day,
    uploaded_at: `${day}T10:00:00Z`,
    updated_at: `${day}T10:00:00Z`,
    width: null,
    height: null,
    duration_ms: null,
    has_thumb: true,
    from: null,
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
  days: days.map(([day, count, bytes]) => ({ day, count, bytes })),
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
    expect(m.sections.map((s) => [s.day, s.files.length, s.count, s.bytes])).toEqual([
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
    void m.setFilter({ kind: 'photo', q: '' });
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
    void m.setFilter({ kind: null, q: 'img' });
    expect(server.calls).toHaveLength(1);
    void m.setFilter({ kind: null, q: ' img ' });
    expect(server.calls).toHaveLength(1);
  });

  it('puts new files on top when the version grows, and keeps the rest', async () => {
    const server = new FakeServer();
    const m = new LibraryModel(server.requests);
    void m.reload();
    await server.answer(overview(1, [['2026-10-01', 2, 200]]));
    await server.answer({ files: [file('a', '2026-10-01'), file('b', '2026-10-01')], next_cursor: 'c1' });

    void m.refreshIfChanged();
    await server.answer(overview(1, [['2026-10-01', 2, 200]]));
    expect(server.calls).toEqual([]); // the same version: nothing to load

    void m.refreshIfChanged();
    await server.answer(overview(2, [['2026-10-02', 1, 50], ['2026-10-01', 2, 200]]));
    expect(server.calls[0]).toMatchObject({ what: 'page', cursor: null, limit: 100 });
    await server.answer({ files: [file('n', '2026-10-02', 50), file('a', '2026-10-01')], next_cursor: 'x' });
    expect(m.files.map((f) => f.id)).toEqual(['n', 'a', 'b']);
    expect(m.overview?.version).toBe(2);
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
    expect(m.overview?.days).toEqual([{ day: '2026-10-02', count: 1, bytes: 100 }]);
  });
});
