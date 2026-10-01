// Which screen shows, as a pure reducer. Upload progress itself lives in Uppy; this only
// knows about PIN sessions and the big steps (screens 1–6 of the mockup).
import type { Session } from './api';

export type Screen = 'boot' | 'pin' | 'ready' | 'welcome' | 'sending' | 'done';

export type PinProblem =
  | { kind: 'wrong'; attemptsLeft: number | null }
  | { kind: 'locked'; until: number } // epoch ms
  | { kind: 'ended' }
  | { kind: 'sessionEnded' }
  /** Uploads came back unauthorized: the browser no longer sends a session, e.g. after clearing cookies. */
  | { kind: 'sessionLost' }
  /** The PIN was right, but the browser didn't keep the session cookie. */
  | { kind: 'noCookie' }
  /** A plain-http page (not localhost): browsers keep no secure cookies there, so no PIN can work. */
  | { kind: 'insecure' }
  | { kind: 'network' };

export interface State {
  screen: Screen;
  session: Session | null;
  unlocking: boolean;
  problem: PinProblem | null;
  /** Uploads are waiting for a new PIN because the old one ended mid-way. */
  waitingForPin: boolean;
  /** An upload interrupted by a closed page came back; screen 5 offers to continue it. */
  restored: boolean;
  done: { files: number; bytes: number } | null;
}

export type Action =
  | { type: 'booted'; session: Session | null; sessionEnded: boolean }
  | { type: 'bootFailed' }
  | { type: 'insecure' }
  | { type: 'restored' }
  | { type: 'unlockStarted' }
  | { type: 'unlocked'; session: Session }
  | { type: 'unlockFailed'; problem: PinProblem }
  | { type: 'continued' }
  | { type: 'startedOver' }
  | { type: 'filesAdded' }
  | { type: 'allDone'; files: number; bytes: number }
  /** lost: the server got no session at all, rather than one whose PIN ended. */
  | { type: 'sessionEnded'; lost?: boolean }
  | { type: 'sendMore' };

export const initialState: State = {
  screen: 'boot',
  session: null,
  unlocking: false,
  problem: null,
  waitingForPin: false,
  restored: false,
  done: null,
};

export function reduce(s: State, a: Action): State {
  switch (a.type) {
    case 'booted':
      if (a.session) return { ...s, screen: s.restored ? 'welcome' : 'ready', session: a.session };
      return { ...s, screen: 'pin', problem: a.sessionEnded ? { kind: 'sessionEnded' } : null };
    case 'bootFailed':
      return { ...s, screen: 'pin', problem: { kind: 'network' } };
    case 'insecure':
      return { ...s, screen: 'pin', problem: { kind: 'insecure' } };
    case 'restored':
      // The restore can finish before or after the session check. Before sending starts it
      // leads to screen 5, now or right after the PIN; once sending runs it changes nothing.
      if (s.screen === 'sending' || s.screen === 'done') return s;
      return { ...s, restored: true, screen: s.screen === 'ready' ? 'welcome' : s.screen };
    case 'unlockStarted':
      return { ...s, unlocking: true, problem: null };
    case 'unlocked':
      return {
        ...s,
        unlocking: false,
        problem: null,
        session: a.session,
        screen: s.waitingForPin ? 'sending' : s.restored ? 'welcome' : 'ready',
        waitingForPin: false,
      };
    case 'unlockFailed':
      return { ...s, unlocking: false, screen: 'pin', problem: a.problem };
    case 'continued':
      return { ...s, restored: false, screen: 'sending', done: null };
    case 'startedOver':
      return { ...s, restored: false, screen: 'ready' };
    case 'filesAdded':
      return s.screen === 'pin' || s.screen === 'welcome' ? s : { ...s, screen: 'sending', done: null };
    case 'allDone':
      return s.waitingForPin ? s : { ...s, screen: 'done', restored: false, done: { files: a.files, bytes: a.bytes } };
    case 'sessionEnded':
      return {
        ...s,
        screen: 'pin',
        session: null,
        waitingForPin: s.screen === 'sending' || s.waitingForPin,
        problem: { kind: a.lost ? 'sessionLost' : 'sessionEnded' },
      };
    case 'sendMore':
      return { ...s, screen: 'ready', done: null };
  }
}
