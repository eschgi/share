import Uppy from '@uppy/core';
import { describe, expect, it } from 'vitest';
import { doneBytes, linkLifeMs, missingParts, partSpan, s3FailureAction, Slots, stale } from '../src/s3parts';
import S3Upload, { type S3Transport } from '../src/s3upload';

describe('the parts of a file', () => {
  it('knows where each part is and what is left', () => {
    expect(partSpan(1, 25, 10)).toEqual([0, 10]);
    expect(partSpan(3, 25, 10)).toEqual([20, 25]);
    expect(missingParts(3, [2])).toEqual([1, 3]);
    expect(missingParts(0, [])).toEqual([]);
    expect(doneBytes([1, 3, 3], 25, 10)).toBe(15);
  });

  it('asks for links again before they run out', () => {
    expect(stale(1000, 1000 + linkLifeMs - 1)).toBe(false);
    expect(stale(1000, 1000 + linkLifeMs)).toBe(true);
  });

  it('knows what a failure means', () => {
    expect(s3FailureAction('bucket', 0)).toBe('refresh');
    expect(s3FailureAction('bucket', 403)).toBe('refresh');
    expect(s3FailureAction('bucket', 404)).toBe('resync');
    expect(s3FailureAction('bucket', 503)).toBe('retry');
    expect(s3FailureAction('server', 0)).toBe('retry');
    expect(s3FailureAction('server', 503, 's3_unavailable')).toBe('retry');
    expect(s3FailureAction('server', 409, 's3_parts_missing')).toBe('resync');
    expect(s3FailureAction('server', 409, 's3_upload_finished')).toBe('resync');
    expect(s3FailureAction('server', 404, 'not_found')).toBe('restart');
    expect(s3FailureAction('server', 404, 'folder_gone')).toBe('refuse');
    expect(s3FailureAction('server', 401, 'session_ended')).toBe('refuse');
    expect(s3FailureAction('server', 413, 'too_large')).toBe('refuse');
  });

  it('runs at most so many at once', async () => {
    const slots = new Slots(2);
    const order: string[] = [];
    const work = async (name: string, ms: number) => {
      const give = await slots.take();
      order.push(name + ' starts');
      await new Promise((r) => setTimeout(r, ms));
      order.push(name + ' ends');
      give();
      give(); // twice changes nothing
    };
    await Promise.all([work('a', 20), work('b', 5), work('c', 1)]);
    expect(order.slice(0, 2)).toEqual(['a starts', 'b starts']);
    expect(order.indexOf('c starts')).toBe(order.indexOf('b ends') + 1);
  });
});

interface FakeUpload {
  id: string;
  name: string;
  size: number;
  partSize: number;
  parts: number;
  folder?: string;
  lastModified?: number;
  done: Map<number, Uint8Array>;
  state: 'receiving' | 'complete';
}

/** The server's /api/s3/uploads and the bucket, in memory. */
class FakeS3 implements S3Transport {
  uploads = new Map<string, FakeUpload>();
  calls: string[] = [];
  bodies: unknown[] = [];
  partSize = 4;
  /** Every call to the API answers 401, as after the PIN's session ended. */
  sessionEnded = false;
  /** Answers for the next requests, before the logic: create, status, parts, complete, put <n>. */
  forced = new Map<string, { status: number; code?: string }[]>();
  putting = 0;
  mostPutting = 0;
  /** Called at each PUT, before it lands. */
  onPut?: (n: number) => void;
  isOnline = () => true;
  private next = 0;

  online = () => this.isOnline();
  sleep = async () => {};

  force(what: string, status: number, code?: string) {
    this.forced.set(what, [...(this.forced.get(what) ?? []), { status, code }]);
  }

  private take(what: string) {
    return this.forced.get(what)?.shift();
  }

  async api(method: string, path: string, body?: unknown) {
    this.calls.push(`${method} ${path}`);
    this.bodies.push(body);
    const fail = (status: number, code?: string) => ({ status, body: { error: { code, message: '' } } });
    if (this.sessionEnded && method !== 'DELETE') return fail(401, 'session_ended');
    if (method === 'POST' && path === '/api/s3/uploads') {
      const forced = this.take('create');
      if (forced) return fail(forced.status, forced.code);
      const b = body as { name: string; size: number; folder?: string; last_modified_ms?: number };
      const parts = Math.ceil(b.size / this.partSize);
      const up: FakeUpload = { id: 'file' + this.next++, name: b.name, size: b.size, partSize: this.partSize, parts, folder: b.folder, lastModified: b.last_modified_ms, done: new Map(), state: 'receiving' };
      this.uploads.set(up.id, up);
      return { status: 201, body: { id: up.id, part_size: up.partSize, parts, urls: this.links(up, missingParts(Math.min(parts, 10), [])), expires_at: '' } };
    }
    const m = path.match(/^\/api\/s3\/uploads\/([^/]+)(\/parts|\/complete)?$/);
    if (!m) return fail(404, 'not_found');
    const what = m[2] === '/parts' ? 'parts' : m[2] === '/complete' ? 'complete' : method === 'DELETE' ? 'delete' : 'status';
    const forced = this.take(what);
    if (forced) return fail(forced.status, forced.code);
    const up = this.uploads.get(decodeURIComponent(m[1]));
    if (!up) return fail(404, 'not_found');
    switch (what) {
      case 'status':
        return { status: 200, body: { id: up.id, state: up.state, size: up.size, part_size: up.partSize, parts: up.parts, done_parts: [...up.done.keys()].sort((a, b) => a - b) } };
      case 'parts':
        return { status: 200, body: { urls: this.links(up, (body as { parts: number[] }).parts), expires_at: '' } };
      case 'complete':
        if (missingParts(up.parts, up.done.keys()).length > 0) return fail(409, 's3_parts_missing');
        up.state = 'complete';
        return { status: 200, body: { id: up.id } };
      default:
        this.uploads.delete(up.id);
        return { status: 204, body: null };
    }
  }

