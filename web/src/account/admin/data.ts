// What the admin's settings show besides the person: the PINs, the people and the storage.
// Each part shows when it arrives; one that fails says so, and the next load tries again.
import { useCallback, useEffect, useState } from 'preact/hooks';
import { getPeople, getPins, getStorage, type People, type PinInfo, type Storage } from '../../api';

export type Loaded<T> = T | 'failed' | null;

export interface AdminData {
  pins: Loaded<PinInfo[]>;
  people: Loaded<People>;
  storage: Loaded<Storage>;
  reload: () => Promise<void>;
}

export function useAdminData(admin: boolean): AdminData {
  const [pins, setPins] = useState<Loaded<PinInfo[]>>(null);
  const [people, setPeople] = useState<Loaded<People>>(null);
  const [storage, setStorage] = useState<Loaded<Storage>>(null);
  const reload = useCallback(async () => {
    if (!admin) return;
    await Promise.all([
      getPins().then((r) => setPins(r.pins), () => setPins((p) => (Array.isArray(p) ? p : 'failed'))),
      getPeople().then(setPeople, () => setPeople((p) => (p && p !== 'failed' ? p : 'failed'))),
      getStorage().then(setStorage, () => setStorage((s) => (s && s !== 'failed' ? s : 'failed'))),
    ]);
  }, [admin]);
  useEffect(() => void reload(), [reload]);
  return { pins, people, storage, reload };
}

/** What has arrived, if it has. */
export function got<T>(v: Loaded<T>): T | null {
  return v === 'failed' ? null : v;
}
