// What can be done with files: download them, one or as a ZIP, or one by one from a bucket;
// share them on a phone; delete them and bring them back (admins).
import { contentPath, contentUrl, createDownload, deleteFiles, getFile, getStorage, moveFiles, restoreFiles, zipUrl, type FileInfo, type ZipDownload } from '../../api';
import { decryptedBlob, streamUrl } from '../../e2ee/files';
import type { I18n } from '../../i18n';

/** The most files one ZIP takes (the server's limit). */
export const zipLimit = 10_000;

/** Lets the browser download from a link, as if it were clicked. */
export function startDownload(href: string, name = ''): void {
  // The service worker's decrypted downloads (e2ee/files.ts) need a navigation: Chrome sends a
  // link's download past the worker. The download outlives the frame.
  if (href.startsWith('/e2ee/')) {
    const f = document.createElement('iframe');
    f.hidden = true;
    f.src = href;
    document.body.append(f);
    setTimeout(() => f.remove(), 60_000);
    return;
  }
  const a = document.createElement('a');
  a.href = href;
  a.download = name; // empty: the name the server gives
  a.hidden = true;
  document.body.append(a);
  a.click();
  a.remove();
}

/** Where a file downloads from: the server for a plain file; for an encrypted one this page's
 * service worker, which decrypts it on the way (e2ee/files.ts). */
export type Href = (id: string) => Promise<string>;

export const plainHref: Href = async (id) => contentPath(id);

/** Where the files of a download's list download from. */
export function hrefsOf(files: ZipDownload['files']): Href {
  const enc = new Map(files.filter((f) => f.enc).map((f) => [f.id, f]));
  return async (id) => {
    const f = enc.get(id);
    if (!f?.enc) return contentPath(id);
    return streamUrl({ id: f.id, folder: f.folder, enc: f.enc, size: f.size, name: f.path.split('/').pop() ?? f.id, mime: '' }, true);
  };
}

/** One file as it is, decrypted if it is encrypted; several as one ZIP, which the browser's
 * download list shows and resumes. The ZIP leaves encrypted files out: with any among them,
 * the answer's each says where to download them all from, one by one. */
export async function download(ids: string[], one?: FileInfo): Promise<{ zip: ZipDownload; each: Href | null } | null> {
  if (ids.length === 1) {
    const f = one?.id === ids[0] ? one : await getFile(ids[0]);
    if (f.enc) startDownload(await streamUrl({ ...f, enc: f.enc }, true), f.name);
    else startDownload(contentPath(f.id), f.name);
    return null;
  }
  const zip = await createDownload(ids);
  if (zip.files.some((f) => f.enc)) return { zip, each: hrefsOf(zip.files) };
  startDownload(zipUrl(zip), zip.name);
  return { zip, each: null };
}

/** The most files that download one by one at a time. */
export const eachLimit = 100;

/** iPhones and iPads start one download per tap, not a row of them. */
export function tapEach(): boolean {
  return /iPad|iPhone|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
}

export interface EachOptions {
  /** Hears each file as it starts: how many have, of how many. */
  onStart?: (started: number, total: number) => void;
  signal?: AbortSignal;
  /** Lets the browser download a link; the tests watch instead. */
  start?: (href: string) => void;
  wait?: (ms: number) => Promise<void>;
  /** Where each file downloads from; the server's /content unless said. */
  href?: Href;
}

/**
 * Downloads files one by one, each straight from the bucket after the server's redirect: a
 * bucket sends no ZIP. One a second, so the browser keeps up with them; the first in the click,
 * where the browser allows it. Resolves with how many started.
 */
export async function downloadOneByOne(ids: string[], opts: EachOptions = {}): Promise<number> {
  const start = opts.start ?? ((href: string) => startDownload(href));
  const wait = opts.wait ?? ((ms: number) => new Promise<void>((r) => setTimeout(r, ms)));
  const total = Math.min(ids.length, eachLimit);
  let started = 0;
  const href = opts.href ?? plainHref;
  for (const id of ids.slice(0, total)) {
    if (started > 0) await wait(1000);
    if (opts.signal?.aborted) break;
    start(await href(id));
    started++;
    opts.onStart?.(started, total);
  }
  return started;
}