  private links(up: FakeUpload, numbers: number[]) {
    return numbers.map((n) => ({ number: n, url: `bucket://${up.id}/${n}`, size: Math.min(up.partSize, up.size - (n - 1) * up.partSize) }));
  }

  async put(url: string, data: Blob, onProgress: (sent: number) => void, signal: AbortSignal) {
    const [, id, part] = url.match(/^bucket:\/\/([^/]+)\/(\d+)$/)!;
    const n = Number(part);
    this.calls.push(`PUT ${id} ${n}`);
    this.bodies.push(undefined);
    this.onPut?.(n);
    const forced = this.take('put ' + n);
    if (forced) return forced.status;
    this.putting++;
    this.mostPutting = Math.max(this.mostPutting, this.putting);
    await new Promise((r) => setTimeout(r, 2));
    this.putting--;
    if (signal.aborted) return 0;
    const up = this.uploads.get(id);
    if (!up) return 404;
    const bytes = new Uint8Array(await data.arrayBuffer());
    onProgress(bytes.length);
    up.done.set(n, bytes);
    return 200;
  }

  text(id: string) {
    const up = this.uploads.get(id)!;
    return [...up.done.entries()].sort(([a], [b]) => a - b).map(([, b]) => new TextDecoder().decode(b)).join('');
  }
}

function setup(fake = new FakeS3()) {
  const refused: [number, string | undefined][] = [];
  const uppy = new Uppy({ autoProceed: false });
  uppy.use(S3Upload, { transport: fake, retryDelays: [0, 0, 0], refused: (status, code) => refused.push([status, code]) });
  const add = (name: string, text: string, meta: Record<string, string> = {}) => uppy.addFile({ name, type: '', data: new Blob([text]), meta });
  return { uppy, fake, refused, add };
}

