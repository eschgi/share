import '../admin/admin.css';
import { useState } from 'preact/hooks';
import { ApiError, deleteMe, logout } from '../../api';
import { Icon } from '../../components/Icon';
import { useMedia } from '../../device';
import { formatBytes } from '../../format';
import { setSignedInHint } from '../../hint';
import { useI18n } from '../../i18n';
import { dropShared } from '../../incoming';
import { useRoute } from '../../router';
import { storedTheme, type ThemeChoice } from '../../theme';
import { got, useAdminData, type AdminData } from '../admin/data';
import { InviteDialog, PeopleGroup } from '../admin/People';
import { NewPinDialog, PinsList } from '../admin/Pins';
import { SelectAllTrash, TrashList, useTrash } from '../admin/Trash';
import { Avatar, Link, RoleBadge, Row } from '../components/Bits';
import { Confirm } from '../components/Modal';
import { useAccount } from '../context';
import { clearMarks } from '../save/marks';
import { Shell, TitleBar } from '../Shell';
import { DevicesDialog, LanguageDialog, PasswordDialog, ThemeDialog, themeSummary } from './dialogs';

/** The admin's pages besides the list: on a computer beside it, elsewhere pages of their own. */
type Page = 'pins' | 'people' | 'trash';

const twoPanes = '(min-width: 1024px) and (min-height: 540px)';

/**
 * Screens 17, 30 and 37: the person, how the website looks, and leaving; admins also run Share
 * here: upload PINs, people, Recently deleted and the storage. On a computer the admin's list
 * stays on the left and the chosen page opens on the right.
 */
export function Settings() {
  const { t } = useI18n();
  const route = useRoute();
  const { me } = useAccount();
  const admin = me.user.role === 'admin';
  const wide = useMedia(twoPanes);
  const data = useAdminData(admin);
  const sub = route.path.split('/')[2];
  const page: Page | null = admin && (sub === 'pins' || sub === 'people' || sub === 'trash') ? sub : null;

  if (admin && wide) {
    const right = page ?? 'people';
    return (
      <Shell tab="settings">
        <div class="dset">
          <div class="pane">
            <SettingsList data={data} current={right} />
          </div>
          <div class="pane pane2">
            <AdminPage page={right} data={data} />
          </div>
        </div>
      </Shell>
    );
  }
  if (page === 'pins' || page === 'trash') {
    return (
      <Shell tab="settings">
        <AdminPage page={page} data={data} back />
      </Shell>
    );
  }
  return (
    <Shell tab="settings">
      <TitleBar title={t('nav.settings')} />
      <div class="settings">
        <SettingsList data={data} />
      </div>
    </Shell>
  );
}

/** One of the admin's pages, with its title and what it offers at the top. On its own page it
 * has the title bar's arrow back on a phone, and a link back on a tablet. */
function AdminPage({ page, data, back }: { page: Page; data: AdminData; back?: boolean }) {
  const { t } = useI18n();
  const [making, setMaking] = useState(false);
  const [inviting, setInviting] = useState(false);
  const trash = useTrash(() => void data.reload());

  let title: string;
  let action = null;
  /** The same in a phone's title bar, where there is less room. */
  let barAction = null;
  let body;
  if (page === 'pins') {
    title = t('pins.title');
    action = (
      <button type="button" class="btn primary xs" onClick={() => setMaking(true)}>
        <Icon name="plus" />
        {t('pins.new')}
      </button>
    );
    barAction = (
      <button type="button" class="ib tonal" aria-label={t('pins.new')} title={t('pins.new')} onClick={() => setMaking(true)}>
        <Icon name="plus" />
      </button>
    );
    const pins = data.pins;
    body = pins === 'failed' ? <p class="help">{t('common.offline')}</p> : pins ? <PinsList pins={pins} onChanged={data.reload} /> : <Spinner />;
  } else if (page === 'trash') {
    title = t('trash.title');
    action = <SelectAllTrash state={trash} />;
    body = <TrashList state={trash} />;
  } else {
    title = t('people.title');
    action = (
      <button type="button" class="btn tonal xs" onClick={() => setInviting(true)}>
        <Icon name="user-plus" />
        {t('people.invite')}
      </button>
    );
    const people = data.people;
    body =
      people === 'failed' ? <p class="help">{t('common.offline')}</p> : people ? <PeopleGroup people={people} onChanged={() => void data.reload()} /> : <Spinner />;
  }
  return (
    <>
      {back && (
        <TitleBar title={title} back="/settings">
          {barAction ?? action}
        </TitleBar>
      )}
      <div class={back ? 'settings' : 'adminpage'}>
        <div class="phead">
          {back && (
            <Link href="/settings" class="ib" aria-label={t('common.back')}>
              <Icon name="back" />
            </Link>
          )}
          <h2>{title}</h2>
          {action}
        </div>
        {body}
      </div>
      {making && (
        <NewPinDialog
          onClose={() => setMaking(false)}
          onCreated={() => {
            setMaking(false);
            void data.reload();
          }}
        />
      )}
      {inviting && (
        <InviteDialog
          onClose={() => {
            setInviting(false);
            void data.reload();
          }}
        />
      )}
    </>
  );
}

