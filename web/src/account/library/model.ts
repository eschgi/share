// The library's files, loaded page by page in the order of the sort, as the app's
// LibraryController does: the server's group totals, paging, and new files put on top when the
// library changes. The requests come in from outside, so the tests can answer them.
import { groupOf, type FileInfo, type FilePage, type LibraryFilter, type LibraryOverview } from '../../api';

export interface LibraryRequests {
  overview(f: LibraryFilter): Promise<LibraryOverview>;
  page(f: LibraryFilter, cursor: string | null, limit: number): Promise<FilePage>;
}

/** One group, a day or a sender: its loaded files, and the server's totals for all of it. */
export interface GroupSection {
  key: string;
  files: FileInfo[];
  count: number;
  bytes: number;
  /** The day its newest file arrived. */
  lastDay: string;
  /** A sender's name; null for days, PIN guests and someone no longer here. */
  from: string | null;
}

const pageSize = 200;
const newerPageSize = 100;

/** Whether two filters show the same: the same folder, kind and words, sorted the same. */
export function sameFilter(a: LibraryFilter, b: LibraryFilter): boolean {
  return (
    a.folder === b.folder &&
    a.kind === b.kind &&
    a.q.trim() === b.q.trim() &&
    (a.sort ?? 'received') === (b.sort ?? 'received') &&
    (a.order ?? 'newest') === (b.order ?? 'newest')
  );
}

/** Some group has fewer files than before, or none: files were moved away or hidden, which
 * adding new files on top can't show. */
export function shrank(before: LibraryOverview, after: LibraryOverview): boolean {
  const now = new Map(after.groups.map((g) => [g.key, g.count]));
  return before.groups.some((g) => (now.get(g.key) ?? 0) < g.count);
}

/** New files come on top only by the day they arrived, newest first; in any other order they
 * may belong anywhere, and the list starts over. */
function newOnTop(f: LibraryFilter): boolean {
  return (f.sort ?? 'received') === 'received' && (f.order ?? 'newest') === 'newest';
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
      if (shrank(current, o) || !newOnTop(this.filter)) {
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

  /** Takes files out of the list, and out of their groups' totals, after they were deleted. */
  remove(ids: readonly string[]): void {
    const gone = new Set(ids);
    const removed = this.files.filter((f) => gone.has(f.id));
    if (removed.length === 0) return;
    this.files = this.files.filter((f) => !gone.has(f.id));
    for (const f of removed) this.shown.delete(f.id);
    if (this.overview) {
      const groups = this.overview.groups.map((g) => {
        const out = removed.filter((f) => groupOf(f, this.filter.sort) === g.key);
        return { ...g, count: g.count - out.length, bytes: g.bytes - out.reduce((s, f) => s + f.size, 0) };
      });
      this.overview = { ...this.overview, groups: groups.filter((g) => g.count > 0) };
    }
    this.changed();
  }

  private add(files: FileInfo[], onTop: boolean): void {
    const fresh = files.filter((f) => !this.shown.has(f.id));
    for (const f of fresh) this.shown.add(f.id);
    this.files = onTop ? [...fresh, ...this.files] : [...this.files, ...fresh];
  }

  /** The loaded files by group, in the server's order, with its totals for each group. */
  get sections(): GroupSection[] {
    const byGroup = new Map<string, FileInfo[]>();
    for (const f of this.files) {
      const key = groupOf(f, this.filter.sort);
      const list = byGroup.get(key);
      if (list) list.push(f);
      else byGroup.set(key, [f]);
    }
    const totals = new Map((this.overview?.groups ?? []).map((g) => [g.key, g]));
    return [...byGroup].map(([key, files]) => {
      const g = totals.get(key);
      return {
        key,
        files,
        count: g?.count ?? files.length,
        bytes: g?.bytes ?? files.reduce((s, f) => s + f.size, 0),
        lastDay: g?.last_day ?? files[0].day,
        from: g?.from ?? null,
      };
    });
  }
}
