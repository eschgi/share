import type { ComponentType } from 'preact';
import { useEffect, useMemo, useReducer, useRef, useState } from 'preact/hooks';
import { ApiError, endSession, getInfo, getSession, unlock, type EncryptKey, type Info } from './api';
import { fromB64u } from './e2ee/bytes';
import { forgetPinSecret, keepPinSecret } from './e2ee/pinlink';
import { DropZone } from './components/DropZone';
import { Page, PagePlaces, type Places } from './components/Page';
import { useLeaveWarning } from './device';
import { formatPercent } from './format';
import { I18nContext, isLang, languages, makeI18n, pickLanguage, storeLanguage, storedLanguage, type Lang } from './i18n';
import { claimShared, dropShared, shareFailed, sharedGone, type Shared } from './incoming';
import { noticeLanguage, notify } from './notify';
import { pinFromHash, pinSecretFromHash } from './pin';
import { DoneScreen } from './screens/DoneScreen';
import { PinScreen } from './screens/PinScreen';
import { ReadyScreen } from './screens/ReadyScreen';
import { SendingScreen } from './screens/SendingScreen';
import { WelcomeScreen } from './screens/WelcomeScreen';
import { initialState, reduce, type PinProblem } from './state';
import { Uploader, holdQueueLock } from './uploader';

function problemOf(e: unknown): PinProblem {
  if (e instanceof ApiError) {
    switch (e.code) {
      case 'pin_wrong':
        return { kind: 'wrong', attemptsLeft: e.attemptsLeft ?? null };
      case 'pin_format': // not a possible PIN; doesn't count as a try
        return { kind: 'wrong', attemptsLeft: null };
      case 'pin_ended':
        return { kind: 'ended' };
      case 'pin_locked':
        return { kind: 'locked', until: Date.now() + (e.retryAfterSeconds ?? 60) * 1000 };
    }
  }
  return { kind: 'network' };
}

type SeeModule = { See: ComponentType<{ name: string; onEnded: () => void }> };
let seeLoading: Promise<SeeModule> | null = null;

/** A guest's look into a PIN's folder comes in a chunk of its own, loaded once it is wanted. */
function loadSee(): Promise<SeeModule> {
  seeLoading ??= import('./guest/See').catch((e: unknown) => {
    seeLoading = null;
    throw e;
  });
  return seeLoading;
}

