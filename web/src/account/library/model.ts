// The library's files, loaded page by page, newest first, as the app's LibraryController
// does: the server's day totals, paging, and new files put on top when the library changes.
// The requests come in from outside, so the tests can answer them.
import type { FileInfo, FilePage, LibraryFilter, LibraryOverview } from '../../api';

export interface LibraryRequests {
  overview(f: LibraryFilter): Promise<LibraryOverview>;
  page(f: LibraryFilter, cursor: string | null, limit: number): Promise<FilePage>;
}

/** One upload day: its loaded files, and the server's totals for all of the day. */
export interface DaySection {
  day: string;
  files: FileInfo[];
  count: number;
  bytes: number;
}

const pageSize = 200;
const newerPageSize = 100;

/** Whether two filters show the same: the same folder, kind and words. */
export function sameFilter(a: LibraryFilter, b: LibraryFilter): boolean {
  return a.folder === b.folder && a.kind === b.kind && a.q.trim() === b.q.trim();
}

/** Some day has fewer files than before, or none: files were moved away or hidden, which
 * adding new files on top can't show. */
export function shrank(before: LibraryOverview, after: LibraryOverview): boolean {
  const now = new Map(after.days.map((d) => [d.day, d.count]));
  return before.days.some((d) => (now.get(d.day) ?? 0) < d.count);
}

export class LibraryModel {
  filter: LibraryFilter;
  overview: LibraryOverview | null = null;
  files: FileInfo[] = [];
  /** Loading the overview or a page. */
  loading = false;
  /** The last request failed; the list keeps what it has. */
  failed = false;
  /** Every file of the filter is loaded. */
  complete = false;

  private shown = new Set<string>();
  private cursor: string | null = null;
  /** Grows with every new list; answers for an older one are dropped. */
  private generation = 0;
  private listeners = new Set<() => void>();

  constructor(
    private readonly requests: LibraryRequests,
    filter: LibraryFilter = { folder: null, kind: null, q: '' },
  ) {
    this.filter = filter;
  }

  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => void this.listeners.delete(listener);
  }

  private changed(): void {
    for (const l of this.listeners) l();
  }

  setFilter(f: LibraryFilter): Promise<void> {
    if (sameFilter(f, this.filter)) return Promise.resolve();
    this.filter = f;
    return this.reload();
  }

  /** Starts the list over: the overview, then the first page. */
  async reload(): Promise<void> {
    const gen = ++this.generation;
    this.files = [];
    this.shown = new Set();
    this.cursor = null;
    this.complete = false;
    this.failed = false;
    this.loading = true;
    this.changed();
    try {
      const o = await this.requests.overview(this.filter);
      if (gen !== this.generation) return;
      this.overview = o;
      this.loading = false;
      await this.more();
    } catch {
      if (gen !== this.generation) return;
      this.failed = true;
      this.loading = false;
      this.changed();
    }
  }

  /** The next page, when the list is scrolled near its end. */
  async more(): Promise<void> {
    if (this.loading || this.complete || !this.overview) return;
    const gen = this.generation;
    this.loading = true;
    this.failed = false;
    this.changed();
    try {
      const page = await this.requests.page(this.filter, this.cursor, pageSize);
      if (gen !== this.generation) return;
      this.add(page.files, false);
      this.cursor = page.next_cursor;
      this.complete = page.next_cursor === null;
    } catch {
      if (gen === this.generation) this.failed = true;
    } finally {
      if (gen === this.generation) {
        this.loading = false;
        this.changed();
      }
    }
  }

  /** Something changed on the server (the version grew): new files go on top, and the place
   * in the list stays; if files went away, the list starts over. Errors wait for the next
   * check. It reports whether the library changed. */
  async refreshIfChanged(): Promise<boolean> {
    const current = this.overview;
    if (!current || this.loading) return false;
    const gen = this.generation;
    try {
      const o = await this.requests.overview(this.filter);
      if (gen !== this.generation || o.version === current.version) return false;
      if (shrank(current, o)) {
        void this.reload();
        return true;
      }
      const first = await this.requests.page(this.filter, null, newerPageSize);
      if (gen !== this.generation) return true;
      this.overview = o;
      this.add(first.files, true);
      this.changed();
      return true;
    } catch {
      return false; // the next check tries again
    }
  }

  /** Takes files out of the list, and out of their days' totals, after they were deleted. */
  remove(ids: readonly string[]): void {
    const gone = new Set(ids);
    const removed = this.files.filter((f) => gone.has(f.id));
    if (removed.length === 0) return;
    this.files = this.files.filter((f) => !gone.has(f.id));
    for (const f of removed) this.shown.delete(f.id);
    if (this.overview) {
      const days = this.overview.days.map((d) => {
        const out = removed.filter((f) => f.day === d.day);
        return { ...d, count: d.count - out.length, bytes: d.bytes - out.reduce((s, f) => s + f.size, 0) };
      });
      this.overview = { ...this.overview, days: days.filter((d) => d.count > 0) };
    }
    this.changed();
  }

  private add(files: FileInfo[], onTop: boolean): void {
    const fresh = files.filter((f) => !this.shown.has(f.id));
    for (const f of fresh) this.shown.add(f.id);
    this.files = onTop ? [...fresh, ...this.files] : [...this.files, ...fresh];
  }

  /** The loaded files by upload day, with the server's totals for each day. */
  get sections(): DaySection[] {
    const byDay = new Map<string, FileInfo[]>();
    for (const f of this.files) {
      const list = byDay.get(f.day);
      if (list) list.push(f);
      else byDay.set(f.day, [f]);
    }
    const totals = new Map((this.overview?.days ?? []).map((d) => [d.day, d]));
    return [...byDay].map(([day, files]) => ({
      day,
      files,
      count: totals.get(day)?.count ?? files.length,
      bytes: totals.get(day)?.bytes ?? files.reduce((s, f) => s + f.size, 0),
    }));
  }
}
