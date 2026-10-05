// The setup page's decisions (screens/SetupPage.tsx): the secret of its link, and what the
// server's answers mean for it.
import { ApiError, type SetupStatus } from './api';

export type SetupState =
  | { kind: 'loading' }
  | { kind: 'folder'; status: SetupStatus }
  | { kind: 'starting' }
  | { kind: 'slow' }
  | { kind: 'done' }
  | { kind: 'link'; key: string }
  | { kind: 'failed'; reason: string | null };

/** The secret from the link in the server's log: <server>/setup#… */
export function secretFromHash(hash: string): string | null {
  const s = hash.replace(/^#/, '');
  return /^[A-Za-z0-9_-]{20,}$/.test(s) ? s : null;
}

/** What a failed request means for the page; hadSecret: whether it had a secret to send. A
 * failed one: the server's words, or none when it can't be reached. */
export function setupProblem(e: unknown, hadSecret: boolean): SetupState {
  if (e instanceof ApiError && e.code === 'forbidden') return { kind: 'link', key: hadSecret ? 'setup.earlier' : 'setup.noSecret' };
  // A server that didn't set anything up (the folder was ready already), or one that has an
  // admin already.
  if (e instanceof ApiError && (e.status === 404 || e.code === 'already_set_up')) return { kind: 'done' };
  return { kind: 'failed', reason: e instanceof ApiError && e.status > 0 ? e.message : null };
}

/** Whether the server may just be changing hands after the setup: nothing answers, or a proxy in
 * front says the server is away. */
export function stillStarting(e: unknown): boolean {
  return !(e instanceof ApiError) || e.status === 0 || e.status >= 500;
}
