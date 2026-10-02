// The settings' dialogs that everyone has: their phones and browsers, language, theme and
// password. Each is rendered only while open.
import { useEffect, useState } from 'preact/hooks';
import { ApiError, getAbout, getMyDevices, setPassword, signOutDevice, type ListedDevice } from '../../api';
import { Icon } from '../../components/Icon';
import { daysAgo, formatWait } from '../../format';
import { pickLanguage, useI18n, type I18n, type Lang } from '../../i18n';
import { applyTheme, darkThemes, lightThemes, resolveTheme, storeTheme, type Theme, type ThemeChoice } from '../../theme';
import { Confirm, Modal } from '../components/Modal';
import { useAccount } from '../context';
import { suggestedUsername, validUsername } from './username';

/** "Last used today", "… yesterday", "… 3 days ago". */
export function lastUsed(i18n: I18n, when: string): string {
  const days = daysAgo(new Date(when), new Date());
  if (days === 0) return i18n.t('devices.lastUsedToday');
  if (days === 1) return i18n.t('devices.lastUsedYesterday');
  return i18n.tn('devices.lastUsedDays', days);
}

/** The phones and browsers signed in as oneself, so a forgotten one can be signed out. */
export function DevicesDialog({ onClose }: { onClose: () => void }) {
  const i18n = useI18n();
  const { t } = i18n;
  const [list, setList] = useState<ListedDevice[] | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [asking, setAsking] = useState<ListedDevice | null>(null);
  const [busy, setBusy] = useState(false);

  const load = () =>
    getMyDevices()
      .then((r) => setList(r.devices))
      .catch((e: unknown) => setProblem(t(e instanceof ApiError && e.status > 0 ? 'common.failed' : 'common.offline')));
  useEffect(() => void load(), []);

  const signOut = async (d: ListedDevice) => {
    setBusy(true);
    try {
      await signOutDevice(d.id);
      setAsking(null);
      await load();
    } catch {
      setProblem(t('common.failed'));
      setAsking(null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal title={t('devices.title')} onClose={onClose}>
      <p class="modal-text">{t('devices.lead')}</p>
      {problem && (
        <p class="help err" role="alert">
          <Icon name="alert" />
          {problem}
        </p>
      )}
      {list && (
        <div class="group">
          {list.map((d) => (
            <div class="row" key={d.id}>
              <span class="ri">
                <Icon name={d.client === 'web' ? 'monitor' : 'smartphone'} />
              </span>
              <span class="rt">
                <b>{d.name}</b>
                <span>
                  {d.this ? t(d.client === 'web' ? 'devices.thisBrowser' : 'devices.thisPhone') : lastUsed(i18n, d.last_seen_at)}
                  {d.home_only ? ` · ${t('devices.atHome')}` : ''}
                </span>
              </span>
              {!d.this && (
                <button type="button" class="tbtn danger" onClick={() => setAsking(d)}>
                  {t('devices.signOut')}
                </button>
              )}
            </div>
          ))}
        </div>
      )}
      {asking && (
        <Confirm
          title={t('devices.signOutTitle', { name: asking.name })}
          body={t('devices.signOutBody')}
          confirm={t('devices.signOut')}
          danger
          busy={busy}
          onConfirm={() => void signOut(asking)}
          onClose={() => setAsking(null)}
        />
      )}
    </Modal>
  );
}

/** Where Share's source code is; the repository also has the license texts. */
const sourceUrl = 'https://github.com/eschgi/share';

/** What runs here: the server's version, the license and where the source code is. */
export function AboutDialog({ onClose }: { onClose: () => void }) {
  const { t } = useI18n();
  const [version, setVersion] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    getAbout().then(
      (a) => setVersion(a.version),
      () => setFailed(true),
    );
  }, []);
  const link = (href: string, icon: 'share' | 'file', title: string, sub: string) => (
    <a class="row" href={href} target="_blank" rel="noopener noreferrer">
      <span class="ri">
        <Icon name={icon} />
      </span>
      <span class="rt">
        <b>{title}</b>
        <span>{sub}</span>
      </span>
    </a>
  );
  return (
    <Modal title={t('about.title')} onClose={onClose}>
      <div class="group">
        <div class="row">
          <span class="ri">
            <Icon name="info" />
          </span>
          <span class="rt">
            <b>{t('about.version')}</b>
            <span class="mono">{version ?? (failed ? t('common.offline') : '…')}</span>
          </span>
        </div>
        {link(`${sourceUrl}/blob/main/LICENSE`, 'file', t('about.license'), 'Apache License 2.0')}
        {link(sourceUrl, 'share', t('about.source'), sourceUrl.replace('https://', ''))}
      </div>
      <p class="modal-text about-made">
        Copyright 2026 Stefan Eschgfäller. {t('about.madeWith')}
      </p>
    </Modal>
  );
}

