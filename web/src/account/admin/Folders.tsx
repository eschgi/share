import { useState } from 'preact/hooks';
import { ApiError, createFolder, deleteFolder, renameFolder, setFolderInvite, setFolderPerson, type FolderInfo, type People, type PinInfo } from '../../api';
import { Icon } from '../../components/Icon';
import { formatTime, formatWhen, daysAgo } from '../../format';
import { useI18n, type Lang } from '../../i18n';
import { navigate } from '../../router';
import { Avatar, AvatarStack, Link, Row, Switch } from '../components/Bits';
import { Confirm, Modal } from '../components/Modal';
import { useAccount } from '../context';
import { whoSees } from '../folders/folders';
import { FolderCover, useFolderLines } from '../folders/Folders';
import { refreshFolders } from '../folders/store';
import { NewPinDialog, PinsList } from './Pins';

/** "Since 26 Sep": when a folder was made, the day first as in the app. */
function since(when: string, lang: Lang): string {
  const d = new Date(when);
  const year = d.getFullYear() !== new Date().getFullYear() ? 'numeric' : undefined;
  return new Intl.DateTimeFormat(lang === 'en' ? 'en-GB' : lang, { day: 'numeric', month: 'short', year }).format(d);
}

/** When an invite ends: "21:00" today, else with the day. */
function inviteEnd(lang: Lang, when: string): string {
  const end = new Date(when);
  const now = new Date();
  return daysAgo(end, now) === 0 && end >= now ? formatTime(end, lang) : formatWhen(end, now, lang);
}

/** What a folder's problem with a change is, in words. */
function folderProblem(t: (key: string) => string, e: unknown): string {
  if (!(e instanceof ApiError) || e.status === 0) return t('common.offline');
  switch (e.code) {
    case 'folder_name_taken':
      return t('folders.nameTaken');
    case 'bad_request':
      return t('folders.nameBad');
    case 'folder_busy':
      return t('folders.busy');
    case 'last_folder':
      return t('folders.lastFolder');
  }
  return t('common.failed');
}

/** Screen 40: every folder, with what it holds, who sees it and whether a PIN sends into it. */
export function FoldersList({ list, people, pins }: { list: FolderInfo[]; people: People | null; pins: PinInfo[] | null }) {
  const { t, tn } = useI18n();
  const lines = useFolderLines();
  return (
    <>
      <p class="lead sm">{t('folders.lead')}</p>
      <div class="group">
        {list.map((f) => {
          const sending = pins?.filter((p) => p.folder === f.id).length ?? 0;
          const line = [lines.count(f.files), sending > 0 ? tn('folders.pins', sending) : '', f.admins_only ? t('folders.onlyAdmins').toLowerCase() : '']
            .filter(Boolean)
            .join(' · ');
          return (
            <Link key={f.id} href={`/settings/folders/${f.id}`} class="row">
              <FolderCover folder={f} />
              <span class="rt">
                <b>{f.name}</b>
                <span>{line}</span>
              </span>
              {people && <AvatarStack people={whoSees(people, f.id)} />}
              <Icon name="chev" class="chev" />
            </Link>
          );
        })}
      </div>
    </>
  );
}

