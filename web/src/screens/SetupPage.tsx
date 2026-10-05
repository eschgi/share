import { useEffect, useMemo, useState } from 'preact/hooks';
import '../account/account.css';
import de from '../account/i18n/de.json';
import en from '../account/i18n/en.json';
import it from '../account/i18n/it.json';
import { warningKey } from '../account/settings/storage';
import { suggestedUsername, validUsername } from '../account/settings/username';
import { ApiError, createFirstAdmin, getMe, getSetup, startSetup, type SetupStatus } from '../api';
import { thisBrowser } from '../browser';
import { DropZone } from '../components/DropZone';
import { Icon } from '../components/Icon';
import { Page } from '../components/Page';
import { formatBytes } from '../format';
import { setSignedInHint } from '../hint';
import { addDictionaries, I18nContext, isLang, languages, makeI18n, pickLanguage, storeLanguage, storedLanguage, type Lang } from '../i18n';
import { secretFromHash, setupProblem, setupStep, stillStarting, type SetupState } from '../setupflow';

// The storage warnings' texts are the account's, and so are the page's own: it is the first
// admin's way in.
addDictionaries({ en, de, it });

/** How long the page waits for the server to start after the setup, before it says so. */
const startWait = 120_000;

const secretKey = 'share.setup';

/** The secret, from the link in the log or, after a reload, from this tab: the address bar no
 * longer shows it. Without one, only visitors at home, or anyone right after a start, get in. */
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
 * Setting Share up while nobody has an account: the storage folder on a drive, with what it
 * and its drive look like, then the wait for the server, then the first admin's account.
 */
export function SetupPage() {
  const secret = useMemo(takeSecret, []);
  const [status, setStatus] = useState<SetupStatus | null>(null);
  const [lang, setLang] = useState<Lang>(() => pickLanguage(languages, storedLanguage(), navigator.languages, 'en'));
  const [state, setState] = useState<SetupState>({ kind: 'loading' });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [who, setWho] = useState('');
  const [username, setUsername] = useState('');
  const [ownUsername, setOwnUsername] = useState(false);
  const [password, setPassword] = useState('');
  const [show, setShow] = useState(false);

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
    const s = setupProblem(e);
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
      const st = await getSetup(secret);
      learn(st);
      setState(setupStep(st));
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
        const st = await getSetup(secret);
        if (st.ready) {
          learn(st);
          return setState(setupStep(st));
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
      await startSetup(secret);
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

  /** Makes the first admin, which signs this browser in, and opens the library. */
  async function createAdmin(e: Event) {
    e.preventDefault();
    if (busy) return;
    if (!validUsername(username)) return setProblem(t('password.usernameBad'));
    if ([...password].length < 8) return setProblem(t('password.tooShort'));
    setBusy(true);
    setProblem(null);
    try {
      await createFirstAdmin(secret, { name: who.trim(), username: username.trim(), password, device_name: thisBrowser() });
    } catch (err) {
      setBusy(false);
      if (!(err instanceof ApiError) || err.status === 0) return setProblem(t('common.offline'));
      if (err.code === 'bad_request') return setProblem(t('common.failed'));
      return setState(failed(err));
    }
    forgetSecret();
    // The answer carries the cookie, but a browser that blocks cookies drops it silently.
    try {
      await getMe();
    } catch {
      setBusy(false);
      return setProblem(t('setup.noCookie'));
    }
    setSignedInHint(true);
    location.replace('/library');
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

  const problemLine = problem && (
    <p class="help err" role="alert">
      <Icon name="alert" />
      {problem}
    </p>
  );

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
        {problemLine}
        <button type="button" class="btn primary" disabled={busy} onClick={() => void setUp()}>
          {t('setup.button')}
        </button>
        <button type="button" class="small link" disabled={busy} onClick={() => void load()}>
          {t('setup.checkAgain')}
        </button>
      </>
    );
  } else if (state.kind === 'admin') {
    body = (
      <>
        <div class="roundico">
          <Icon name="crown" />
        </div>
        <h1 class="hero md">{t('setup.adminTitle')}</h1>
        <p class="lead">{t('setup.adminLead', { name })}</p>
        <form class="form" method="post" onSubmit={(e) => void createAdmin(e)}>
          <label class="label" for="setup-name">
            {t('setup.yourName')}
          </label>
          <input
            id="setup-name"
            class="input"
            autocomplete="name"
            value={who}
            onInput={(e) => {
              setWho(e.currentTarget.value);
              if (!ownUsername) setUsername(suggestedUsername(e.currentTarget.value));
            }}
            required
          />
          <label class="label" for="setup-user">
            {t('signIn.username')}
          </label>
          <input
            id="setup-user"
            class="input"
            autocomplete="username"
            autoCapitalize="none"
            autoCorrect="off"
            spellcheck={false}
            value={username}
            onInput={(e) => {
              setUsername(e.currentTarget.value);
              setOwnUsername(true);
            }}
            required
          />
          <label class="label" for="setup-pass">
            {t('signIn.password')}
          </label>
          <span class="input-wrap">
            <input
              id="setup-pass"
              class="input"
              type={show ? 'text' : 'password'}
              autocomplete="new-password"
              value={password}
              onInput={(e) => setPassword(e.currentTarget.value)}
              required
            />
            <button type="button" class="ib" aria-label={t(show ? 'signIn.hidePassword' : 'signIn.showPassword')} onClick={() => setShow(!show)}>
              <Icon name={show ? 'eye-off' : 'eye'} />
            </button>
          </span>
          <p class="help">{t('password.tooShort')}</p>
          {problemLine}
          <button type="submit" class="btn primary" disabled={busy || !who.trim() || !username.trim() || !password}>
            {t('setup.create')}
          </button>
        </form>
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
    const [icon, title, lead] =
      state.kind === 'slow'
        ? (['alert', t('setup.slowTitle', { name }), t('setup.slow')] as const)
        : state.kind === 'closed'
          ? (['lock', t('setup.closedTitle', { name }), t('setup.closed', { name })] as const)
          : state.reason
            ? (['alert', t('setup.failedTitle'), t('setup.answered', { reason: state.reason })] as const)
            : (['alert', t('common.offlineTitle'), t('pin.network')] as const);
    body = (
      <>
        <div class="roundico">
          <Icon name={icon} />
        </div>
        <h1 class="hero md">{title}</h1>
        <p class="lead">{lead}</p>
        <div class="grow" />
        <button type="button" class="btn primary" onClick={() => void (state.kind === 'slow' ? waitForShare() : load())}>
          {t(state.kind === 'failed' ? 'common.retry' : 'setup.checkAgain')}
        </button>
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
