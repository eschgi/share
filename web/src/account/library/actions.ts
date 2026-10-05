// What can be done with files: download them, one or as a ZIP; share them on a phone; delete
// them and bring them back (admins).
import { contentPath, contentUrl, createDownload, deleteFiles, getStorage, moveFiles, restoreFiles, zipUrl, type FileInfo, type ZipDownload } from '../../api';

/** The most files one ZIP takes (the server's limit). */
export const zipLimit = 10_000;

/** Lets the browser download from a link, as if it were clicked. */
export function startDownload(href: string, name = ''): void {
  const a = document.createElement('a');
  a.href = href;
  a.download = name; // empty: the name the server gives
  a.hidden = true;
  document.body.append(a);
  a.click();
  a.remove();
}

/** One file as it is; several as one ZIP, which the browser's download list shows and resumes. */
export async function download(ids: string[], one?: FileInfo): Promise<ZipDownload | null> {
  if (ids.length === 1) {
    startDownload(contentPath(ids[0]), one?.name);
    return null;
  }
  const zip = await createDownload(ids);
  startDownload(zipUrl(zip), zip.name);
  return zip;
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
      const res = await fetch(contentUrl(f));
      if (!res.ok) throw new Error(`${res.status}`);
      return new File([await res.blob()], f.name, { type: f.mime || 'application/octet-stream' });
    }),
  );
}
