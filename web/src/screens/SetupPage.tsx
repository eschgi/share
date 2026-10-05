import { useEffect, useMemo, useState } from 'preact/hooks';
import de from '../account/i18n/de.json';
import en from '../account/i18n/en.json';
import it from '../account/i18n/it.json';
import { warningKey } from '../account/settings/storage';
import { ApiError, getSetup, setupInvite, startSetup, type SetupStatus } from '../api';
import { DropZone } from '../components/DropZone';
import { Icon } from '../components/Icon';
import { Page } from '../components/Page';
import { formatBytes } from '../format';
import { addDictionaries, I18nContext, isLang, languages, makeI18n, pickLanguage, storeLanguage, storedLanguage, type Lang } from '../i18n';
import { secretFromHash, setupProblem, stillStarting, type SetupState } from '../setupflow';

// The storage warnings' texts are the account's, and so are the page's own: it is the first
// admin's way in.
addDictionaries({ en, de, it });

/** How long the page waits for the server to start after the setup, before it says so. */
const startWait = 120_000;

const secretKey = 'share.setup';

/** The secret, from the link or, after a reload, from this tab: the address bar no longer
 * shows it. */
function takeSecret(): string | null {
  const fromLink = secretFromHash(location.hash);
  try {
    if (fromLink) sessionStorage.setItem(secretKey, fromLink);
    return fromLink ?? sessionStorage.getItem(secretKey);
  } catch {
    return fromLink;
  }
}

function forgetSecret(): void {
  try {
    sessionStorage.removeItem(secretKey);
  } catch {
    // nothing was kept
  }
}

/**
 * Setting up the storage folder, from the link a new server logs instead of starting: what the
 * folder and its drive look like, then the setup, the wait for the server, and on to the first
 * admin's invite.
 */
