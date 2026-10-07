// Showing files whatever they are: the server's addresses for plain ones, decrypted ones for
// encrypted files (docs/e2ee-plan.md).
import { useEffect, useState } from 'preact/hooks';
import { contentUrl, thumbUrl, type FileInfo } from '../api';
import { decryptedBlob, isEncrypted, streamUrl, thumbBlobUrl } from './files';
import { keyring, type Keyring } from './keyring';

/** The keys of this page; re-renders whenever they change. */
export function useKeyring(): Keyring {
  const [, bump] = useState(0);
  useEffect(() => keyring.watch(() => bump((n) => n + 1)), []);
  return keyring;
}

/** Where a picture is: src once there is one; locked while an encrypted file can't be opened
 * here (its folder's key isn't open, or not yet). */
export interface Source {
  src: string | null;
  locked: boolean;
}

const plain = (src: string): Source => ({ src, locked: false });

/** A file's thumbnail. */
export function useThumbSrc(f: FileInfo): Source {
  const keys = useKeyring();
  const [got, setGot] = useState<Source>({ src: null, locked: false });
  const ready = isEncrypted(f) && keys.hasFolderKey(f.folder, f.enc.version);
  useEffect(() => {
    if (!isEncrypted(f) || !f.has_thumb) return;
    if (!ready) {
      setGot({ src: null, locked: true });
      return;
    }
    let live = true;
    thumbBlobUrl(f).then(
      (src) => live && setGot(plain(src)),
      () => live && setGot({ src: null, locked: true }),
    );
    return () => {
      live = false;
    };
  }, [f.id, f.updated_at, ready]);
  if (!isEncrypted(f)) return plain(thumbUrl(f));
  return got;
}

/** A file's contents, to show a photo or play a sound: a blob URL of the decrypted file for an
 * encrypted one, freed when it isn't shown any more, or with stream, an address from the
 * service worker. Nothing is fetched unless wanted. */
export function useContentSrc(f: FileInfo, stream = false, wanted = true): Source {
  const keys = useKeyring();
  const [got, setGot] = useState<Source>({ src: null, locked: false });
  const ready = isEncrypted(f) && keys.hasFolderKey(f.folder, f.enc.version);
  useEffect(() => {
    if (!isEncrypted(f) || !wanted) return;
    if (!ready) {
      setGot({ src: null, locked: true });
      return;
    }
    let live = true;
    let made: string | null = null;
    const ctl = new AbortController();
    const url = stream ? streamUrl(f, false) : decryptedBlob(f, ctl.signal).then((b) => URL.createObjectURL(b));
    url.then(
      (src) => {
        if (src.startsWith('blob:')) made = src;
        if (live) setGot(plain(src));
        else if (made) URL.revokeObjectURL(made);
      },
      () => live && setGot({ src: null, locked: true }),
    );
    return () => {
      live = false;
      ctl.abort();
      if (made) URL.revokeObjectURL(made);
    };
  }, [f.id, f.updated_at, ready, stream, wanted]);
  if (!isEncrypted(f)) return plain(contentUrl(f));
  return got;
}

/** Downloads an encrypted file under its name, decrypted. */
export async function downloadDecrypted(f: FileInfo, start: (href: string, name: string) => void): Promise<void> {
  if (!isEncrypted(f)) return start(contentUrl(f), f.name);
  start(await streamUrl(f, true), f.name);
}
