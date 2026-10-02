// Files shared into Share from other apps reach the pages through the service worker's inbox
// (inbox.ts). Each share is sent by one page: the first to claim it holds a Web Lock for it
// while it is open, so other tabs leave it alone, and one whose page closed goes to the next
// page that opens. A file leaves the inbox once it is sent.
import { clearShared, inbox, removeShared, sortShared, type Shared } from './inbox';

export type { Shared };

const params = new URLSearchParams(location.search);

/**
 * The share reached the server instead of the service worker (there was none yet): the files
 * didn't come along, and the page offers to pick them instead. The address loses the mark.
 */
export const shareFailed = params.get('share') === 'failed';
if (params.has('share')) {
  params.delete('share');
  const rest = params.toString();
  history.replaceState(history.state, '', location.pathname + (rest ? `?${rest}` : '') + location.hash);
}

/** The shared files no other open page is sending, claimed by this one. */
export async function claimShared(): Promise<Shared[]> {
  let all: Shared[];
  try {
    all = await inbox();
  } catch {
    return [];
  }
  const { fresh, old } = sortShared(all, Date.now());
  void removeShared(old.map((s) => s.key)).catch(() => {});
  const mine: Shared[] = [];
  for (const batch of new Set(fresh.map((s) => s.batch))) {
    if (await claim(batch)) mine.push(...fresh.filter((s) => s.batch === batch));
  }
  return mine;
}

/** Takes the share's lock, if no other page has it, and keeps it while this page is open. */
function claim(batch: string): Promise<boolean> {
  const locks = navigator.locks;
  if (!locks) return Promise.resolve(true);
  return new Promise((resolve) => {
    locks
      .request(`share-inbox:${batch}`, { ifAvailable: true }, (lock) => {
        resolve(lock !== null);
        return lock ? new Promise<void>(() => {}) : undefined;
      })
      .catch(() => resolve(true));
  });
}

/** A shared file was sent, or given up: it leaves the inbox. */
export function sharedGone(key: number): void {
  void removeShared([key]).catch(() => {});
}

/** "Don't send" for some files; without keys, all of them, as when signing out. */
export async function dropShared(keys?: number[]): Promise<void> {
  await (keys ? removeShared(keys) : clearShared()).catch(() => {});
}
