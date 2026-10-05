// What sending a file into a bucket needs apart from Uppy and the network: where its parts
// are, which are left, when links are too old, and what a failed request means.

/** Where part n (from 1) is in a file: from start up to, not including, end. */
export function partSpan(n: number, size: number, partSize: number): [start: number, end: number] {
  const start = (n - 1) * partSize;
  return [start, Math.min(start + partSize, size)];
}

/** The parts still to send, in order. */
export function missingParts(parts: number, done: Iterable<number>): number[] {
  const have = new Set(done);
  const out: number[] = [];
  for (let n = 1; n <= parts; n++) if (!have.has(n)) out.push(n);
  return out;
}

/** How many bytes the done parts hold. */
export function doneBytes(done: Iterable<number>, size: number, partSize: number): number {
  let bytes = 0;
  for (const n of new Set(done)) {
    const [start, end] = partSpan(n, size, partSize);
    bytes += Math.max(0, end - start);
  }
  return bytes;
}

/** Links for parts work for an hour; after 45 minutes they are asked for again, measured on a
 * clock that a changed system time doesn't move. */
export const linkLifeMs = 45 * 60_000;

export function stale(fetchedAt: number, now = performance.now()): boolean {
  return now - fetchedAt >= linkLifeMs;
}

/** What to do after a request of an upload into the bucket failed:
 * - retry: the same again, after a pause;
 * - refresh: ask the server for new links, then send the part again;
 * - resync: ask the server which parts the bucket has, and go on from there;
 * - restart: the upload is gone; start the file again;
 * - refuse: give up on the file, the way the tus rules do (signed out, too big, …). */
export type S3Action = 'retry' | 'refresh' | 'resync' | 'restart' | 'refuse';

/** source says who answered: the bucket (a part's PUT) or the server. status 0 is no answer at
 * all, which from the bucket also means a link that ran out: R2 answers that with a 403 the
 * browser can't read, having no CORS headers. */
export function s3FailureAction(source: 'bucket' | 'server', status: number, code?: string): S3Action {
  if (source === 'bucket') {
    if (status === 0 || status === 401 || status === 403) return 'refresh';
    if (status === 404) return 'resync'; // NoSuchUpload: the server says whether it is gone
    return 'retry';
  }
  if (status === 0 || status === 408 || status === 429 || status >= 500) return 'retry';
  if (status === 404 && code !== 'folder_gone') return 'restart';
  if (status === 409 && (code === 's3_parts_missing' || code === 's3_upload_finished')) return 'resync';
  return 'refuse';
}

/** At most limit things at once; the others wait their turn. */
export class Slots {
  private running = 0;
  private waiting: (() => void)[] = [];

  constructor(readonly limit: number) {}

  /** Waits for a free slot; call what it returns to give it back. */
  async take(): Promise<() => void> {
    if (this.running >= this.limit) await new Promise<void>((resolve) => this.waiting.push(resolve));
    else this.running++;
    let given = false;
    return () => {
      if (given) return;
      given = true;
      const next = this.waiting.shift();
      if (next) next();
      else this.running--;
    };
  }
}
