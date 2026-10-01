// Files dragged onto the page or picked as a whole folder: which drags carry files, and which
// files of a folder are worth sending.

/** Whether a drag carries files, rather than text or a link from a page. */
export function isFileDrag(types: ArrayLike<string> | null | undefined): boolean {
  return !!types && Array.from(types).includes('Files');
}

/** Where a file sat in what was dropped or picked: "/Holiday/IMG_1.jpg", "Holiday/IMG_1.jpg". */
export interface PathFile {
  name: string;
  /** Set by Uppy's getDroppedFiles for files inside a dropped folder; null at the top. */
  relativePath?: string | null;
  /** Set by a folder picker (webkitdirectory). */
  webkitRelativePath?: string;
}

// What operating systems, NAS boxes and cameras leave in folders; nobody means to send these.
const junkNames = new Set(['thumbs.db', 'desktop.ini', 'icon\r']);
const junkFolders = new Set(['__macosx', '@eadir', '$recycle.bin', 'system volume information']);

/**
 * The files of a folder without hidden ones (.DS_Store, ._IMG_1.jpg, .Trashes/…) and the systems'
 * own (Thumbs.db, desktop.ini, Synology's @eaDir/…). Files dropped or picked one by one are all
 * kept, whatever their name.
 */
export function skipJunk<T extends PathFile>(files: T[]): T[] {
  return files.filter((f) => {
    const parts = (f.relativePath || f.webkitRelativePath || '').split('/').filter(Boolean);
    if (parts.length < 2) return true; // not inside a folder
    if (parts.some((p) => p.startsWith('.'))) return false;
    if (parts.slice(0, -1).some((p) => junkFolders.has(p.toLowerCase()))) return false;
    return !junkNames.has(parts[parts.length - 1].toLowerCase());
  });
}
