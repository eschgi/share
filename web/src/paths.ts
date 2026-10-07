// The website's pages and where each one goes for whom. The server answers all of them with
// this one page (contract/web_routes.json); this decides what it shows. Pure, for the tests.

/** Where Android's share sheet posts files, in the field files (the manifest's share_target). */
export const shareTarget = { path: '/share-target', field: 'files' };

/** The pages of people with an account; each may have up to three more segments. */
const accountPages = ['/library', '/send', '/settings'];
const segment = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;

/** Whether the server answers path with this page (see contract/web_routes.json). */
export function isAppPath(path: string): boolean {
  if (path === '/' || path === '/sign-in') return true;
  for (const page of accountPages) {
    if (path === page) return true;
    if (!path.startsWith(page + '/')) continue;
    const parts = path.slice(page.length + 1).split('/');
    return parts.length <= 3 && parts.every((p) => segment.test(p));
  }
  return false;
}

/** What a path shows: the PIN pages, the sign-in, or the pages of people with an account; or
 * the setup page, which is a page of its own. */
export type View = 'pin' | 'signIn' | 'account' | 'setup';

export interface Decision {
  view: View;
  /** Where to go instead, replacing the address. */
  redirect?: string;
}

/**
 * Where a visit goes. Signed in, the start is the library, and a PIN link goes to sending,
 * which needs no PIN then. Signed out, the account's pages ask to sign in first and come back
 * afterwards. While nobody has an account (setup), Share is set up first.
 */
export function decide(path: string, signedIn: boolean, pinLink: boolean, setup = false): Decision {
  if (signedIn) {
    if (path === '/') return { view: 'account', redirect: pinLink ? '/send' : '/library' };
    if (path === '/sign-in') return { view: 'account', redirect: '/library' };
    return { view: 'account' };
  }
  // Nobody has an account yet: Share is to be set up, unless a PIN from the console came along.
  if (setup && !pinLink) return { view: 'setup' };
  if (path === '/') return { view: 'pin' };
  if (path === '/sign-in') return { view: 'signIn' };
  if (path === '/send' || path.startsWith('/send/')) return { view: 'pin', redirect: '/' };
  return { view: 'signIn', redirect: '/sign-in?next=' + encodeURIComponent(path) };
}

/** Where to go after signing in: the page asked for, if it is one of ours, else the library. */
export function nextAfterSignIn(search: string): string {
  const next = new URLSearchParams(search).get('next');
  return next && next.startsWith('/') && isAppPath(next) && next !== '/sign-in' && next !== '/' ? next : '/library';
}