/** Automatic, or one of the languages the server offers. */
export function LanguageDialog({ onClose }: { onClose: () => void }) {
  const { t, lang, offered } = useI18n();
  const { languageAuto, chooseLanguage } = useAccount();
  const browser = pickLanguage(offered, null, navigator.languages, offered[0] ?? 'en');
  const choose = (l: Lang | 'auto') => {
    chooseLanguage(l);
    onClose();
  };
  return (
    <Modal title={t('settings.language')} onClose={onClose}>
      <div class="choices" role="radiogroup" aria-label={t('settings.language')}>
        <button type="button" role="radio" aria-checked={languageAuto} class={`choice${languageAuto ? ' on' : ''}`} onClick={() => choose('auto')}>
          <i class="radio" />
          <span>
            <b>{t('language.auto')}</b>
            <span>{t(`lang.${browser}`)}</span>
          </span>
        </button>
        {offered.map((l) => {
          const on = !languageAuto && l === lang;
          return (
            <button key={l} type="button" role="radio" aria-checked={on} lang={l} class={`choice${on ? ' on' : ''}`} onClick={() => choose(l)}>
              <i class="radio" />
              <span>
                <b>{t(`lang.${l}`)}</b>
              </span>
            </button>
          );
        })}
      </div>
    </Modal>
  );
}

/** The library in miniature, in a theme's colours, as in the app's theme picker. */
function Preview({ theme }: { theme: Theme }) {
  return (
    <span class="tprev" data-theme={theme} aria-hidden="true">
      <i class="tl" />
      <i class="tc" />
      <i class="tt">
        <b class="t1" />
        <b class="t3" />
        <b class="t5" />
      </i>
      <i class="tb" />
      <i class="tn">
        <b />
        <b />
        <b />
      </i>
    </span>
  );
}

/** The app's seven themes and Automatic. A choice applies at once and stays in this browser. */
export function ThemeDialog({ choice, onChoose, onClose }: { choice: ThemeChoice; onChoose: (c: ThemeChoice) => void; onClose: () => void }) {
  const { t } = useI18n();
  const choose = (c: ThemeChoice) => {
    storeTheme(c);
    applyTheme(c);
    onChoose(c);
  };
  const tile = (theme: Theme) => (
    <button
      key={theme}
      type="button"
      role="radio"
      aria-checked={choice === theme}
      class={`tch${choice === theme ? ' on' : ''}`}
      onClick={() => choose(theme)}
    >
      <Preview theme={theme} />
      {choice === theme && (
        <span class="tck">
          <Icon name="check" />
        </span>
      )}
      <span class="tname">{t(`theme.${theme}`)}</span>
    </button>
  );
  return (
    <Modal title={t('settings.theme')} onClose={onClose} wide>
      <div role="radiogroup" aria-label={t('settings.theme')}>
        <button type="button" role="radio" aria-checked={choice === 'auto'} class={`tauto${choice === 'auto' ? ' on' : ''}`} onClick={() => choose('auto')}>
          <span class="tsplit">
            <Preview theme="linen" />
            <Preview theme="ember" />
          </span>
          <span>
            <b>{t('theme.auto')}</b>
            <span>{t('theme.autoDetail')}</span>
          </span>
        </button>
        <p class="glabel">{t('theme.dark')}</p>
        <div class="tgrid">{darkThemes.map(tile)}</div>
        <p class="glabel">{t('theme.light')}</p>
        <div class="tgrid">{lightThemes.map(tile)}</div>
      </div>
    </Modal>
  );
}

