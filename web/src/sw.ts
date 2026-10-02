/// <reference lib="webworker" />
// The service worker. Golden Retriever's part keeps picked files in memory while the page
// reloads, and takes over new versions right away. Ours keeps the page and its assets, so the
// site (and an installed app) opens even without a connection and shows what is waiting, and
// takes files shared from other apps. Uploads and the API always go straight to the network.
import '@uppy/golden-retriever/lib/ServiceWorker.js';
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
  if (req.mode === 'navigate' && isAppPath(url.pathname)) {
    event.respondWith(page(req));
  } else if (url.pathname.startsWith('/assets/')) {
    event.respondWith(asset(req));
  } else if (url.pathname === '/theme-boot.js') {
    event.respondWith(fresh(req));
  }
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
