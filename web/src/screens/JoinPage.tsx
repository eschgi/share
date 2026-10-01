import { useEffect, useMemo, useState } from 'preact/hooks';
import { ApiError, getApp, getInfo, peekInvite, type AppInfo, type Info, type InvitePeek } from '../api';
import { DropZone } from '../components/DropZone';
import { Icon } from '../components/Icon';
import { Page } from '../components/Page';
import { QrCode } from '../components/QrCode';
import { formatBytes, formatWhen } from '../format';
import { I18nContext, isLang, languages, makeI18n, pickLanguage, storeLanguage, storedLanguage, type Lang } from '../i18n';

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

/** The invite token from the link: <server>/join#shi_… */
export function tokenFromHash(hash: string): string | null {
  const t = decodeURIComponent(hash.replace(/^#/, ''));
  return /^shi_[A-Za-z0-9_-]{20,}$/.test(t) ? t : null;
}

/**
 * The link that hands the invite to the installed app. Chrome opens the app with
 * <scheme>://join?server=…&token=…, or, without the app, comes back to this page.
 */
export function intentLink(app: AppInfo, server: string, token: string): string {
  const q = new URLSearchParams({ server, token }).toString();
  const fallback = encodeURIComponent(`${server}/join#${token}`);
  return `intent://join?${q}#Intent;scheme=${app.link_scheme};package=${app.android_package};S.browser_fallback_url=${fallback};end`;
}

/** Screen 9: what an invite link opens when the app isn't installed yet. */
export function JoinPage() {
  // Read the token before it leaves the address bar, so it doesn't stay in the history.
  const token = useMemo(() => tokenFromHash(location.hash), []);
  const [info, setInfo] = useState<Info | null>(null);
  const [lang, setLang] = useState<Lang>(() => pickLanguage(languages, storedLanguage(), navigator.languages, 'en'));
  const [state, setState] = useState<State>({ kind: 'loading' });

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
  } else {
    const { peek, app } = state;
    const android = /Android/i.test(navigator.userAgent);
    const until = formatWhen(new Date(peek.expires_at), new Date(), lang);
    const size = app.apk ? formatBytes(app.apk.size, lang) : '';
    // On a computer or an iPhone the invite goes to the Android phone as a QR code: the phone's
    // camera opens it there, and the app can scan it as well.
    const scan = !android && !!app.apk && !!token;
    split = android || scan;
    body = (
      <>
        <div class="pane">
          <div class="appico lg">
            <Icon name="images" />
          </div>
          <h1 class="hero md">{peek.inviter ? t('join.invitedBy', { inviter: peek.inviter }) : t('join.invited')}</h1>
          <p class="lead">{t(scan ? 'join.leadPhone' : 'join.lead', { name })}</p>
          {android && app.apk ? (
            <div class="steps">
              <Step n={1} title={t('join.step1')} detail={t('join.step1Detail', { size })} />
              <Step n={2} title={t('join.step2')} detail={t('join.step2Detail')} />
              <Step n={3} title={t('join.step3')} detail={t('join.step3Detail', { when: until })} />
            </div>
          ) : scan ? (
            <div class="steps">
              <Step n={1} title={t('join.scanStep')} detail={t('join.scanStepDetail')} />
              <Step n={2} title={t('join.installStep')} detail={t('join.installStepDetail')} />
              <Step n={3} title={t('join.joinStep')} detail={t('join.step3Detail', { when: until })} />
            </div>
          ) : (
            <p class="help">{t('join.noApk')}</p>
          )}
        </div>
        <div class="grow" />
        <div class="pane">
          {android && app.apk && (
            <a class="btn primary" href="/download/share.apk" download="share.apk">
              <Icon name="download" />
              {t('join.download')}
            </a>
          )}
          {android && token && (
            <a class="btn link" href={intentLink(app, location.origin, token)}>
              {t('join.already', { name: peek.name })}
            </a>
          )}
          {scan && (
            <>
              <div class="qrcard">
                <QrCode text={`${location.origin}/join#${token}`} label={t('join.qrLabel')} />
                <b>{t('join.scan')}</b>
                <span class="exp">
                  <Icon name="clock" />
                  {t('join.scanUntil', { when: until })}
                </span>
              </div>
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
