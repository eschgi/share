// Who is here decides what the page shows. People with an account get their pages, which come
// in a chunk of their own, so someone sending with a PIN never downloads them; everyone else
// gets the PIN's pages (app.tsx). Going from one to the other always loads the page anew.
import type { ComponentType } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import { ApiError, getInfo, getMe, type Me } from './api';
import { App } from './app';
import { setSignedInHint, signedInHint } from './hint';
import { decide } from './paths';
import { pinFromHash } from './pin';

export interface AccountProps {
  /** The person signed in here; null on the sign-in page, or while the server can't be reached. */
  me: Me | null;
  /** Something to say once: a PIN link isn't needed when signed in, or this browser was signed out. */
  notice: 'noPinNeeded' | 'signedOut' | null;
}

type AccountModule = { Account: ComponentType<AccountProps> };

let loading: Promise<AccountModule> | null = null;

/** The account's pages, loaded once. If a newer version replaced them on the server meanwhile,
 * the page loads anew, once. */
function loadAccount(): Promise<AccountModule> {
  loading ??= import('./account/Account').catch((e: unknown) => {
    try {
      if (!sessionStorage.getItem('share.reloaded')) {
        sessionStorage.setItem('share.reloaded', '1');
        location.reload();
      }
    } catch {
      // no storage: don't risk reloading forever
    }
    throw e;
  });
  return loading;
}

type Shown = { kind: 'pin' } | { kind: 'account'; Account: ComponentType<AccountProps>; props: AccountProps };

export function Root() {
  const [shown, setShown] = useState<Shown | null>(null);

  useEffect(() => {
    void (async () => {
      const pinLink = !!pinFromHash(location.hash);
      const hint = signedInHint();
      // Whichever pages show, they need the server's name and languages: ask meanwhile.
      getInfo().catch(() => {});
      if (hint) loadAccount().catch(() => {});
      let me: Me | null = null;
      let unreachable = false;
      let signedOut = false;
      try {
        me = await getMe();
      } catch (e) {
        // Without a connection a browser that was signed in stays so; the pages say so.
        unreachable = hint && (!(e instanceof ApiError) || e.status === 0 || e.status >= 500);
        signedOut = e instanceof ApiError && e.code === 'signed_out';
      }
      setSignedInHint(!!me || unreachable);
      const d = decide(location.pathname, !!me || unreachable, pinLink);
      if (d.redirect) history.replaceState(null, '', d.redirect);
      if (d.view === 'pin') {
        setShown({ kind: 'pin' });
        return;
      }
      try {
        const { Account } = await loadAccount();
        const notice = pinLink && me ? 'noPinNeeded' : signedOut ? 'signedOut' : null;
        setShown({ kind: 'account', Account, props: { me, notice } });
      } catch {
        setShown({ kind: 'pin' }); // the account's pages can't be loaded: sending with a PIN still works
      }
    })();
  }, []);

  if (!shown) return null;
  if (shown.kind === 'pin') return <App />;
  return <shown.Account {...shown.props} />;
}
