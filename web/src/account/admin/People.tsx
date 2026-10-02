import { useEffect, useState } from 'preact/hooks';
import {
  ApiError,
  createInvite,
  inviteDevice,
  removePerson,
  setRole,
  signOutDevice,
  withdrawInvite,
  type ListedDevice,
  type NewInvite,
  type OpenInvite,
  type People,
  type Person,
  type Role,
} from '../../api';
import { Icon } from '../../components/Icon';
import { QrCode } from '../../components/QrCode';
import { formatTime, formatWhen, daysAgo } from '../../format';
import { useI18n, type Lang } from '../../i18n';
import { Avatar, RoleBadge } from '../components/Bits';
import { Confirm, Modal } from '../components/Modal';
import { useAccount } from '../context';
import { lastUsed } from '../settings/dialogs';
import { personLine } from './format';
import { NewPasswordDialog } from './NewPassword';
import { copyText, sharesLinks, shareText } from './share';

/** When an invite ends: "21:00" today, else with the day. */
function inviteEnd(lang: Lang, when: string): string {
  const end = new Date(when);
  const now = new Date();
  return daysAgo(end, now) === 0 && end >= now ? formatTime(end, lang) : formatWhen(end, now, lang);
}

/** Screen 30's people: everyone with an account, then the invites nobody has used yet. */
export function PeopleGroup({ people, onChanged }: { people: People; onChanged: () => void }) {
  const i18n = useI18n();
  const { t, lang } = i18n;
  const [person, setPerson] = useState<Person | null>(null);
  const [invite, setInvite] = useState<OpenInvite | null>(null);
  const now = new Date();
  return (
    <>
      <div class="group">
        {people.users.map((p) => (
          <button key={p.id} type="button" class="row" onClick={() => setPerson(p)}>
            <Avatar id={p.id} name={p.name} />
            <span class="rt">
              <b>{p.name}</b>
              <span>{personLine(i18n, p, now)}</span>
            </span>
            <RoleBadge role={p.role} />
            <Icon name="chev" class="chev" />
          </button>
        ))}
        {people.invites.map((i) => (
          <button key={i.id} type="button" class="row" onClick={() => setInvite(i)}>
            <Avatar id={i.id} name={i.name} pending />
            <span class="rt">
              <b>{i.name}</b>
              <span>{t('people.inviteOpen', { when: inviteEnd(lang, i.expires_at) })}</span>
            </span>
            <span class="role pend">{t('people.invited')}</span>
            <Icon name="chev" class="chev" />
          </button>
        ))}
      </div>
      {person && (
        <PersonDialog
          person={person}
          onClose={() => {
            setPerson(null);
            onChanged();
          }}
        />
      )}
      {invite && (
        <InviteInfo
          invite={invite}
          people={people}
          onClose={() => {
            setInvite(null);
            onChanged();
          }}
        />
      )}
    </>
  );
}

type Asking = { kind: 'signOut'; device: ListedDevice } | { kind: 'remove' } | null;

