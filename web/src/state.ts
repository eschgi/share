// Which screen shows, as a pure reducer. Upload progress itself lives in Uppy; this only
// knows about PIN sessions and the big steps (screens 1–4 and 6 of the mockup).
import type { Session } from './api';

export type Screen = 'boot' | 'pin' | 'ready' | 'sending' | 'done';

export type PinProblem =
  | { kind: 'wrong'; attemptsLeft: number | null }
  | { kind: 'locked'; until: number } // epoch ms
  | { kind: 'ended' }
  | { kind: 'sessionEnded' }
  | { kind: 'network' };

export interface State {
  screen: Screen;
  session: Session | null;
  unlocking: boolean;
  problem: PinProblem | null;
  /** Uploads are waiting for a new PIN because the old one ended mid-way. */
  waitingForPin: boolean;
  done: { files: number; bytes: number } | null;
}

export type Action =
  | { type: 'booted'; session: Session | null; sessionEnded: boolean }
  | { type: 'bootFailed' }
  | { type: 'unlockStarted' }
  | { type: 'unlocked'; session: Session }
  | { type: 'unlockFailed'; problem: PinProblem }
  | { type: 'filesAdded' }
  | { type: 'allDone'; files: number; bytes: number }
  | { type: 'sessionEnded' }
  | { type: 'sendMore' };

export const initialState: State = {
  screen: 'boot',
  session: null,
  unlocking: false,
  problem: null,
  waitingForPin: false,
  done: null,
};

export function reduce(s: State, a: Action): State {
  switch (a.type) {
    case 'booted':
      if (a.session) return { ...s, screen: 'ready', session: a.session };
      return { ...s, screen: 'pin', problem: a.sessionEnded ? { kind: 'sessionEnded' } : null };
    case 'bootFailed':
      return { ...s, screen: 'pin', problem: { kind: 'network' } };
    case 'unlockStarted':
      return { ...s, unlocking: true, problem: null };
    case 'unlocked':
      return {
        ...s,
        unlocking: false,
        problem: null,
        session: a.session,
        screen: s.waitingForPin ? 'sending' : 'ready',
        waitingForPin: false,
      };
    case 'unlockFailed':
      return { ...s, unlocking: false, screen: 'pin', problem: a.problem };
    case 'filesAdded':
      return s.screen === 'pin' ? s : { ...s, screen: 'sending', done: null };
    case 'allDone':
      return s.waitingForPin ? s : { ...s, screen: 'done', done: { files: a.files, bytes: a.bytes } };
    case 'sessionEnded':
      return { ...s, screen: 'pin', session: null, waitingForPin: s.screen === 'sending' || s.waitingForPin, problem: { kind: 'sessionEnded' } };
    case 'sendMore':
      return { ...s, screen: 'ready', done: null };
  }
}
