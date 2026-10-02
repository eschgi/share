import { useState } from 'preact/hooks';
import { ApiError, getMe, login } from '../api';
import { thisBrowser } from '../browser';
import { Stack } from '../components/Bits';
import { Icon } from '../components/Icon';
import { Page } from '../components/Page';
import { formatWait } from '../format';
import { setSignedInHint } from '../hint';
import { useI18n } from '../i18n';
import { nextAfterSignIn } from '../paths';
import { useRoute } from '../router';

/** Screen 22: signing in with the password set in the app. Most people come in with an invite. */
export function SignIn({ name, signedOut }: { name: string; signedOut: boolean }) {
  const { t, tn, lang } = useI18n();
  const route = useRoute();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [show, setShow] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(signedOut ? t('signIn.signedOut') : null);

  async function submit(e: Event) {
    e.preventDefault();
    if (busy || !username.trim() || !password) return;
    setBusy(true);
    setProblem(null);
    try {
      await login(username.trim(), password, thisBrowser());
    } catch (err) {
      setBusy(false);
      if (!(err instanceof ApiError)) return setProblem(t('common.offline'));
      switch (err.code) {
        case 'login_wrong':
          return setProblem(err.attemptsLeft == null ? t('signIn.wrong') : tn('signIn.wrongLeft', err.attemptsLeft));
        case 'login_locked':
          return setProblem(t('signIn.locked', { time: formatWait(err.retryAfterSeconds ?? 600, lang) }));
        default:
          return setProblem(t(err.status === 0 ? 'common.offline' : 'common.failed'));
      }
    }
    // The answer carries the cookie, but a browser that blocks cookies drops it silently.
    try {
      await getMe();
    } catch {
      setBusy(false);
      return setProblem(t('signIn.noCookie'));
    }
    setSignedInHint(true);
    location.replace(nextAfterSignIn(route.search));
  }

  return (
    <Page name={name} languageSwitch layout="split">
      <div class="pane">
        <div class="roundico narrow-only">
          <Icon name="user" />
        </div>
        <div class="wide-only">
          <Stack />
        </div>
        <h1 class="hero">{t('signIn.title')}</h1>
        <p class="lead">{t('signIn.lead')}</p>
        <p class="help wide-only">{t('signIn.noPassword')}</p>
      </div>
      <div class="pane">
        <form class="form" method="post" onSubmit={submit}>
          <label class="label" for="signin-user">
            {t('signIn.username')}
          </label>
          <input
            id="signin-user"
            class="input"
            name="username"
            autocomplete="username"
            autoCapitalize="none"
            autoCorrect="off"
            spellcheck={false}
            value={username}
            onInput={(e) => setUsername(e.currentTarget.value)}
            required
          />
          <label class="label" for="signin-pass">
            {t('signIn.password')}
          </label>
          <span class="input-wrap">
            <input
              id="signin-pass"
              class="input"
              name="password"
              type={show ? 'text' : 'password'}
              autocomplete="current-password"
              value={password}
              onInput={(e) => setPassword(e.currentTarget.value)}
              required
            />
            <button type="button" class="ib" aria-label={t(show ? 'signIn.hidePassword' : 'signIn.showPassword')} onClick={() => setShow(!show)}>
              <Icon name={show ? 'eye-off' : 'eye'} />
            </button>
          </span>
          {problem && (
            <p class="help err" role="alert">
              <Icon name="alert" />
              {problem}
            </p>
          )}
          <button type="submit" class="btn primary" disabled={busy || !username.trim() || !password}>
            {t('signIn.button')}
          </button>
        </form>
        <p class="small narrow-only">{t('signIn.noPassword')}</p>
        <a class="small link" href="/">
          {t('signIn.pinInstead')}
        </a>
      </div>
    </Page>
  );
}
