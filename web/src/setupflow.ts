// The setup page's decisions (screens/SetupPage.tsx): the secret of its link, and what the
// server's answers mean for it.
import { ApiError, type SetupStatus } from './api';

export type SetupState =
  | { kind: 'loading' }
  | { kind: 'folder'; status: SetupStatus }
  | { kind: 'starting' }
  | { kind: 'slow' }
  | { kind: 'admin' }
  | { kind: 'done' }
  | { kind: 'closed' }
  | { kind: 'failed'; reason: string | null };

/** Where an answer leads: the folder to set up, the first admin to make, or nothing to do. */
export function setupStep(st: SetupStatus): SetupState {
  if (!st.ready) return { kind: 'folder', status: st };
  return { kind: st.needs_admin ? 'admin' : 'done' };
}

/** The secret from the link in the server's log: <server>/setup#… */
export function secretFromHash(hash: string): string | null {
  const s = hash.replace(/^#/, '');
  return /^[A-Za-z0-9_-]{20,}$/.test(s) ? s : null;
}

/** What a failed request means for the page. A failed one: the server's words, or none when
 * it can't be reached. */
export function setupProblem(e: unknown): SetupState {
  if (e instanceof ApiError && e.code === 'setup_closed') return { kind: 'closed' };
  if (e instanceof ApiError && e.code === 'already_set_up') return { kind: 'done' };
  return { kind: 'failed', reason: e instanceof ApiError && e.status > 0 ? e.message : null };
}

/** Whether the server may just be changing hands after the setup: nothing answers, or a proxy in
 * front says the server is away. */
export function stillStarting(e: unknown): boolean {
  return !(e instanceof ApiError) || e.status === 0 || e.status >= 500;
}