/** How the theme row describes the choice: "Ember", or "Automatic · Linen". */
export function themeSummary(t: I18n['t'], choice: ThemeChoice): string {
  if (choice !== 'auto') return t(`theme.${choice}`);
  const now = resolveTheme('auto', matchMedia('(prefers-color-scheme: light)').matches);
  return t('theme.autoNow', { name: t(`theme.${now}`) });
}

/** Sets a first password, or changes it: for signing in on other phones and in browsers. */
export function PasswordDialog({ onClose }: { onClose: () => void }) {
  const { t, tn, lang } = useI18n();
  const { me, refreshMe, toast } = useAccount();
  const has = me.user.has_password;
  const [username, setUsername] = useState(me.user.username ?? suggestedUsername(me.user.name));
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);

  async function save(e: Event) {
    e.preventDefault();
    if (busy) return;
    if (!validUsername(username)) return setProblem(t('password.usernameBad'));
    if ([...next].length < 8) return setProblem(t('password.tooShort'));
    setBusy(true);
    setProblem(null);
    try {
      await setPassword(username.trim(), next, has ? current : undefined);
    } catch (err) {
      setBusy(false);
      if (!(err instanceof ApiError) || err.status === 0) return setProblem(t('common.offline'));
      switch (err.code) {
        case 'password_wrong':
          return setProblem(err.attemptsLeft == null ? t('password.wrong') : tn('password.wrongLeft', err.attemptsLeft));
        case 'login_locked':
          return setProblem(t('password.locked', { time: formatWait(err.retryAfterSeconds ?? 900, lang) }));
        case 'username_taken':
          return setProblem(t('password.usernameTaken'));
        case 'bad_request':
          return setProblem(t('password.usernameBad'));
        default:
          return setProblem(t('common.failed'));
      }
    }
    await refreshMe().catch(() => {});
    toast({ text: t('password.saved') });
    onClose();
  }

  return (
    <Modal title={t('settings.password')} onClose={onClose}>
      <form onSubmit={save}>
        <p class="modal-text">{t('password.lead')}</p>
        <label class="label" for="pw-user">
          {t('signIn.username')}
        </label>
        <input
          id="pw-user"
          class="input"
          autocomplete="username"
          autoCapitalize="none"
          spellcheck={false}
          value={username}
          onInput={(e) => setUsername(e.currentTarget.value)}
        />
        {has && (
          <>
            <label class="label" for="pw-current">
              {t('password.current')}
            </label>
            <input
              id="pw-current"
              class="input"
              type="password"
              autocomplete="current-password"
              value={current}
              onInput={(e) => setCurrent(e.currentTarget.value)}
            />
          </>
        )}
        <label class="label" for="pw-new">
          {t('password.new')}
        </label>
        <input id="pw-new" class="input" type="password" autocomplete="new-password" value={next} onInput={(e) => setNext(e.currentTarget.value)} />
        {problem && (
          <p class="help err" role="alert">
            <Icon name="alert" />
            {problem}
          </p>
        )}
        <div class="dbtns">
          <button type="button" class="tbtn" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button type="submit" class="btn sm primary" disabled={busy || !username.trim() || !next || (has && !current)}>
            {t('common.save')}
          </button>
        </div>
      </form>
    </Modal>
  );
}