/** Screen 31: one person: their phones and browsers, their role, and removing them. */
function PersonDialog({ person, onClose }: { person: Person; onClose: () => void }) {
  const i18n = useI18n();
  const { t } = i18n;
  const { refreshMe } = useAccount();
  const [p, setP] = useState(person);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [asking, setAsking] = useState<Asking>(null);
  const [adding, setAdding] = useState(false);
  const [newPassword, setNewPassword] = useState(false);

  /** Runs a change; a refusal, such as for the last admin, shows as a message. */
  const run = async (change: () => Promise<void>): Promise<boolean> => {
    setBusy(true);
    setProblem(null);
    try {
      await change();
      return true;
    } catch (e) {
      setProblem(
        e instanceof ApiError && e.code === 'last_admin' ? t('people.lastAdmin') : t(e instanceof ApiError && e.status > 0 ? 'common.failed' : 'common.offline'),
      );
      return false;
    } finally {
      setBusy(false);
    }
  };

  const toggleRole = async () => {
    const role: Role = p.role === 'admin' ? 'member' : 'admin';
    if (await run(() => setRole(p.id, role))) {
      setP({ ...p, role });
      if (p.me) await refreshMe().catch(() => {}); // the settings change with it
    }
  };

  const signOut = async (d: ListedDevice) => {
    if (await run(() => signOutDevice(d.id))) setP({ ...p, phones: p.phones.filter((x) => x.id !== d.id) });
    setAsking(null);
  };

  const remove = async () => {
    const ok = await run(() => removePerson(p.id));
    setAsking(null);
    if (ok) onClose();
  };

  return (
    <Modal
      title={p.name}
      wide
      onClose={onClose}
      head={
        <div class="pdh">
          <Avatar id={p.id} name={p.name} size="lg" />
          <div>
            <b>{p.name}</b>
            {p.username && <span>@{p.username}</span>}
          </div>
          <RoleBadge role={p.role} />
        </div>
      }
    >
      <p class="glabel">{t('people.devices')}</p>
      <div class="group">
        {p.phones.map((d) => (
          <div key={d.id} class="row">
            <span class="ri">
              <Icon name={d.client === 'web' ? 'monitor' : 'smartphone'} />
            </span>
            <span class="rt">
              <b>{d.name}</b>
              <span>
                {d.this ? t(d.client === 'web' ? 'devices.thisBrowser' : 'devices.thisPhone') : lastUsed(i18n, d.last_seen_at)}
                {d.home_only ? ` · ${t('devices.atHome')}` : ''}
              </span>
            </span>
            {!d.this && (
              <button type="button" class="tbtn danger" disabled={busy} onClick={() => setAsking({ kind: 'signOut', device: d })}>
                {t('devices.signOut')}
              </button>
            )}
          </div>
        ))}
        <button type="button" class="row acc" onClick={() => setAdding(true)}>
          <span class="ri acc">
            <Icon name="plus" />
          </span>
          <span class="rt">
            <b>{t('people.addDevice')}</b>
          </span>
        </button>
      </div>
      <div class="group mt12">
        <button type="button" class="row" disabled={busy} onClick={() => void toggleRole()}>
          <span class="ri">
            <Icon name="crown" />
          </span>
          <span class="rt">
            <b>{t(p.role === 'admin' ? 'people.makeMember' : 'people.makeAdmin')}</b>
            <span>{t(p.role === 'admin' ? 'join.roleMember' : 'join.roleAdmin')}</span>
          </span>
        </button>
        {!p.me && (
          <button type="button" class="row" disabled={busy} onClick={() => setNewPassword(true)}>
            <span class="ri">
              <Icon name="key" />
            </span>
            <span class="rt">
              <b>{t('people.newPassword')}</b>
              <span>{p.has_password ? t('people.newPasswordForgot', { name: p.name }) : t('people.newPasswordFirst')}</span>
            </span>
          </button>
        )}
        {!p.me && (
          <button type="button" class="row dang" disabled={busy} onClick={() => setAsking({ kind: 'remove' })}>
            <span class="ri dang">
              <Icon name="user-x" />
            </span>
            <span class="rt">
              <b>{t('people.remove', { name: p.name })}</b>
            </span>
          </button>
        )}
      </div>
      {problem && (
        <p class="help err" role="alert">
          <Icon name="alert" />
          {problem}
        </p>
      )}
      {asking?.kind === 'signOut' && (
        <Confirm
          title={t('devices.signOutTitle', { name: asking.device.name })}
          body={t('devices.signOutBody')}
          confirm={t('devices.signOut')}
          danger
          busy={busy}
          onConfirm={() => void signOut(asking.device)}
          onClose={() => setAsking(null)}
        />
      )}
      {asking?.kind === 'remove' && (
        <Confirm
          title={t('people.removeTitle', { name: p.name })}
          body={t('people.removeBody')}
          confirm={t('people.remove', { name: p.name })}
          danger
          busy={busy}
          problem={problem}
          onConfirm={() => void remove()}
          onClose={() => setAsking(null)}
        />
      )}
      {adding && <InviteDialog forPerson={p} onClose={() => setAdding(false)} />}
      {newPassword && (
        <NewPasswordDialog person={p} onDone={(username) => setP({ ...p, username, has_password: true })} onClose={() => setNewPassword(false)} />
      )}
    </Modal>
  );
}

/** An invite nobody has used yet: who it is for, and withdrawing it. */
function InviteInfo({ invite, people, onClose }: { invite: OpenInvite; people: People; onClose: () => void }) {
  const { t, lang } = useI18n();
  const [busy, setBusy] = useState(false);
  const owner = invite.user_id ? people.users.find((u) => u.id === invite.user_id) : undefined;
  const withdraw = async () => {
    setBusy(true);
    try {
      await withdrawInvite(invite.id);
    } catch {
      // it shows again in the list
    }
    onClose();
  };
  return (
    <Modal
      title={invite.name}
      onClose={onClose}
      head={
        <div class="pdh">
          <Avatar id={invite.id} name={invite.name} size="lg" pending />
          <div>
            <b>{invite.name}</b>
            <span>{owner ? t('people.inviteForDevice', { name: owner.name }) : t('people.inviteOpen', { when: inviteEnd(lang, invite.expires_at) })}</span>
          </div>
        </div>
      }
    >
      <div class="group">
        <button type="button" class="row dang" disabled={busy} onClick={() => void withdraw()}>
          <span class="ri dang">
            <Icon name="x" />
          </span>
          <span class="rt">
            <b>{t('people.withdraw')}</b>
          </span>
        </button>
      </div>
    </Modal>
  );
}

