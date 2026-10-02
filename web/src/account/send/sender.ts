// Sending while signed in: one uploader for all the account's pages, so uploads go on while the
// person looks at the library. It is started when the pages open, which also brings back a
// queue that a closed page interrupted. The screens' steps are the PIN pages' (state.ts).
import { useEffect, useState } from 'preact/hooks';
import { getInfo } from '../../api';
import { claimShared, sharedGone } from '../../incoming';
import { initialState, reduce, type Action, type State } from '../../state';
import { holdQueueLock, Uploader, type Snapshot } from '../../uploader';

let state: State = initialState;
let uploader: Uploader | null = null;
let rejected: string[] = [];
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
        onAllDone: (files, bytes) => dispatch({ type: 'allDone', files, bytes }),
        // The uploads were refused: this browser was signed out.
        onSessionEnded: () => whenSignedOut(),
        onRejected: (name) => {
          if (!rejected.includes(name)) rejected = [...rejected, name];
          changed();
        },
        onRestored: () => dispatch({ type: 'restored' }),
        onSharedGone: sharedGone,
      },
      keepQueue,
    );
    dispatch({ type: 'booted', session: { kind: 'account' }, sessionEnded: false });
    // Files shared from other apps go out right away, from whichever page this is.
    const shared = await claimShared();
    if (shared.length > 0) {
      uploader.addShared(shared);
      dispatch({ type: 'filesAdded' });
    }
  })().catch(() => {
    starting = null;
  });
}

/** Sends files; the screens and the drop zone offer this only once the uploader is ready. */
export function sendFiles(files: File[]): void {
  if (files.length === 0 || !uploader) return; // e.g. a dropped folder with nothing but hidden files
  rejected = [];
  uploader.add(files);
  dispatch({ type: 'filesAdded' });
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
  uploader?.retryFailed();
}

export function sendMore(): void {
  uploader?.clear();
  dispatch({ type: 'sendMore' });
}

export interface Sending {
  state: State;
  snapshot: Snapshot | null;
  rejected: string[];
}

/** The sending, kept up to date: redrawn at most once a frame while files go out. */
export function useSender(): Sending {
  const [, redraw] = useState(0);
  useEffect(() => {
    const l = () => redraw((n) => n + 1);
    listeners.add(l);
    return () => void listeners.delete(l);
  }, []);
  return { state, snapshot: uploader?.snapshot() ?? null, rejected };
}

/** Files are still on their way: closing the page would interrupt them. */
export function unfinished(s: Sending): boolean {
  const snap = s.snapshot;
  return s.state.screen === 'sending' && !!snap && snap.done + snap.failed + snap.ghosts.length < snap.total;
}
