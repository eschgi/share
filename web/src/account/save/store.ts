// The folder saving that is running, if any. It lives outside the pages, so it goes on while
// the person looks at other ones; the panel (SavePanel.tsx) shows it on all of them.
import { useEffect, useState } from 'preact/hooks';
import { saveFiles, type SaveItem, type SaveState } from './engine';
import { fetchFile, folderOf } from './folder';
import { notify } from '../../notify';
import { markSaved } from './marks';

export interface SaveRun {
  /** The picked folder's name. */
  folder: string;
  state: SaveState;
  /** Folded into a small button. */
  hidden: boolean;
}

let run: SaveRun | null = null;
let controller: AbortController | null = null;
let again: (() => void) | null = null;
let frame = 0;
const listeners = new Set<() => void>();
const changed = () => listeners.forEach((l) => l());

export function startSave(items: SaveItem[], dir: FileSystemDirectoryHandle): void {
  controller?.abort();
  const ctl = (controller = new AbortController());
  again = () => startSave(items, dir);
  run = {
    folder: dir.name,
    hidden: false,
    state: {
      total: items.length,
      done: 0,
      skipped: 0,
      failed: [],
      bytesTotal: items.reduce((s, f) => s + f.size, 0),
      bytesDone: 0,
      waiting: false,
      stopped: null,
      finished: false,
    },
  };
  changed();
  void saveFiles(items, folderOf(dir), fetchFile, {
    signal: ctl.signal,
    onSaved: (item, path) => markSaved(item.id, { path, size: item.size }, dir.name),
    onChange: (state) => {
      if (!run || controller !== ctl) return;
      run = { ...run, state };
      if (state.finished) told(state, dir.name);
      // Many small steps a second: drawn at most once a frame, and the end at once.
      if (state.finished) changed();
      else frame ||= requestAnimationFrame(() => {
        frame = 0;
        changed();
      });
    },
  });
}

/** Says how saving ended, if the page is in the background; not when it was cancelled here. */
function told(s: SaveState, folder: string): void {
  if (s.stopped === 'cancelled') return;
  void notify(s.stopped ? { kind: 'saveStopped', why: s.stopped } : { kind: 'saved', saved: s.done - s.skipped, folder });
}

/** Stops saving and puts the panel away. */
export function stopSave(): void {
  controller?.abort();
  controller = null;
  run = null;
  changed();
}

/** Saves the same files again: those there by now are skipped. */
export function saveAgain(): void {
  again?.();
}

export function hideSave(hidden: boolean): void {
  if (!run) return;
  run = { ...run, hidden };
  changed();
}

export function useSaveRun(): SaveRun | null {
  const [, redraw] = useState(0);
  useEffect(() => {
    const l = () => redraw((n) => n + 1);
    listeners.add(l);
    return () => void listeners.delete(l);
  }, []);
  return run;
}