/** Screen 41: a folder, who sees it, the PINs that send into it, and renaming or deleting it. */
export function FolderPage({
  folder,
  list,
  people,
  pins,
  onChanged,
}: {
  folder: FolderInfo;
  list: FolderInfo[];
  people: People | null;
  pins: PinInfo[] | null;
  onChanged: () => Promise<void>;
}) {
  const { t, lang } = useI18n();
  const { toast } = useAccount();
  const lines = useFolderLines();
  /** Switches already turned, before the server's answer. */
  const [turned, setTurned] = useState<Map<string, boolean>>(new Map());
  const [dialog, setDialog] = useState<'pin' | 'rename' | 'delete' | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);

  const turn = async (key: string, sees: boolean, change: () => Promise<void>) => {
    setTurned((m) => new Map(m).set(key, sees));
    try {
      await change();
      await Promise.all([onChanged(), refreshFolders()]);
    } catch (e) {
      toast({ text: folderProblem(t, e) });
    }
    setTurned((m) => {
      const next = new Map(m);
      next.delete(key);
      return next;
    });
  };

  const remove = async () => {
    setBusy(true);
    setProblem(null);
    try {
      await deleteFolder(folder.id);
    } catch (e) {
      setBusy(false);
      return setProblem(folderProblem(t, e));
    }
    setBusy(false);
    setDialog(null);
    toast({ text: t('folders.deleted', { folder: folder.name }) });
    navigate('/settings/folders', { replace: true });
    await Promise.all([refreshFolders(), onChanged()]);
  };

  const sending = pins?.filter((p) => p.folder === folder.id) ?? [];
  return (
    <>
      <div class="fhead">
        <FolderCover folder={folder} large />
        <span>
          <b>{folder.name}</b>
          <span>
            {lines.holds(folder.files, folder.bytes)} · {t('folders.since', { date: since(folder.created_at, lang) })}
          </span>
        </span>
      </div>
      <p class="glabel">{t('folders.whoSees')}</p>
      {people ? (
        <div class="group">
          {people.users.map((u) => {
            const admin = u.role === 'admin';
            const sees = admin || (turned.get(u.id) ?? u.folders.includes(folder.id));
            return (
              <button
                key={u.id}
                type="button"
                class="row"
                role="switch"
                aria-checked={sees}
                aria-disabled={admin}
                onClick={admin ? undefined : () => void turn(u.id, !sees, () => setFolderPerson(folder.id, u.id, !sees))}
              >
                <Avatar id={u.id} name={u.name} />
                <span class="rt">
                  <b>{u.name}</b>
                  <span>{t(admin ? 'folders.adminSeesAll' : 'role.member')}</span>
                </span>
                <Switch on={sees} locked={admin} />
              </button>
            );
          })}
          {people.invites
            .filter((i) => i.user_id === null)
            .map((i) => {
              const admin = i.role === 'admin';
              const sees = admin || (turned.get(i.id) ?? i.folders.includes(folder.id));
              return (
                <button
                  key={i.id}
                  type="button"
                  class="row"
                  role="switch"
                  aria-checked={sees}
                  aria-disabled={admin}
                  onClick={admin ? undefined : () => void turn(i.id, !sees, () => setFolderInvite(folder.id, i.id, !sees))}
                >
                  <Avatar id={i.id} name={i.name} pending />
                  <span class="rt">
                    <b>{i.name}</b>
                    <span>{t('people.inviteOpen', { when: inviteEnd(lang, i.expires_at) })}</span>
                  </span>
                  <Switch on={sees} locked={admin} />
                </button>
              );
            })}
        </div>
      ) : (
        <span class="spinner" role="status" aria-label={t('common.loading')} />
      )}
      <p class="glabel">
        {t('folders.pinsInto')}
        <button type="button" class="gact" onClick={() => setDialog('pin')}>
          <Icon name="plus" />
          {t('folders.newPin')}
        </button>
      </p>
      {sending.length > 0 ? <PinsList pins={sending} onChanged={onChanged} bare /> : <p class="help">{t('folders.noPins')}</p>}
      <div class="group fmore">
        <Row icon="folder" title={t('folders.rename')} chevron={false} onClick={() => setDialog('rename')} />
        {list.length > 1 && <Row icon="trash" title={t('folders.delete')} chevron={false} tone="danger" onClick={() => setDialog('delete')} />}
      </div>
      {dialog === 'pin' && (
        <NewPinDialog
          folder={folder.id}
          onClose={() => setDialog(null)}
          onCreated={() => {
            setDialog(null);
            void onChanged();
          }}
        />
      )}
      {dialog === 'rename' && <FolderNameDialog folder={folder} onClose={() => setDialog(null)} />}
      {dialog === 'delete' && (
        <Confirm
          icon="trash"
          title={t('folders.deleteTitle', { folder: folder.name })}
          body={t('folders.deleteBody')}
          confirm={t('folders.delete')}
          danger
          busy={busy}
          problem={problem}
          onConfirm={() => void remove()}
          onClose={() => setDialog(null)}
        />
      )}
    </>
  );
}

/** A new folder's name, or a new name for folder; a new folder opens its page. */
export function FolderNameDialog({ folder, onClose }: { folder?: FolderInfo; onClose: () => void }) {
  const { t } = useI18n();
  const [name, setName] = useState(folder?.name ?? '');
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);

  const save = async (e: Event) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setProblem(null);
    try {
      const made = folder ? await renameFolder(folder.id, name.trim()) : await createFolder(name.trim());
      await refreshFolders();
      onClose();
      if (!folder) navigate(`/settings/folders/${made.id}`);
    } catch (err) {
      setBusy(false);
      setProblem(folderProblem(t, err));
    }
  };

  return (
    <Modal title={t(folder ? 'folders.renameTitle' : 'folders.newTitle')} onClose={onClose}>
      <form onSubmit={save}>
        <label class="label first" for="folder-name">
          {t('folders.name')}
        </label>
        <input
          id="folder-name"
          class="input"
          maxLength={60}
          autoComplete="off"
          value={name}
          onInput={(e) => {
            setName(e.currentTarget.value);
            setProblem(null);
          }}
          autoFocus
        />
        {problem ? (
          <p class="help err" role="alert">
            <Icon name="alert" />
            {problem}
          </p>
        ) : (
          <p class="help">{t(folder ? 'folders.renameHelp' : 'folders.newHelp')}</p>
        )}
        <div class="dbtns">
          <button type="button" class="tbtn" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button type="submit" class="btn sm primary" disabled={busy || !name.trim()}>
            {t(folder ? 'folders.renameButton' : 'folders.create')}
          </button>
        </div>
      </form>
    </Modal>
  );
}
