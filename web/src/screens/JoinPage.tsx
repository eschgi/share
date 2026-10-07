import { useEffect, useMemo, useState } from 'preact/hooks';
import { acceptInvite, ApiError, getApp, getInfo, getMe, peekInvite, type AppInfo, type Info, type InvitePeek } from '../api';
import { isApple, thisBrowser } from '../browser';
import { DropZone } from '../components/DropZone';
import { Icon } from '../components/Icon';
import { Page } from '../components/Page';
import { QrCode } from '../components/QrCode';
import { formatBytes, formatWhen } from '../format';
import { setSignedInHint } from '../hint';
import { I18nContext, isLang, languages, makeI18n, pickLanguage, storeLanguage, storedLanguage, type Lang } from '../i18n';
import { keysFromInvite } from '../e2ee/keyring';
import { intentLink, secretFromHash, tokenFromHash } from '../joinlink';

type State =
  | { kind: 'loading' }
  | { kind: 'ok'; peek: InvitePeek; app: AppInfo }
  | { kind: 'problem'; key: string };

const problems: Record<string, string> = {
  invite_used: 'join.used',
  invite_expired: 'join.expired',
  invite_revoked: 'join.revoked',
  invite_unknown: 'join.unknown',
  invite_locked: 'join.locked',
};

/**
 * Screens 9 and 23: what an invite link opens when the app isn't installed yet. On Android it
 * leads to the app, on a computer to the app on an Android phone, by QR code; and everywhere it
 * can sign this browser in instead, which on iPhones and iPads is the way in.
 */