/** A line about downloads one by one, for a toast or a page. */
export interface EachNote {
  text: string;
  action?: { label: string; run: () => void };
  /** Waits for a tap: it stays until it is used. */
  sticky?: boolean;
}

/**
 * Downloads files one by one and says how it goes: a second apart, with a way to stop; on an
 * iPhone or iPad one per tap, which each note asks for. The last note downloads them again.
 */
export function downloadEach(ids: string[], note: (n: EachNote) => void, i18n: Pick<I18n, 't' | 'tn'>, href: Href = plainHref): void {
  const { t, tn } = i18n;
  const total = Math.min(ids.length, eachLimit);
  const again = { label: t('zip.again'), run: () => downloadEach(ids, note, i18n, href) };
  if (tapEach()) {
    const next = (started: number) => {
      if (started === total) return note({ text: tn('each.started', total), action: again });
      const tap = () => {
        // In the tap, which the browser asks for; an encrypted file's address comes a moment later.
        void href(ids[started]).then((h) => startDownload(h));
        next(started + 1);
      };
      note({ text: t('each.next', { n: started, total }), action: { label: t('each.nextButton'), run: tap }, sticky: true });
    };
    return next(0);
  }
  const stop = new AbortController();
  const cancel = { label: t('common.cancel'), run: () => stop.abort() };
  note({ text: t('each.starting', { n: 1, total }), action: cancel });
  void downloadOneByOne(ids, {
    signal: stop.signal,
    href,
    onStart: (n) => {
      if (n < total) note({ text: t('each.starting', { n: n + 1, total }), action: cancel });
    },
  }).then((n) => note({ text: tn('each.started', n), action: again }));
}

/** Sends ids in parts of 1000, the most the server takes at once; says how many changed. */
export async function inParts(ids: string[], send: (part: string[]) => Promise<{ changed: number }>): Promise<number> {
  let changed = 0;
  for (let i = 0; i < ids.length; i += 1000) changed += (await send(ids.slice(i, i + 1000))).changed;
  return changed;
}

export const deleteMany = (ids: string[]) => inParts(ids, deleteFiles);
export const restoreMany = (ids: string[]) => inParts(ids, restoreFiles);
export const moveMany = (ids: string[], folder: string) => inParts(ids, (part) => moveFiles(part, folder));

let trashDays = 30;
let askedTrashDays = false;

/** How long Recently deleted keeps files: 30 days until the server says otherwise, as in the app. */
export function knownTrashDays(): number {
  return trashDays;
}

/** Asks the server once how long Recently deleted keeps files. */
export async function learnTrashDays(): Promise<number> {
  if (!askedTrashDays) {
    askedTrashDays = true;
    try {
      trashDays = (await getStorage()).trash_days;
    } catch {
      askedTrashDays = false;
    }
  }
  return trashDays;
}

/** Sharing files from a phone, with the phone's own share sheet (on an iPhone: Save to Photos). */
export const shareLimit = { files: 10, bytes: 50_000_000 };

export function canShareFiles(): boolean {
  if (typeof navigator.canShare !== 'function' || !matchMedia('(pointer: coarse)').matches) return false;
  try {
    return navigator.canShare({ files: [new File([''], 'photo.jpg', { type: 'image/jpeg' })] });
  } catch {
    return false;
  }
}

/** The files themselves, for the share sheet. */
export async function filesToShare(files: FileInfo[]): Promise<File[]> {
  return Promise.all(
    files.map(async (f) => {
      if (f.enc) return new File([await decryptedBlob({ ...f, enc: f.enc })], f.name, { type: f.mime || 'application/octet-stream' });
      const res = await fetch(contentUrl(f));
      if (!res.ok) throw new Error(`${res.status}`);
      return new File([await res.blob()], f.name, { type: f.mime || 'application/octet-stream' });
    }),
  );
}
