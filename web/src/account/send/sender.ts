// Sending while signed in: one uploader for all the account's pages, so uploads go on while the
// person looks at the library. It is started when the pages open, which also brings back a
// queue that a closed page interrupted. The screens' steps are the PIN pages' (state.ts).
import { useEffect, useState } from 'preact/hooks';
import { getInfo } from '../../api';
import { claimShared, dropShared, sharedGone, type Shared } from '../../incoming';
import { notify } from '../../notify';
import { initialState, reduce, type Action, type State } from '../../state';
import { holdQueueLock, Uploader, type Snapshot } from '../../uploader';
import { hasChoices } from '../folders/folders';
import { foldersNow, refreshFolders } from '../folders/store';

let state: State = initialState;
let uploader: Uploader | null = null;
let rejected: string[] = [];
/** Files waiting for the person to say which folder they go into. */
let pending: { files: File[]; shared: Shared[] } | null = null;
/** Grows each time a folder turned out to be gone, for the pages to say so. */
let foldersGone = 0;
let starting: Promise<void> | null = null;
let whenSignedOut = () => {};
const listeners = new Set<() => void>();
const changed = () => listeners.forEach((l) => l());
const dispatch = (a: Action) => {
  state = reduce(state, a);
  changed();
};

/** Starts the uploader, once per page load; again after a failure, such as no connection. */
export function startSender(signedOut: () => void): void {
  whenSignedOut = signedOut;
  starting ??= (async () => {
    const [info, keepQueue] = await Promise.all([getInfo(), holdQueueLock()]);
    uploader = new Uploader(
      info,
      {
        onChange: changed,
        onAllDone: (files, bytes) => {
          dispatch({ type: 'allDone', files, bytes });
          void notify({ kind: 'sent', files, bytes });
        },
        // The uploads were refused: this browser was signed out.
        onSessionEnded: () => void notify({ kind: 'signedOut' }).finally(() => whenSignedOut()),
        onFailed: (failed) => void notify({ kind: 'failed', failed }),
        onRejected: (name) => {
          if (!rejected.includes(name)) rejected = [...rejected, name];
          changed();
        },
        onRestored: () => dispatch({ type: 'restored' }),
        onSharedGone: sharedGone,
        onFolderGone: () => {
          foldersGone++;
          void refreshFolders();
          changed();
        },
      },
      keepQueue,
    );
    await refreshFolders();
    dispatch({ type: 'booted', session: { kind: 'account' }, sessionEnded: false });
    // Files shared from other apps go out right away, from whichever page this is; with
    // several folders they wait on the Send tab until the person says which.
    const shared = await claimShared();
    if (shared.length > 0) {
      const { list, sendTo } = foldersNow();
      if (hasChoices(list) || sendTo === null) hold([], shared);
      else {
        uploader.addShared(shared, sendTo);
        dispatch({ type: 'filesAdded' });
      }
    }
  })().catch(() => {
    starting = null;
  });
}

/** Sends files into a folder, by default the one chosen for sending; the screens and the
 * drop zone offer this only once the uploader is ready. */
export function sendFiles(files: File[], folder = foldersNow().sendTo): void {
  if (files.length === 0 || !uploader) return; // e.g. a dropped folder with nothing but hidden files
  if (folder === null) return hold(files, []);
  rejected = [];
  uploader.add(files, folder);
  dispatch({ type: 'filesAdded' });
}

/** Keeps files until the person says which folder they go into. */
export function hold(files: File[], shared: Shared[]): void {
  if (files.length === 0 && shared.length === 0) return;
  pending = { files: [...(pending?.files ?? []), ...files], shared: [...(pending?.shared ?? []), ...shared] };
  changed();
}

/** Sends the files that waited, into folder. */
export function sendPending(folder: string): void {
  if (!pending || !uploader) return;
  const { files, shared } = pending;
  pending = null;
  rejected = [];
  uploader.add(files, folder);
  uploader.addShared(shared, folder);
  dispatch({ type: 'filesAdded' });
}

/** Doesn't send the files that waited; shared ones leave the inbox. */
export function dropPending(): void {
  if (!pending) return;
  void dropShared(pending.shared.map((s) => s.key));
  pending = null;
  changed();
}

export function continueRestored(): void {
  uploader?.continue();
  dispatch({ type: 'continued' });
}

export function startOver(): void {
  uploader?.startOver();
  dispatch({ type: 'startedOver' });
}

export function skipGhosts(): void {
  uploader?.skipGhosts();
  if (uploader?.snapshot().total === 0) dispatch({ type: 'startedOver' }); // nothing left
}

export function retryFailed(): void {
  uploader?.retryFailed(foldersNow().sendTo ?? undefined);
}

export function sendMore(): void {
  uploader?.clear();
  dispatch({ type: 'sendMore' });
}

export interface Sending {
  state: State;
  snapshot: Snapshot | null;
  rejected: string[];
  /** How many files wait for the person to say which folder they go into. */
  pending: number;
  /** Grows each time a folder turned out to be gone. */
  foldersGone: number;
}

/** The sending, kept up to date: redrawn at most once a frame while files go out. */
export function useSender(): Sending {
  const [, redraw] = useState(0);
  useEffect(() => {
    const l = () => redraw((n) => n + 1);
    listeners.add(l);
    return () => void listeners.delete(l);
  }, []);
  return { state, snapshot: uploader?.snapshot() ?? null, rejected, pending: (pending?.files.length ?? 0) + (pending?.shared.length ?? 0), foldersGone };
}

/** Files are still on their way: closing the page would interrupt them. */
export function unfinished(s: Sending): boolean {
  const snap = s.snapshot;
  return s.state.screen === 'sending' && !!snap && snap.done + snap.failed + snap.ghosts.length < snap.total;
}
