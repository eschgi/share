// What the phone can tell us or do for us: keep the screen on while sending, say whether it
// is on Wi-Fi, and install the site as an app. Each one is optional; a browser without it
// just doesn't show the matching bit.
import { useEffect, useState } from 'preact/hooks';

/** Keeps the screen on while active, so the phone doesn't go to sleep in the middle of sending. */
export function useWakeLock(active: boolean): void {
  useEffect(() => {
    if (!active || !('wakeLock' in navigator)) return;
    let lock: WakeLockSentinel | null = null;
    let taking = false;
    let stopped = false;
    const take = async () => {
      if (lock || taking || document.visibilityState !== 'visible') return;
      taking = true;
      try {
        const l = await navigator.wakeLock.request('screen');
        if (stopped) return void l.release();
        lock = l;
        l.addEventListener('release', () => (lock = null));
      } catch {
        // Not allowed right now, e.g. in battery saver mode.
      } finally {
        taking = false;
      }
    };
    // The phone lets go of the lock whenever the page is hidden; take it again on return.
    const onVisibility = () => void take();
    document.addEventListener('visibilitychange', onVisibility);
    void take();
    return () => {
      stopped = true;
      document.removeEventListener('visibilitychange', onVisibility);
      lock?.release().catch(() => {});
    };
  }, [active]);
}

interface NetworkInformation extends EventTarget {
  type?: string;
}

function connection(): NetworkInformation | undefined {
  return (navigator as Navigator & { connection?: NetworkInformation }).connection;
}

/** Whether the phone is on Wi-Fi, or null if the browser doesn't say (most desktops). */
export function useOnWifi(): boolean | null {
  const read = () => {
    const type = connection()?.type;
    return type ? type === 'wifi' : null;
  };
  const [wifi, setWifi] = useState(read);
  useEffect(() => {
    const c = connection();
    if (!c) return;
    const onChange = () => setWifi(read());
    c.addEventListener('change', onChange);
    return () => c.removeEventListener('change', onChange);
  }, []);
  return wifi;
}

interface BeforeInstallPromptEvent extends Event {
  prompt(): Promise<void>;
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>;
}

let installEvent: BeforeInstallPromptEvent | null = null;
const installListeners = new Set<() => void>();

function installChanged(e: BeforeInstallPromptEvent | null) {
  installEvent = e;
  installListeners.forEach((l) => l());
}

/** Call once at start: Chrome offers installing early, once per page load. */
export function captureInstallPrompt(): void {
  addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault(); // instead of Chrome's own bar, the card on screen 6 offers it
    installChanged(e as BeforeInstallPromptEvent);
  });
  addEventListener('appinstalled', () => installChanged(null));
}

/** Installing the site as an app, if the browser offers it: the function opens its dialog. */
export function useInstall(): (() => Promise<void>) | null {
  const [, redraw] = useState(0);
  useEffect(() => {
    const l = () => redraw((n) => n + 1);
    installListeners.add(l);
    return () => void installListeners.delete(l);
  }, []);
  const e = installEvent;
  if (!e || matchMedia('(display-mode: standalone)').matches) return null;
  return async () => {
    installChanged(null); // an event can show the dialog only once
    await e.prompt();
    await e.userChoice;
  };
}
