import { useEffect, useMemo, useReducer, useRef, useState } from 'preact/hooks';
import { ApiError, getInfo, getSession, unlock, type Info } from './api';
import { Brand } from './components/Brand';
import { I18nContext, isLang, languages, makeI18n, pickLanguage, storeLanguage, storedLanguage, type Lang } from './i18n';
import { pinFromHash } from './pin';
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

export function App() {
  const [info, setInfo] = useState<Info | null>(null);
  const [state, dispatch] = useReducer(reduce, initialState);
  const [lang, setLang] = useState<Lang>(() => pickLanguage(languages, storedLanguage(), navigator.languages, 'en'));
  const [online, setOnline] = useState(navigator.onLine);
  const [rejected, setRejected] = useState<string[]>([]);
  const [, setTick] = useState(0);
  const redraw = () => setTick((n) => n + 1);
  const uploader = useRef<Uploader | null>(null);

  useEffect(() => {
    document.documentElement.lang = lang;
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

  async function doUnlock(code: string) {
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
    if (location.hash) history.replaceState(null, '', location.pathname + location.search);
    (async () => {
      try {
        const [i, keepQueue] = await Promise.all([getInfo(), holdQueueLock()]);
        setInfo(i);
        setLang(pickLanguage(i.languages, storedLanguage(), navigator.languages, i.default_language));
        // Over plain http (other than localhost) browsers refuse the session cookie, which is
        // Secure, so a PIN would seem to work and every upload would then be refused.
        if (!isSecureContext) {
          dispatch({ type: 'insecure' });
          return;
        }
        uploader.current = new Uploader(i, {
          onChange: redraw,
          onAllDone: (files, bytes) => dispatch({ type: 'allDone', files, bytes }),
          onSessionEnded: (lost) => dispatch({ type: 'sessionEnded', lost }),
          onRejected: (name) => setRejected((r) => (r.includes(name) ? r : [...r, name])),
          onRestored: () => dispatch({ type: 'restored' }),
        }, keepQueue);
        if (hashPin) {
          await doUnlock(hashPin);
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

  const name = info?.name ?? 'Share';
  const onFiles = (files: File[]) => {
    setRejected([]);
    uploader.current?.add(files);
    dispatch({ type: 'filesAdded' });
  };
  const onSkipGhosts = () => {
    uploader.current?.skipGhosts();
    if (uploader.current?.snapshot().total === 0) dispatch({ type: 'startedOver' }); // nothing left
  };

  let screen;
  switch (state.screen) {
    case 'boot':
      screen = (
        <main class="screen">
          <Brand name={name} />
          <p class="lead">{i18n.t('common.loading')}</p>
        </main>
      );
      break;
    case 'pin':
      screen = <PinScreen name={name} problem={state.problem} unlocking={state.unlocking} onSubmit={doUnlock} />;
      break;
    case 'ready':
      screen = <ReadyScreen name={name} session={state.session!} onFiles={onFiles} />;
      break;
    case 'welcome':
      screen = (
        <WelcomeScreen
          name={name}
          snapshot={uploader.current!.snapshot()}
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
          snapshot={uploader.current!.snapshot()}
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
          offerInstall={state.session?.pin_kind === 'permanent'}
          onMore={() => {
            uploader.current?.clear();
            dispatch({ type: 'sendMore' });
          }}
        />
      );
      break;
  }
  return <I18nContext.Provider value={i18n}>{screen}</I18nContext.Provider>;
}