function Spinner() {
  const { t } = useI18n();
  return <span class="spinner" role="status" aria-label={t('common.loading')} />;
}

type Dialog = 'devices' | 'language' | 'theme' | 'password' | 'signOut' | 'delete' | 'invite';

/** The list: the person, then the admin's parts, how the website looks, and leaving. With
 * current, it sits beside the admin's pages (on a computer), which open on the right. */
function SettingsList({ data, current }: { data: AdminData; current?: Page }) {
  const { t, tn, lang } = useI18n();
  const { me, languageAuto } = useAccount();
  const admin = me.user.role === 'admin';
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const [theme, setTheme] = useState<ThemeChoice>(storedTheme);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const close = () => {
    setDialog(null);
    setProblem(null);
  };

  const leave = async (how: 'signOut' | 'delete') => {
    setBusy(true);
    setProblem(null);
    try {
      await (how === 'signOut' ? logout() : deleteMe());
    } catch (e) {
      // The page can't remove the cookie itself; without the server's answer nothing changed.
      setBusy(false);
      if (e instanceof ApiError && e.code === 'last_admin') return setProblem(t('delete.lastAdmin'));
      return setProblem(t(e instanceof ApiError && e.status > 0 ? 'common.failed' : 'common.offline'));
    }
    setSignedInHint(false);
    await Promise.all([clearMarks(), dropShared()]);
    location.replace('/');
  };

  const pins = got(data.pins);
  const people = got(data.people);
  const storage = got(data.storage);
  const pinsLine = pins
    ? pins.length === 0
      ? t('pins.noneShort')
      : [tn('pins.countPermanent', pins.filter((p) => p.kind === 'permanent').length), t('pins.countDay', { n: pins.filter((p) => p.kind === 'day').length })].join(' · ')
    : '';
  const peopleLine = people
    ? [tn('people.count', people.users.length), people.invites.length > 0 ? tn('people.invitesOpen', people.invites.length) : ''].filter(Boolean).join(' · ')
    : '';
  const trashLine = storage ? (storage.trash_files === 0 ? t('trash.emptyShort') : tn('trash.summary', storage.trash_files, { size: formatBytes(storage.trash_bytes, lang) })) : '';
  const freeLine =
    storage && storage.total_bytes > 0 ? t('storage.free', { free: formatBytes(storage.free_bytes, lang), total: formatBytes(storage.total_bytes, lang) }) : '';

  const language = t(`lang.${lang}`);
  const languageRow = (
    <Row icon="globe" title={t('settings.language')} sub={languageAuto ? t('settings.languageAuto', { language }) : language} onClick={() => setDialog('language')} />
  );
  const themeRow = <Row icon="palette" title={t('settings.theme')} sub={themeSummary(t, theme)} onClick={() => setDialog('theme')} />;
  const passwordRow = (
    <Row
      icon="lock"
      title={t('settings.password')}
      sub={me.user.has_password ? t('settings.passwordSet', { username: me.user.username ?? '' }) : t('settings.passwordNone')}
      onClick={() => setDialog('password')}
    />
  );
  const pinsRow = <Row icon="key" tone="accent" title={t('pins.title')} sub={pinsLine} href="/settings/pins" current={current === 'pins'} />;

  return (
    <>
      <button type="button" class="profile" onClick={() => setDialog('devices')}>
        <Avatar id={me.user.id} name={me.user.name} size="lg" />
        <span class="rt">
          <b>
            {me.user.name} <RoleBadge role={me.user.role} crown />
          </b>
          <span>{t(me.device.home_only ? 'settings.signedInAtHome' : 'settings.signedInHere')}</span>
        </span>
        <Icon name="chev" class="chev" />
      </button>
      {admin && current ? (
        <>
          <div class="group">
            {pinsRow}
            <Row icon="users" title={t('people.title')} sub={peopleLine} href="/settings/people" current={current === 'people'} />
            <Row icon="trash" title={t('trash.title')} sub={trashLine} href="/settings/trash" current={current === 'trash'} />
            {storage && (
              <div class="row">
                <span class="ri">
                  <Icon name="hdd" />
                </span>
                <span class="rt">
                  <b>{t('storage.title')}</b>
                  <span>{freeLine || t('storage.setOnServer')}</span>
                </span>
              </div>
            )}
          </div>
          <div class="group">
            {languageRow}
            {themeRow}
            {passwordRow}
          </div>
        </>
      ) : admin ? (
        <>
          <div class="group">
            {pinsRow}
            {languageRow}
            {themeRow}
          </div>
          <p class="glabel">
            {t('people.title')}
            <button type="button" class="gact" onClick={() => setDialog('invite')}>
              <Icon name="user-plus" />
              {t('people.invite')}
            </button>
          </p>
          {data.people === 'failed' ? (
            <p class="help">{t('common.offline')}</p>
          ) : people ? (
            <PeopleGroup people={people} onChanged={() => void data.reload()} />
          ) : (
            <span class="spinner" role="status" aria-label={t('common.loading')} />
          )}
          {storage && (
            <>
              <p class="glabel">{t('storage.title')}</p>
              <div class="group">
                <div class="row">
                  <span class="ri">
                    <Icon name="hdd" />
                  </span>
                  <span class="rt">
                    <b class="mono">{storage.storage_dir}</b>
                    <span>{[t('storage.setOnServer'), freeLine].filter(Boolean).join(' · ')}</span>
                  </span>
                </div>
                <Row icon="trash" title={t('trash.title')} sub={trashLine} href="/settings/trash" />
              </div>
            </>
          )}
          <div class="group">{passwordRow}</div>
        </>
      ) : (
        <>
          <div class="group">
            {languageRow}
            {themeRow}
          </div>
          <div class="group">{passwordRow}</div>
        </>
      )}
      <div class="group">
        <Row icon="log-out" title={t('settings.signOut')} chevron={false} onClick={() => setDialog('signOut')} />
        <Row icon="user-x" title={t('settings.deleteAccount')} chevron={false} tone="danger" onClick={() => setDialog('delete')} />
      </div>
      {dialog === 'devices' && <DevicesDialog onClose={close} />}
      {dialog === 'language' && <LanguageDialog onClose={close} />}
      {dialog === 'theme' && <ThemeDialog choice={theme} onChoose={setTheme} onClose={close} />}
      {dialog === 'password' && <PasswordDialog onClose={close} />}
      {dialog === 'invite' && (
        <InviteDialog
          onClose={() => {
            close();
            void data.reload();
          }}
        />
      )}
      {dialog === 'signOut' && (
        <Confirm
          title={t('signOut.title')}
          body={t('signOut.body')}
          confirm={t('settings.signOut')}
          busy={busy}
          problem={problem}
          onConfirm={() => void leave('signOut')}
          onClose={close}
        />
      )}
      {dialog === 'delete' && (
        <Confirm
          title={t('delete.title')}
          body={t('delete.body')}
          confirm={t('delete.button')}
          danger
          busy={busy}
          problem={problem}
          onConfirm={() => void leave('delete')}
          onClose={close}
        />
      )}
    </>
  );
}