export function SetupPage() {
  const secret = useMemo(takeSecret, []);
  const [status, setStatus] = useState<SetupStatus | null>(null);
  const [lang, setLang] = useState<Lang>(() => pickLanguage(languages, storedLanguage(), navigator.languages, 'en'));
  const [state, setState] = useState<SetupState>({ kind: 'loading' });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);

  const i18n = useMemo(
    () =>
      makeI18n(lang, status ? status.languages.filter(isLang) : [...languages], (l) => {
        storeLanguage(l);
        setLang(l);
      }),
    [lang, status],
  );
  const { t } = i18n;
  const name = status?.name || 'Share';

  /** What a failed request means for the page; the secret is of no more use but to try again. */
  function failed(e: unknown): SetupState {
    const s = setupProblem(e, !!secret);
    if (s.kind !== 'failed') forgetSecret();
    return s;
  }

  function learn(st: SetupStatus) {
    setStatus(st);
    setLang(pickLanguage(st.languages, storedLanguage(), navigator.languages, st.default_language));
  }

  async function load() {
    setState({ kind: 'loading' });
    setProblem(null);
    try {
      const st = await getSetup(secret ?? '');
      learn(st);
      if (st.ready) return void (await join(st));
      setState({ kind: 'folder', status: st });
    } catch (e) {
      setState(failed(e));
    }
  }

  /** Share runs: on to the first admin's invite, if nobody has an account yet. */
  async function join(st: SetupStatus) {
    if (!st.needs_admin) {
      forgetSecret();
      return setState({ kind: 'done' });
    }
    try {
      const { invite } = await setupInvite(secret ?? '');
      forgetSecret();
      location.replace(`/join#${invite}`);
    } catch (e) {
      setState(failed(e));
    }
  }

  /** After the setup the server starts for real, on the same port; meanwhile nothing answers. */
  async function waitForShare() {
    setState({ kind: 'starting' });
    const until = Date.now() + startWait;
    for (;;) {
      try {
        const st = await getSetup(secret ?? '');
        if (st.ready) {
          learn(st);
          return void (await join(st));
        }
      } catch (e) {
        if (!stillStarting(e)) return setState(failed(e));
      }
      if (Date.now() > until) return setState({ kind: 'slow' });
      await new Promise((r) => setTimeout(r, 1000));
    }
  }

  async function setUp() {
    if (busy) return;
    setBusy(true);
    setProblem(null);
    try {
      await startSetup(secret ?? '');
    } catch (e) {
      // 404 or 405: set up meanwhile, by share init, and the server runs already.
      if (!(e instanceof ApiError && (e.status === 404 || e.status === 405))) {
        setBusy(false);
        if (e instanceof ApiError && e.code === 'setup_failed') return setProblem(t('setup.failed', { reason: e.message }));
        if (e instanceof ApiError && e.status === 0) return setProblem(t('pin.network'));
        return setState(failed(e));
      }
    }
    setBusy(false);
    await waitForShare();
  }

  useEffect(() => {
    if (location.hash) history.replaceState(null, '', location.pathname + location.search);
    // Opening another setup link in this tab only changes the fragment: start over with it.
    addEventListener('hashchange', () => location.reload());
    void load();
  }, []);

  useEffect(() => {
    document.documentElement.lang = lang;
  }, [lang]);

  let body;
  if (state.kind === 'loading') {
    body = <p class="lead">{t('common.loading')}</p>;
  } else if (state.kind === 'folder') {
    const st = state.status;
    const drive = [st.fs_type, st.total_bytes > 0 && t('storage.free', { free: formatBytes(st.free_bytes, lang), total: formatBytes(st.total_bytes, lang) })];
    body = (
      <>
        <div class="roundico">
          <Icon name="hdd" />
        </div>
        <h1 class="hero md">{t('setup.title', { name })}</h1>
        <p class="lead">{t('setup.lead', { name })}</p>
        <div class="facts">
          <div>
            <Icon name="folder" />
            <em>{t('setup.folder')}</em>
            <b class="path">{st.storage_dir}</b>
          </div>
          <div>
            <Icon name="hdd" />
            <em>{t('setup.drive')}</em>
            <b>{drive.filter(Boolean).join(' · ')}</b>
          </div>
          <div>
            <Icon name="info" />
            <em>{t('setup.now')}</em>
            <b>{t(!st.exists ? 'setup.missing' : st.empty ? 'setup.empty' : 'setup.notEmpty', { name })}</b>
          </div>
        </div>
        {st.warnings.map((w) => (
          <p key={w.code} class={`help ${w.level === 'problem' ? 'err' : 'warn'}`}>
            <Icon name="alert" />
            {t(warningKey(w.code, (key) => t(key) !== key))}
          </p>
        ))}
        {/* An unmounted drive leaves an empty folder, or none, on the system's disk. */}
        {(!st.exists || st.empty) && (
          <p class="help">
            <Icon name="info" />
            {t('setup.mounted')}
          </p>
        )}
        <div class="grow" />
        {problem && (
          <p class="help err" role="alert">
            <Icon name="alert" />
            {problem}
          </p>
        )}
        <button type="button" class="btn primary" disabled={busy} onClick={() => void setUp()}>
          {t('setup.button')}
        </button>
        <button type="button" class="small link" disabled={busy} onClick={() => void load()}>
          {t('setup.checkAgain')}
        </button>
      </>
    );
  } else if (state.kind === 'starting') {
    body = (
      <>
        <div class="roundico">
          <Icon name="check" />
        </div>
        <h1 class="hero md">{t('setup.startingTitle', { name })}</h1>
        <p class="lead" role="status">
          {t('setup.starting')}
        </p>
      </>
    );
  } else if (state.kind === 'done') {
    body = (
      <>
        <div class="roundico">
          <Icon name="check" />
        </div>
        <h1 class="hero md">{t('setup.doneTitle', { name })}</h1>
        <p class="lead">{t('setup.done')}</p>
        <div class="grow" />
        <a class="btn primary" href="/sign-in">
          {t('signIn.button')}
        </a>
      </>
    );
  } else {
    const [title, lead] =
      state.kind === 'slow'
        ? [t('setup.slowTitle', { name }), t('setup.slow')]
        : state.kind === 'link'
          ? [t('setup.linkTitle'), t(state.key)]
          : state.reason
            ? [t('setup.failedTitle'), t('setup.answered', { reason: state.reason })]
            : [t('common.offlineTitle'), t('pin.network')];
    body = (
      <>
        <div class="roundico">
          <Icon name="alert" />
        </div>
        <h1 class="hero md">{title}</h1>
        <p class="lead">{lead}</p>
        <div class="grow" />
        {state.kind !== 'link' && (
          <button type="button" class="btn primary" onClick={() => void (state.kind === 'slow' ? waitForShare() : load())}>
            {t(state.kind === 'slow' ? 'setup.checkAgain' : 'common.retry')}
          </button>
        )}
      </>
    );
  }

  return (
    <I18nContext.Provider value={i18n}>
      <Page name={name} languageSwitch>
        {body}
      </Page>
      {/* Takes no files, but keeps a dropped one from replacing the page. */}
      <DropZone onFiles={null} label="" />
    </I18nContext.Provider>
  );
}
