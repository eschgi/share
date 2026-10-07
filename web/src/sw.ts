/// <reference lib="webworker" />
// The service worker. Golden Retriever's part keeps picked files in memory while the page
// reloads, and takes over new versions right away. Ours keeps the page and its assets, so the
// site (and an installed app) opens even without a connection and shows what is waiting, and
// takes files shared from other apps. Uploads and the API always go straight to the network.
import '@uppy/golden-retriever/lib/ServiceWorker.js';
import { chunkSize, cipherRange, ContentCipher, decryptStream } from './e2ee/content';
import { byteRange, parseRange, type StreamOrder } from './e2ee/stream';
import { stash } from './inbox';
import { isAppPath, shareTarget } from './paths';

declare const self: ServiceWorkerGlobalScope;

const cacheName = 'share-v1';

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((names) => Promise.all(names.filter((n) => n !== cacheName).map((n) => caches.delete(n)))),
  );
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;
  if (req.method === 'POST' && url.pathname === shareTarget.path) {
    event.respondWith(receiveShare(req));
    return;
  }
  if (req.method !== 'GET') return;
  if (url.pathname.startsWith('/e2ee/')) {
    event.respondWith(decrypted(req, url));
  } else if (req.mode === 'navigate' && isAppPath(url.pathname)) {
    event.respondWith(page(req));
  } else if (url.pathname.startsWith('/assets/')) {
    event.respondWith(asset(req));
  } else if (url.pathname === '/theme-boot.js') {
    event.respondWith(fresh(req));
  }
});

// Encrypted files (docs/e2ee-plan.md): a page tells the worker a file's key and where its bytes
// are, under a random token (e2ee/files.ts), and GET /e2ee/<token>/<name> gives the file
// decrypted while it streams, with ranges, so a video seeks and a download resumes. The worker
// keeps that for 12 hours at most, and forgets it when it stops.
const streams = new Map<string, StreamOrder & { at: number }>();
const streamLife = 12 * 3600_000;
/** Responses still streaming; the page keeps the worker awake while there are any. */
let open = 0;

self.addEventListener('message', (event) => {
  const o = event.data as StreamOrder | { type: 'e2ee-ping' } | null;
  if (o?.type === 'e2ee-ping') return event.ports[0]?.postMessage(open);
  if (o?.type !== 'e2ee-stream') return;
  for (const [token, s] of streams) if (Date.now() - s.at > streamLife) streams.delete(token);
  streams.set(o.token, { ...o, at: Date.now() });
  event.ports[0]?.postMessage('ok');
});

/** body, counted in open until it ends or the reader stops. */
function counted(body: ReadableStream<Uint8Array>): ReadableStream<Uint8Array> {
  const reader = body.getReader();
  let counting = true;
  const end = () => {
    if (counting) open--;
    counting = false;
  };
  open++;
  return new ReadableStream({
    async pull(c) {
      try {
        const r = await reader.read();
        if (r.done) {
          end();
          c.close();
        } else c.enqueue(r.value);
      } catch (e) {
        end();
        c.error(e);
      }
    },
    cancel(reason) {
      end();
      return reader.cancel(reason);
    },
  });
}

async function decrypted(req: Request, url: URL): Promise<Response> {
  const s = streams.get(url.pathname.split('/')[2]);
  if (!s) return new Response('This link only works in the tab that made it.', { status: 404 });
  const range = parseRange(req.headers.get('Range'), s.size);
  if (range === 'unsatisfiable') return new Response(null, { status: 416, headers: { 'Content-Range': `bytes */${s.size}` } });
  const { start, end } = range ?? { start: 0, end: s.size };
  const cipher = await ContentCipher.create(s.key, s.header, s.size);
  const r = cipherRange(s.size, start, end);
  const last = Math.max(r.first, Math.floor((Math.max(end, 1) - 1) / chunkSize));
  const res = await fetch(s.src, { headers: { Range: `bytes=${r.from}-${r.to - 1}` }, credentials: 'same-origin' });
  if ((res.status !== 200 && res.status !== 206) || !res.body) return new Response(null, { status: res.status === 404 ? 404 : 502 });
  let body: ReadableStream<Uint8Array> = res.body;
  if (res.status === 200) body = body.pipeThrough(byteRange(r.from, r.to - r.from)); // the range was ignored
  const headers: Record<string, string> = {
    'Content-Type': s.mime || 'application/octet-stream',
    'Content-Length': String(end - start),
    'Accept-Ranges': 'bytes',
    'Cache-Control': 'no-store',
  };
  if (s.attachment) headers['Content-Disposition'] = `attachment; filename*=UTF-8''${encodeURIComponent(s.name)}`;
  if (range) headers['Content-Range'] = `bytes ${start}-${end - 1}/${s.size}`;
  const plain = body.pipeThrough(decryptStream(cipher, r.first, last + 1)).pipeThrough(byteRange(r.skip, end - start));
  return new Response(counted(plain), { status: range ? 206 : 200, headers });
}

// A tap on a notification (notify.ts) brings the page back, or opens it.
self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const path = (event.notification.data as { url?: string } | null)?.url ?? '/';
  event.waitUntil(
    (async () => {
      const tabs = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
      const tab = tabs.find((c) => new URL(c.url).pathname === path) ?? tabs[0];
      if (tab) await tab.focus();
      else await self.clients.openWindow(path);
    })(),
  );
});

/**
 * Files from Android's share sheet: kept in the inbox, all of them or none, and the Send page
 * sends them. They never go to the server this way.
 */
async function receiveShare(req: Request): Promise<Response> {
  try {
    const form = await req.formData();
    const files = form.getAll(shareTarget.field).filter((f): f is File => f instanceof File && f.name !== '');
    if (files.length > 0) await stash(files, crypto.randomUUID(), Date.now());
    return Response.redirect('/send', 303);
  } catch {
    return Response.redirect('/send?share=failed', 303);
  }
}

/** A file without a hash in its name: fresh when possible, the last copy otherwise. */
async function fresh(req: Request): Promise<Response> {
  const cache = await caches.open(cacheName);
  try {
    const res = await fetch(req);
    if (res.ok) await cache.put(req, res.clone());
    return res;
  } catch (err) {
    const cached = await cache.match(req);
    if (cached) return cached;
    throw err;
  }
}

/**
 * The page: fresh from the network when possible, the last copy otherwise. That includes
 * Cloudflare's error pages (502, 530) for when the server or the tunnel is down. Every path of
 * ours (paths.ts) gets the same page, so one copy, kept as '/', serves them all.
 */
async function page(req: Request): Promise<Response> {
  const cache = await caches.open(cacheName);
  let res: Response;
  try {
    res = await fetch(req);
  } catch (err) {
    const cached = await cache.match('/');
    if (cached) return cached;
    throw err;
  }
  if (res.ok) {
    const old = await cache.match('/');
    const fresh = await res.clone().text();
    if (old && (await old.text()) !== fresh) await dropAssets(cache); // a new version: new asset names
    await cache.put('/', res.clone());
  } else if (res.status >= 500) {
    const cached = await cache.match('/');
    if (cached) return cached;
  }
  return res;
}

/** Assets carry a hash in their name and never change, so a stored copy is always right. */
async function asset(req: Request): Promise<Response> {
  const cache = await caches.open(cacheName);
  const hit = await cache.match(req);
  if (hit) return hit;
  const res = await fetch(req);
  if (res.ok) await cache.put(req, res.clone());
  return res;
}

async function dropAssets(cache: Cache): Promise<void> {
  for (const req of await cache.keys()) {
    if (new URL(req.url).pathname.startsWith('/assets/')) await cache.delete(req);
  }
}
