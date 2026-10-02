// The pages of people with an account, in a chunk of their own (see root.tsx): the library,
// sending without a PIN, the settings, and the sign-in.
import './account.css';
import { useCallback, useEffect, useMemo, useState } from 'preact/hooks';
import { getInfo, getMe, onSignedOut, type Info, type Me } from '../api';
import { Icon } from '../components/Icon';
import { Page } from '../components/Page';
import { setSignedInHint } from '../hint';
import {
  addDictionaries,
  clearStoredLanguage,
  I18nContext,
  isLang,
  languages,
  makeI18n,
  pickLanguage,
  storedLanguage,
  storeLanguage,
  type Lang,
} from '../i18n';
import type { AccountProps } from '../root';
import { navigate, useRoute } from '../router';
import { ToastView } from './components/Bits';
import { AccountContext, type Account as AccountState, type Toast } from './context';
import de from './i18n/de.json';
import en from './i18n/en.json';
import it from './i18n/it.json';
import { Library } from './library/Library';
import { SendTab } from './send/SendTab';
import { Settings } from './settings/Settings';
import { SignIn } from './SignIn';

addDictionaries({ en, de, it });

export function Account({ me: first, notice }: AccountProps) {
  const route = useRoute();
  const [info, setInfo] = useState<Info | null>(null);
  const [me, setMe] = useState<Me | null>(first);
  const [stored, setStored] = useState(storedLanguage);
  const [lang, setLang] = useState<Lang>(() => pickLanguage(languages, stored, navigator.languages, 'en'));
  const [toast, setToast] = useState<Toast | null>(null);

  useEffect(() => {
    getInfo()
      .then((i) => {
        setInfo(i);
        setLang(pickLanguage(i.languages, storedLanguage(), navigator.languages, i.default_language));
      })
      .catch(() => {});
    // Signed out elsewhere (an admin, another tab): the next answer says so.
    onSignedOut(() => {
      setSignedInHint(false);
      location.replace('/sign-in?signed_out=1&next=' + encodeURIComponent(location.pathname));
    });
    return () => onSignedOut(null);
  }, []);

  useEffect(() => {
    document.documentElement.lang = lang;
  }, [lang]);

  useEffect(() => {
    if (!toast) return;
    const id = setTimeout(() => setToast(null), toast.action ? 10_000 : 6_000);
    return () => clearTimeout(id);
  }, [toast]);

  const offered = info ? info.languages.filter(isLang) : [...languages];
  const i18n = useMemo(
    () =>
      makeI18n(lang, offered, (l) => {
        storeLanguage(l);
        setStored(l);
        setLang(l);
      }),
    [lang, info],
  );

  // Something said once, on the way in: a PIN link isn't needed when signed in.
  useEffect(() => {
    if (notice === 'noPinNeeded') setToast({ text: i18n.t('send.noPinNeeded') });
  }, []);

  const refreshMe = useCallback(async () => setMe(await getMe()), []);
  const chooseLanguage = useCallback(
    (l: Lang | 'auto') => {
      if (l === 'auto') {
        clearStoredLanguage();
        setStored(null);
        setLang(pickLanguage(offered, null, navigator.languages, info?.default_language ?? 'en'));
      } else {
        i18n.setLang(l);
      }
    },
    [i18n, info],
  );

  const name = info?.name ?? 'Share';
  const known = /^\/(sign-in|library|send|settings)(\/|$)/.test(route.path);
  useEffect(() => {
    if (!known) navigate('/library', { replace: true });
  }, [known]);
  let page;
  if (route.path === '/sign-in') {
    page = <SignIn name={name} signedOut={notice === 'signedOut' || new URLSearchParams(route.search).has('signed_out')} />;
  } else if (!me) {
    // Signed in last time, but the server can't be reached now.
    page = (
      <Page name={name}>
        <div class="roundico">
          <Icon name="wifi" />
        </div>
        <h1 class="hero md">{i18n.t('common.offlineTitle')}</h1>
        <p class="lead">{i18n.t('common.offline')}</p>
        <div class="grow" />
        <button type="button" class="btn primary" onClick={() => location.reload()}>
          {i18n.t('common.retry')}
        </button>
      </Page>
    );
  } else if (route.path === '/library' || route.path.startsWith('/library/')) {
    page = <Library />;
  } else if (route.path === '/send' || route.path.startsWith('/send/')) {
    page = <SendTab />;
  } else if (route.path === '/settings' || route.path.startsWith('/settings/')) {
    page = <Settings />;
  } else {
    page = null; // no page of ours: the library (see the effect below)
  }

  const state: AccountState | null = me
    ? { info, me, refreshMe, toast: setToast, languageAuto: stored === null || !isLang(stored), chooseLanguage }
    : null;
  return (
    <I18nContext.Provider value={i18n}>
      <AccountContext.Provider value={state}>
        {page}
        {toast && <ToastView text={toast.text} action={toast.action} onDone={() => setToast(null)} />}
      </AccountContext.Provider>
    </I18nContext.Provider>
  );
}