export function App() {
  const [info, setInfo] = useState<Info | null>(null);
  const [state, dispatch] = useReducer(reduce, initialState);
  const [lang, setLang] = useState<Lang>(() => pickLanguage(languages, storedLanguage(), navigator.languages, 'en'));
  const [online, setOnline] = useState(navigator.onLine);
  const [rejected, setRejected] = useState<string[]>([]);
  /** Files shared from other apps, waiting for a PIN that works. */
  const [shared, setShared] = useState<Shared[]>([]);
  const [, setTick] = useState(0);
  const redraw = () => setTick((n) => n + 1);
  const uploader = useRef<Uploader | null>(null);
  /** The PIN's session for the uploader, which outlives renders; and the key its folder's files
   * are encrypted for, once asked again after the server refused an older one. */
  const sessionNow = useRef(state.session);
  sessionNow.current = state.session;
  const encryptKey = useRef<EncryptKey | null | undefined>(undefined);
  /** A PIN that shows its folder: sending, or looking into the folder. */
  const [tab, setTab] = useState<'send' | 'see'>('send');
  const [See, setSee] = useState<SeeModule['See'] | null>(null);
  const [seeFailed, setSeeFailed] = useState(false);

  useEffect(() => {
    document.documentElement.lang = lang;
    noticeLanguage(lang);
  }, [lang]);

  useEffect(() => {
    const on = () => setOnline(true);
    const off = () => setOnline(false);
    addEventListener('online', on);
    addEventListener('offline', off);
    return () => {
      removeEventListener('online', on);
      removeEventListener('offline', off);
    };
  }, []);

  async function doUnlock(code: string, secret?: string | null) {
    dispatch({ type: 'unlockStarted' });
    try {
      const res = await unlock(code);
      // The answer carries the session cookie, but a browser that blocks cookies drops it
      // silently, and every upload would then be refused.
      if (!(await getSession()).session) {
        dispatch({ type: 'unlockFailed', problem: { kind: 'noCookie' } });
        return;
      }
      if (!uploader.current) {
        location.reload(); // the first load failed; start clean with the new session
        return;
      }
      // The secret of a link of a PIN that shows an encrypted folder opens it on the See tab.
      if (secret) keepPinSecret(secret);
      dispatch({ type: 'unlocked', session: res.session });
      uploader.current.resume(); // the server has moved unfinished uploads to this session
    } catch (e) {
      dispatch({ type: 'unlockFailed', problem: problemOf(e) });
    }
  }

  useEffect(() => {
    // A PIN in the link (share.example.com/#K7M2Q) unlocks by itself; take it out of the
    // address so it doesn't stay in the history.
    const hashPin = pinFromHash(location.hash);
    const hashSecret = pinSecretFromHash(location.hash);
    if (location.hash) history.replaceState(null, '', location.pathname + location.search);
    (async () => {
      try {
        const [i, keepQueue] = await Promise.all([getInfo(), holdQueueLock()]);
        setInfo(i);
        setLang(pickLanguage(i.languages, storedLanguage(), navigator.languages, i.default_language));
        uploader.current = new Uploader(i, {
          onChange: redraw,
          onAllDone: (files, bytes) => {
            dispatch({ type: 'allDone', files, bytes });
            void notify({ kind: 'sent', files, bytes });
          },
          onSessionEnded: (lost) => {
            dispatch({ type: 'sessionEnded', lost });
            void notify({ kind: lost ? 'pinLost' : 'pinEnded' });
          },
          onFailed: (failed) => void notify({ kind: 'failed', failed }),
          onRejected: (name) => setRejected((r) => (r.includes(name) ? r : [...r, name])),
          onRestored: () => dispatch({ type: 'restored' }),
          onSharedGone: sharedGone,
          encryptFor: () => {
            const s = sessionNow.current;
            const e = encryptKey.current !== undefined ? encryptKey.current : s?.kind === 'pin' ? s.encrypt : null;
            return e ? { folder: e.folder, version: e.version, publicKey: fromB64u(e.public_key) } : null;
          },
          refreshKeys: async () => {
            encryptKey.current = (await getSession()).session?.encrypt ?? null;
          },
        }, keepQueue);
        void claimShared().then(setShared);
        if (hashPin) {
          await doUnlock(hashPin, hashSecret);
          return;
        }
        const { session, ended } = await getSession();
        dispatch({ type: 'booted', session, sessionEnded: ended });
      } catch {
        dispatch({ type: 'bootFailed' });
      }
    })();
  }, []);

  const i18n = useMemo(
    () =>
      makeI18n(lang, info ? info.languages.filter(isLang) : [...languages], (l) => {
        storeLanguage(l);
        setLang(l);
      }),
    [lang, info],
  );

  // Shared files go out as soon as a PIN works.
  useEffect(() => {
    if (shared.length === 0 || !uploader.current || (state.screen !== 'ready' && state.screen !== 'welcome')) return;
    setRejected([]);
    uploader.current.addShared(shared);
    setShared([]);
    dispatch({ type: 'filesAdded' });
  }, [shared, state.screen]);

  const name = info?.name ?? 'Share';
  const snap = uploader.current?.snapshot() ?? null;
  const unfinished = !!snap && snap.done + snap.failed + snap.ghosts.length < snap.total;

  // While sending, the tab shows how far it is, also when another tab is in front.
  const title =
    state.screen === 'sending' && snap && snap.bytesTotal > 0 ? `${formatPercent(snap.bytesDone / snap.bytesTotal, lang)} · ${name}` : name;
  useEffect(() => {
    document.title = title;
  }, [title]);
  // Closing the tab in the middle asks first, also while waiting for a new PIN.
  useLeaveWarning((state.screen === 'sending' && unfinished) || state.waitingForPin);

  const onFiles = (files: File[]) => {
    if (files.length === 0) return; // e.g. a dropped folder with nothing but hidden files
    setRejected([]);
    uploader.current?.add(files);
    dispatch({ type: 'filesAdded' });
  };
  const onSkipGhosts = () => {
    uploader.current?.skipGhosts();
    if (uploader.current?.snapshot().total === 0) dispatch({ type: 'startedOver' }); // nothing left
  };
  const sendMore = () => {
    uploader.current?.clear();
    dispatch({ type: 'sendMore' });
  };
  // Only between batches. Without the server's answer the old PIN's cookie stays, but the next
  // PIN replaces it.
  const forgetPin = async () => {
    await endSession().catch(() => {});
    forgetPinSecret();
    uploader.current?.clear();
    dispatch({ type: 'pinForgotten' });
  };

  // Where files dropped on the page go: wherever files can be picked, and on screen 5 only the
  // ones to pick again. Never before a PIN works.
  let dropTo: ((files: File[]) => void) | null = null;
  let dropLabel = '';
  switch (state.screen) {
    case 'ready':
      dropTo = onFiles;
      dropLabel = i18n.t('drop.send');
      break;
    case 'sending':
      dropTo = onFiles;
      dropLabel = i18n.t('drop.add');
      break;
    case 'welcome':
      if (snap?.ghosts.length) {
        dropTo = onFiles;
        dropLabel = i18n.t('drop.pickAgain');
      }
      break;
    case 'done':
      dropTo = (files) => {
        if (files.length === 0) return;
        sendMore();
        onFiles(files);
      };
      dropLabel = i18n.t('drop.send');
      break;
  }

  // A PIN that shows its folder gets Send and See, once it works.
  const shows = state.session?.kind === 'pin' && state.session.shows_folder && state.screen !== 'pin' && state.screen !== 'boot';
  const seeing = shows && tab === 'see';
  useEffect(() => {
    if (!seeing || See) return;
    setSeeFailed(false);
    loadSee().then(
      (m) => setSee(() => m.See),
      () => setSeeFailed(true),
    );
  }, [seeing]);
  if (seeing && dropTo) {
    // Files dropped while looking go out, on the Send tab.
    const send = dropTo;
    dropTo = (files) => {
      setTab('send');
      send(files);
    };
  }
  const places: Places | null = shows
    ? {
        label: name,
        items: [
          { key: 'send', icon: 'upload', label: i18n.t('ready.tabSend') },
          { key: 'see', icon: 'images', label: i18n.t('ready.tabSee') },
        ],
        on: tab,
        choose: (k) => setTab(k === 'see' ? 'see' : 'send'),
      }
    : null;

  let screen;
  switch (seeing ? 'see' : state.screen) {
    case 'see':
      screen = See ? (
        <See
          name={name}
          onEnded={() => {
            setTab('send');
            dispatch({ type: 'sessionEnded', lost: false });
          }}
        />
      ) : (
        <Page name={name} languageSwitch>
          <p class="lead">{i18n.t(seeFailed ? 'pin.network' : 'common.loading')}</p>
        </Page>
      );
      break;
    case 'boot':
      screen = (
        <Page name={name}>
          <p class="lead">{i18n.t('common.loading')}</p>
        </Page>
      );
      break;
    case 'pin':
      screen = (
        <PinScreen
          name={name}
          problem={state.problem}
          unlocking={state.unlocking}
          onSubmit={doUnlock}
          waiting={shared.length}
          shareFailed={shareFailed}
          onDontSend={() => {
            void dropShared(shared.map((s) => s.key));
            setShared([]);
          }}
        />
      );
      break;
    case 'ready':
      screen = (
        <ReadyScreen name={name} session={state.session!} onFiles={onFiles} shareFailed={shareFailed} onForgetPin={() => void forgetPin()} />
      );
      break;
    case 'welcome':
      screen = (
        <WelcomeScreen
          name={name}
          snapshot={snap!}
          onFiles={onFiles}
          onSkipGhosts={onSkipGhosts}
          onContinue={() => {
            uploader.current?.continue();
            dispatch({ type: 'continued' });
          }}
          onStartOver={() => {
            uploader.current?.startOver();
            dispatch({ type: 'startedOver' });
          }}
        />
      );
      break;
    case 'sending':
      screen = (
        <SendingScreen
          name={name}
          snapshot={snap!}
          online={online}
          rejected={rejected}
          onFiles={onFiles}
          onSkipGhosts={onSkipGhosts}
          onRetry={() => uploader.current?.retryFailed()}
        />
      );
      break;
    case 'done':
      screen = (
        <DoneScreen
          name={name}
          files={state.done!.files}
          bytes={state.done!.bytes}
          offerInstall={state.session?.kind === 'pin' && state.session.pin_kind === 'permanent'}
          onMore={sendMore}
          onForgetPin={state.session?.kind === 'pin' ? () => void forgetPin() : undefined}
        />
      );
      break;
  }
  return (
    <I18nContext.Provider value={i18n}>
      <PagePlaces.Provider value={places}>{screen}</PagePlaces.Provider>
      <DropZone onFiles={dropTo} label={dropLabel} />
    </I18nContext.Provider>
  );
}
