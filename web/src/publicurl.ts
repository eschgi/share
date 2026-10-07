// The server's public address, for pages opened over plain http: browsers encrypt only over
// https or on localhost, so those point there for encrypted folders.
import { useEffect, useState } from 'preact/hooks';
import { getInfo } from './api';

/** The server's public address when it is an https one; null until known, or without one. */
export function usePublicUrl(): string | null {
  const [url, setUrl] = useState<string | null>(null);
  useEffect(() => {
    getInfo().then(
      (i) => setUrl(i.public_url?.startsWith('https://') ? i.public_url : null),
      () => {},
    );
  }, []);
  return url;
}

/** "share.example.com" for "https://share.example.com/". */
export function hostOf(url: string): string {
  return new URL(url).host;
}