export function JoinPage() {
  // Read the token before it leaves the address bar, so it doesn't stay in the history.
  const token = useMemo(() => tokenFromHash(location.hash), []);
  const secret = useMemo(() => secretFromHash(location.hash), []);
  const link = token ? `${location.origin}/join#${token}${secret ? '.' + secret : ''}` : '';
  const [info, setInfo] = useState<Info | null>(null);
  const [lang, setLang] = useState<Lang>(() => pickLanguage(languages, storedLanguage(), navigator.languages, 'en'));
  const [state, setState] = useState<State>({ kind: 'loading' });
  const [joining, setJoining] = useState(false);
  const [joinProblem, setJoinProblem] = useState<string | null>(null);

  useEffect(() => {
    if (location.hash) history.replaceState(null, '', location.pathname + location.search);
    // Opening another invite link in this tab only changes the fragment: start over with it.
    addEventListener('hashchange', () => location.reload());
    (async () => {
      try {
        const i = await getInfo();
        setInfo(i);
        setLang(pickLanguage(i.languages, storedLanguage(), navigator.languages, i.default_language));
        if (!token) return setState({ kind: 'problem', key: 'join.unknown' });
        const [peek, app] = await Promise.all([peekInvite(token), getApp()]);
        setState({ kind: 'ok', peek, app });
      } catch (e) {
        const key = e instanceof ApiError ? (problems[e.code] ?? 'pin.network') : 'pin.network';
        setState({ kind: 'problem', key });
      }
    })();
  }, []);

  useEffect(() => {
    document.documentElement.lang = lang;
  }, [lang]);

  const i18n = useMemo(
    () =>
      makeI18n(lang, info ? info.languages.filter(isLang) : [...languages], (l) => {
        storeLanguage(l);
        setLang(l);
      }),
    [lang, info],
  );
  const { t } = i18n;
  const name = info?.name ?? 'Share';

  /** Uses the invite for this browser: it signs in as the invited person and opens the library. */
  async function joinHere() {
    if (!token || joining) return;
    // The invite works once; without cookies it would be used up for nothing.
    if (!navigator.cookieEnabled) return setJoinProblem(t('join.noCookie'));
    setJoining(true);
    setJoinProblem(null);
    let joined;
    try {
      joined = await acceptInvite(token, thisBrowser());
    } catch (e) {
      setJoining(false);
      if (e instanceof ApiError && problems[e.code]) return setState({ kind: 'problem', key: problems[e.code] });
      return setJoinProblem(t(e instanceof ApiError && e.status > 0 ? 'join.failed' : 'pin.network'));
    }
    let me;
    try {
      me = await getMe();
    } catch {
      setJoining(false);
      return setJoinProblem(t('join.cookieLost'));
    }
    // The keys the link's secret unlocks become this browser's; without them it waits for others.
    await keysFromInvite(me, secret, joined.keys, joined.root).catch(() => {});
    setSignedInHint(true);
    location.replace('/library');
  }
  const problemLine = joinProblem && (
    <p class="help err" role="alert">
      <Icon name="alert" />
      {joinProblem}
    </p>
  );

  let body;
  let split = false;
  if (state.kind === 'loading') {
    body = <p class="lead">{t('common.loading')}</p>;
  } else if (state.kind === 'problem') {
    body = (
      <>
        <div class="roundico">
          <Icon name="alert" />
        </div>
        <h1 class="hero md">{t('join.problemTitle')}</h1>
        <p class="lead">{t(state.key)}</p>
      </>
    );
  } else if (isApple(navigator.userAgent, navigator.maxTouchPoints) || !state.app.apk) {
    // No app for this device: the browser is the way in (screen 23 on an iPhone).
    const { peek } = state;
    const until = formatWhen(new Date(peek.expires_at), new Date(), lang);
    body = (
      <>
        <div class="bigav" aria-hidden="true">
          {([...peek.name.trim()][0] ?? '?').toLocaleUpperCase()}
          <i>
            <Icon name="check" />
          </i>
        </div>
        <h1 class="hero md center">{peek.inviter ? t('join.invitedBy', { inviter: peek.inviter }) : t('join.invited')}</h1>
        <p class="lead center">{t('join.browserLead', { person: peek.name })}</p>
        <div class="facts">
          <div>
            <Icon name="user" />
            <em>{t('join.factName')}</em>
            <b>{peek.name}</b>
          </div>
          <div>
            <Icon name="shield" />
            <em>{t('join.factRole')}</em>
            <b>{t(peek.role === 'admin' ? 'join.roleAdmin' : 'join.roleMember')}</b>
          </div>
          <div>
            <Icon name="clock" />
            <em>{t('join.factValid')}</em>
            <b>{t('join.validUntil', { when: until })}</b>
          </div>
        </div>
        <div class="grow" />
        {problemLine}
        <button type="button" class="btn primary" disabled={joining} onClick={() => void joinHere()}>
          {t('join.useBrowser', { name })}
        </button>
        <p class="small">
          {peek.inviter ? t('join.notYouInviter', { person: peek.name, inviter: peek.inviter }) : t('join.notYou', { person: peek.name })}
        </p>
      </>
    );
  } else {
    const { peek, app } = state;
    const android = /Android/i.test(navigator.userAgent);
    const until = formatWhen(new Date(peek.expires_at), new Date(), lang);
    const size = app.apk ? formatBytes(app.apk.size, lang) : '';
    // On a computer the invite goes to the Android phone as a QR code: the phone's camera opens
    // it there, and the app can scan it as well. Or this browser takes it.
    const scan = !android && !!token;
    split = true;
    body = (
      <>
        <div class="pane">
          <div class="appico lg">
            <Icon name="images" />
          </div>
          <h1 class="hero md">{peek.inviter ? t('join.invitedBy', { inviter: peek.inviter }) : t('join.invited')}</h1>
          <p class="lead">{t(scan ? 'join.leadBoth' : 'join.lead', { name })}</p>
          {scan ? (
            <div class="steps">
              <Step n={1} title={t('join.scanStep')} detail={t('join.scanStepDetail')} />
              <Step n={2} title={t('join.installStep')} detail={t('join.installStepDetail')} />
              <Step n={3} title={t('join.joinStep')} detail={t('join.step3Detail', { when: until })} />
            </div>
          ) : (
            <div class="steps">
              <Step n={1} title={t('join.step1')} detail={t('join.step1Detail', { size })} />
              <Step n={2} title={t('join.step2')} detail={t('join.step2Detail')} />
              <Step n={3} title={t('join.step3')} detail={t('join.step3Detail', { when: until })} />
            </div>
          )}
        </div>
        <div class="grow" />
        <div class="pane">
          {android && (
            <>
              <a class="btn primary" href="/download/share.apk" download="share.apk">
                <Icon name="download" />
                {t('join.download')}
              </a>
              {token && (
                <a class="btn link" href={intentLink(app, location.origin, token, secret)}>
                  {t('join.already', { name: peek.name })}
                </a>
              )}
              {problemLine}
              <button type="button" class="small link" disabled={joining} onClick={() => void joinHere()}>
                {t('join.useBrowserLink', { name })}
              </button>
            </>
          )}
          {scan && (
            <>
              <div class="qrcard">
                <QrCode text={link} label={t('join.qrLabel')} />
                <b>{t('join.scan')}</b>
                <span class="exp">
                  <Icon name="clock" />
                  {t('join.scanUntil', { when: until })}
                </span>
              </div>
              <div class="or">{t('join.or')}</div>
              {problemLine}
              <button type="button" class="btn outline" disabled={joining} onClick={() => void joinHere()}>
                <Icon name="monitor" />
                {t('join.useBrowser', { name })}
              </button>
              <p class="small">{t('join.useBrowserNote', { person: peek.name })}</p>
              {/* Chrome shows Android tablets the desktop site, so they land here too. */}
              <a class="small link" href="/download/share.apk" download="share.apk">
                {t('join.tabletDownload', { size })}
              </a>
            </>
          )}
        </div>
      </>
    );
  }

  return (
    <I18nContext.Provider value={i18n}>
      <Page name={name} languageSwitch layout={split ? 'split' : 'single'}>
        {body}
      </Page>
      {/* Takes no files, but keeps a dropped one from replacing the page. */}
      <DropZone onFiles={null} label="" />
    </I18nContext.Provider>
  );
}

function Step({ n, title, detail }: { n: number; title: string; detail: string }) {
  return (
    <div class="step">
      <span class="num">{n}</span>
      <div>
        <b>{title}</b>
        <span>{detail}</span>
      </div>
    </div>
  );
}