describe('sending into the bucket', () => {
  it('sends the parts straight to the bucket, then has them put together', async () => {
    const { uppy, fake, add } = setup();
    const id = add('IMG_1.jpg', '0123456789', { folder: 'f4mily' });
    const progress: number[] = [];
    let uploadURL: string | undefined;
    uppy.on('upload-progress', (_f, p) => progress.push(p.bytesUploaded));
    uppy.on('upload-success', (_f, res) => (uploadURL = res.uploadURL));
    const result = await uppy.upload();
    expect(result?.successful?.map((f) => f.id)).toEqual([id]);
    expect(fake.text('file0')).toBe('0123456789');
    expect(fake.calls).toEqual(['POST /api/s3/uploads', 'PUT file0 1', 'PUT file0 2', 'PUT file0 3', 'POST /api/s3/uploads/file0/complete']);
    expect(fake.bodies[0]).toMatchObject({ name: 'IMG_1.jpg', size: 10, folder: 'f4mily' });
    expect(fake.bodies.at(-1)).toEqual({});
    expect(uploadURL).toBe('/api/files/file0');
    expect(progress.at(-1)).toBe(10);
    expect(uppy.getFile(id).s3).toEqual({ id: 'file0', partSize: 4, parts: 3 });
  });

  it('asks for links beyond the first ten', async () => {
    const { uppy, fake, add } = setup();
    add('clip.mp4', 'x'.repeat(50));
    await uppy.upload();
    expect(fake.calls).toContain('POST /api/s3/uploads/file0/parts');
    expect(fake.bodies[fake.calls.indexOf('POST /api/s3/uploads/file0/parts')]).toEqual({ parts: [11, 12, 13] });
    expect(fake.text('file0')).toBe('x'.repeat(50));
  });

  it('comes back to where the bucket is', async () => {
    const fake = new FakeS3();
    const { uppy, add } = setup(fake);
    await fake.api('POST', '/api/s3/uploads', { name: 'a.jpg', size: 10 });
    fake.uploads.get('file0')!.done.set(1, new TextEncoder().encode('0123')).set(3, new TextEncoder().encode('89'));
    fake.calls = [];
    const id = add('a.jpg', '0123456789');
    uppy.setFileState(id, { s3: { id: 'file0', partSize: 4, parts: 3 } });
    await uppy.upload();
    expect(fake.calls).toEqual(['GET /api/s3/uploads/file0', 'POST /api/s3/uploads/file0/parts', 'PUT file0 2', 'POST /api/s3/uploads/file0/complete']);
    expect(fake.text('file0')).toBe('0123456789');
  });

  it('gets new links when the bucket refuses the old ones', async () => {
    const { uppy, fake, add } = setup();
    fake.force('put 1', 403);
    fake.force('put 2', 0); // R2's expired links, without CORS headers
    add('a.jpg', '0123456789');
    const result = await uppy.upload();
    expect(result?.successful).toHaveLength(1);
    expect(fake.calls.filter((c) => c === 'POST /api/s3/uploads/file0/parts')).toHaveLength(2);
    expect(fake.text('file0')).toBe('0123456789');
  });

  it('sends what is missing when completing says so', async () => {
    const { uppy, fake, add } = setup();
    let dropped = false;
    fake.onPut = (n) => {
      if (n === 3 && !dropped) {
        dropped = true;
        fake.uploads.get('file0')!.done.delete(2); // a part sent again, which then failed
      }
    };
    add('a.jpg', '0123456789');
    const result = await uppy.upload();
    expect(result?.successful).toHaveLength(1);
    // Its link from the start still works: no need to ask for another.
    expect(fake.calls.slice(4)).toEqual(['POST /api/s3/uploads/file0/complete', 'GET /api/s3/uploads/file0', 'PUT file0 2', 'POST /api/s3/uploads/file0/complete']);
  });

  it('starts over when the upload is gone', async () => {
    const { uppy, fake, add } = setup();
    const id = add('a.jpg', '0123');
    uppy.setFileState(id, { s3: { id: 'lost', partSize: 4, parts: 1 } });
    const result = await uppy.upload();
    expect(result?.successful).toHaveLength(1);
    expect(fake.calls[0]).toBe('GET /api/s3/uploads/lost');
    expect(uppy.getFile(id).s3?.id).toBe('file0');
  });

  it('ends the file for a 401 or a gone folder, and says so', async () => {
    const { uppy, fake, refused, add } = setup();
    fake.force('create', 404, 'folder_gone');
    add('a.jpg', '0123');
    let result = await uppy.upload();
    expect(result?.failed).toHaveLength(1);
    expect(refused).toEqual([[404, 'folder_gone']]);
    uppy.cancelAll();
    fake.sessionEnded = true;
    add('b.jpg', '4567');
    result = await uppy.upload();
    expect(result?.failed).toHaveLength(1);
    expect(refused.at(-1)).toEqual([401, 'session_ended']);
  });

  it('sends three files at a time', async () => {
    const { uppy, fake, add } = setup();
    for (const name of ['a', 'b', 'c', 'd', 'e']) add(name + '.jpg', '01234567');
    const result = await uppy.upload();
    expect(result?.successful).toHaveLength(5);
    expect(fake.mostPutting).toBe(3);
  });

  it('cancels on the server when a file is removed', async () => {
    const { uppy, fake, add } = setup();
    const id = add('a.jpg', '0123456789');
    fake.onPut = (n) => {
      if (n === 2) uppy.removeFile(id);
    };
    await uppy.upload();
    expect(fake.calls).toContain('DELETE /api/s3/uploads/file0');
    expect(fake.uploads.size).toBe(0);
  });

  it('fails a file whose data is gone, and gives up after the retries', async () => {
    const { uppy, fake, add } = setup();
    const ghost = add('ghost.jpg', '0123');
    uppy.setFileState(ghost, { data: undefined as unknown as Blob });
    for (let i = 0; i < 4; i++) fake.force('create', 503, 's3_unavailable');
    add('busy.jpg', '0123');
    const result = await uppy.upload();
    expect(result?.failed).toHaveLength(2);
    expect(fake.calls.filter((c) => c === 'POST /api/s3/uploads')).toHaveLength(4);
  });

  it('waits while offline', async () => {
    const { uppy, fake, add } = setup();
    let checks = 0;
    fake.isOnline = () => ++checks > 3;
    fake.force('put 1', 0);
    fake.force('put 1', 0);
    add('a.jpg', '0123');
    const result = await uppy.upload();
    expect(result?.successful).toHaveLength(1);
    expect(checks).toBeGreaterThan(3);
  });

  it('pauses and goes on where the bucket is', async () => {
    const { uppy, fake, add } = setup();
    let paused!: () => void;
    const pausedNow = new Promise<void>((r) => (paused = r));
    let once = true;
    fake.onPut = (n) => {
      if (n === 2 && once) {
        once = false;
        uppy.pauseAll();
        paused();
      }
    };
    add('a.jpg', '0123456789');
    const done = uppy.upload();
    await pausedNow;
    await new Promise((r) => setTimeout(r, 20));
    expect(fake.calls).not.toContain('PUT file0 3');
    uppy.resumeAll();
    const result = await done;
    expect(result?.successful).toHaveLength(1);
    expect(fake.calls).toContain('GET /api/s3/uploads/file0');
    expect(fake.text('file0')).toBe('0123456789');
  });
});