/** Screen 32: an invite as a QR code and a link, for someone new, or a phone or browser for
 * someone who has an account. */
export function InviteDialog({ forPerson, onClose }: { forPerson?: Person; onClose: () => void }) {
  const { t } = useI18n();
  const { toast } = useAccount();
  const [name, setName] = useState('');
  const [role, setRoleChoice] = useState<Role>('member');
  const [invite, setInvite] = useState<NewInvite | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const who = forPerson?.name ?? name.trim();
  const shares = sharesLinks();

  const create = async () => {
    if (busy || (!forPerson && !who)) return;
    setBusy(true);
    setProblem(null);
    try {
      setInvite(forPerson ? await inviteDevice(forPerson.id) : await createInvite(who, role));
    } catch (e) {
      setProblem(t(e instanceof ApiError && e.status > 0 ? 'common.failed' : 'common.offline'));
    } finally {
      setBusy(false);
    }
  };
  useEffect(() => {
    if (forPerson) void create();
  }, []);

  const handOn = async (link: string) => {
    if (shares) await shareText(t('invite.shareText', { name: who, link }));
    else toast({ text: t((await copyText(link)) ? 'common.copied' : 'common.notCopied') });
  };
  const again = () => {
    setInvite(null);
    setName('');
    setRoleChoice('member');
  };

  return (
    <Modal title={forPerson ? t('invite.deviceTitle', { name: forPerson.name }) : t('invite.title')} onClose={onClose} wide>
      {!forPerson && (
        <>
          <label class="label first" for="invite-name">
            {t('join.factName')}
          </label>
          <input
            id="invite-name"
            class="input"
            value={name}
            readOnly={!!invite}
            autoCapitalize="words"
            enterKeyHint="done"
            onInput={(e) => setName(e.currentTarget.value)}
            onKeyDown={(e) => e.key === 'Enter' && !invite && void create()}
          />
          <p class="label">{t('join.factRole')}</p>
          <div class="seg" role="radiogroup" aria-label={t('join.factRole')}>
            {(['member', 'admin'] as const).map((r) => (
              <button
                key={r}
                type="button"
                role="radio"
                aria-checked={role === r}
                class={role === r ? 'on' : ''}
                disabled={!!invite}
                onClick={() => setRoleChoice(r)}
              >
                {role === r && <Icon name="check" />}
                {t(r === 'admin' ? 'role.admin' : 'role.member')}
              </button>
            ))}
          </div>
          <p class="help">{t('invite.roleHelp')}</p>
        </>
      )}
      {invite && (
        <div class="qrrow">
          <QrCode text={invite.link} label={t('invite.qrLabel', { name: who })} />
          <div>
            <b>{forPerson ? t('invite.deviceLead', { name: who }) : t('invite.scanThis', { name: who })}</b>
            <span class="exp">
              <Icon name="clock" />
              {t('invite.worksOnce')}
            </span>
            <button type="button" class="btn tonal sm" onClick={() => void handOn(invite.link)}>
              <Icon name={shares ? 'share' : 'copy'} />
              {t(shares ? 'invite.sendLink' : 'invite.copy')}
            </button>
          </div>
        </div>
      )}
      {!invite && forPerson && busy && <span class="spinner" role="status" aria-label={t('common.loading')} />}
      {problem && (
        <p class="help err" role="alert">
          <Icon name="alert" />
          {problem}
        </p>
      )}
      <div class="dbtns">
        {invite ? (
          <>
            {!forPerson && (
              <button type="button" class="tbtn dleft" onClick={again}>
                {t('invite.another')}
              </button>
            )}
            <button type="button" class="btn sm outline" onClick={onClose}>
              {t('common.close')}
            </button>
          </>
        ) : (
          !forPerson && (
            <button type="button" class="btn sm primary" disabled={busy || !who} onClick={() => void create()}>
              <Icon name="qr" />
              {t('invite.showCode')}
            </button>
          )
        )}
      </div>
    </Modal>
  );
}
