// End-to-end encryption on the account's pages (docs/e2ee-plan.md): a folder's switch, the
// recovery code, what a browser without its keys can do, and the checks before keys are passed on.
import './e2ee.css';
import { useEffect, useState } from 'preact/hooks';
import { ApiError, getSettings, putSettings, type FolderInfo } from '../../api';
import { Bold } from '../../components/Bits';
import { Icon } from '../../components/Icon';
import { QrCode } from '../../components/QrCode';
import { SealError } from '../../e2ee/formats';
import { useKeyring } from '../../e2ee/hooks';
import { keyring, NeedsRoot, type Ask, type ShownCode } from '../../e2ee/keyring';
import { useI18n, type I18n } from '../../i18n';
import { hostOf, usePublicUrl } from '../../publicurl';
import { Row, Switch } from '../components/Bits';
import { Confirm, Modal } from '../components/Modal';
import { useAccount } from '../context';
import { refreshFolders, useFolders } from '../folders/store';

/** The recovery code, shown once: to write down, copy or print, with its QR code for the app. */
export function RecoveryCodeDialog({ code, onDone }: { code: string; onDone: () => void }) {
  const { t } = useI18n();
  const { toast } = useAccount();
  const [kept, setKept] = useState(false);
  const copy = () =>
    navigator.clipboard.writeText(code).then(
      () => toast({ text: t('recovery.copied') }),
      () => toast({ text: t('common.notCopied') }),
    );
  return (
    <Modal title={t('recovery.title')} icon="key" onClose={() => kept && onDone()}>
      <p class="modal-text">{t('recovery.lead')}</p>
      <div class="recovery-print">
        <p class="recovery-code" translate={false}>
          <span>{code.split('-').slice(0, 4).join('-')}</span>
          <span>{code.split('-').slice(4).join('-')}</span>
        </p>
        <QrCode text={code} label={t('recovery.qrLabel')} />
        <p class="recovery-note">{t('recovery.printNote')}</p>
      </div>
      <div class="recovery-actions">
        <button type="button" class="btn sm outline" onClick={() => void copy()}>
          <Icon name="copy" />
          {t('recovery.copy')}
        </button>
        <button type="button" class="btn sm outline" onClick={() => print()}>
          <Icon name="download" />
          {t('recovery.print')}
        </button>
      </div>
      <label class="check">
        <input type="checkbox" checked={kept} onChange={(e) => setKept(e.currentTarget.checked)} />
        {t('recovery.kept')}
      </label>
      <div class="dbtns">
        <button type="button" class="btn sm primary" disabled={!kept} onClick={onDone}>
          {t('recovery.done')}
        </button>
      </div>
    </Modal>
  );
}

/** Opens every encrypted folder with the recovery code, e.g. in a new browser. */
export function UseRecoveryDialog({ onClose }: { onClose: () => void }) {
  const { t, tn } = useI18n();
  const { toast } = useAccount();
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const use = async (e: Event) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setProblem(null);
    try {
      const n = await keyring.useRecoveryCode(code);
      await refreshFolders();
      toast({ text: tn('recovery.opened', n) });
      onClose();
    } catch (err) {
      setBusy(false);
      setProblem(t(err instanceof SealError ? 'recovery.wrong' : err instanceof ApiError && err.status === 0 ? 'common.offline' : 'common.failed'));
    }
  };
  return (
    <Modal title={t('recovery.useTitle')} icon="key" onClose={onClose}>
      <form onSubmit={(e) => void use(e)}>
        <p class="modal-text">{t('recovery.useLead')}</p>
        <label class="label first" for="recovery-code">
          {t('recovery.code')}
        </label>
        <input
          id="recovery-code"
          class="input mono"
          autoComplete="off"
          autoCapitalize="characters"
          spellcheck={false}
          value={code}
          onInput={(e) => setCode(e.currentTarget.value)}
          autoFocus
        />
        {problem && (
          <p class="help err" role="alert">
            <Icon name="alert" />
            {problem}
          </p>
        )}
        <div class="dbtns">
          <button type="button" class="tbtn" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button type="submit" class="btn sm primary" disabled={busy || code.replace(/[-\s]/g, '').length < 32}>
            {t('recovery.use')}
          </button>
        </div>
      </form>
    </Modal>
  );
}

/** Admins: a folder's switch for encrypting its new files. The first encrypted folder makes the
 * recovery key, whose code is shown once, before the folder's key. */
export function EncryptionSwitch({ folder, start, onChanged }: { folder: FolderInfo; start?: boolean; onChanged: () => void }) {
  const { t } = useI18n();
  const { toast } = useAccount();
  const keys = useKeyring();
  const [step, setStep] = useState<'ask' | 'off' | 'code' | null>(start && !folder.encrypted ? 'ask' : null);
  const [code, setCode] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const ready = keys.status === 'ready';

  const turnOn = async () => {
    // Until the keys are loaded, a recovery key there isn't known yet, and would be replaced.
    if (!ready) return;
    setBusy(true);
    setProblem(null);
    try {
      if (!keyring.hasRecovery()) {
        setCode(await keyring.makeRecovery());
        setBusy(false);
        return setStep('code');
      }
      await keyring.encryptFolder(folder);
      await refreshFolders();
      onChanged();
      setStep(null);
      toast({ text: t('encryption.on', { folder: folder.name }) });
    } catch (e) {
      setProblem(t(e instanceof NeedsRoot ? 'encryption.needsRoot' : e instanceof ApiError && e.status === 0 ? 'common.offline' : 'common.failed'));
    }
    setBusy(false);
  };

  const turnOff = async () => {
    setBusy(true);
    setProblem(null);
    try {
      await keyring.switchOff(folder);
      await refreshFolders();
      onChanged();
      setStep(null);
    } catch (e) {
      setProblem(t(e instanceof NeedsRoot ? 'encryption.needsRoot' : e instanceof ApiError && e.status === 0 ? 'common.offline' : 'common.failed'));
    }
    setBusy(false);
  };

  return (
    <>
      <button
        type="button"
        class="row"
        role="switch"
        aria-checked={folder.encrypted}
        aria-disabled={!ready}
        onClick={ready ? () => setStep(folder.encrypted ? 'off' : 'ask') : undefined}
      >
        <span class="ri">
          <Icon name="lock" />
        </span>
        <span class="rt">
          <b>{t('encryption.switch')}</b>
          <span>{t(ready ? (folder.encrypted ? 'encryption.switchOn' : 'encryption.switchOff') : 'encryption.noKeysHere')}</span>
        </span>
        <Switch on={folder.encrypted} locked={!ready} />
      </button>
      {step === 'ask' && (
        <Confirm
          icon="lock"
          title={t('encryption.askTitle', { folder: folder.name })}
          body={t(keyring.hasRecovery() ? 'encryption.askBody' : 'encryption.askFirst')}
          confirm={t('encryption.turnOn')}
          busy={busy || !ready}
          problem={problem}
          onConfirm={() => void turnOn()}
          onClose={() => setStep(null)}
        />
      )}
      {step === 'off' && (
        <Confirm
          icon="lock-open"
          title={t('encryption.offTitle', { folder: folder.name })}
          body={t('encryption.offBody')}
          confirm={t('encryption.turnOff')}
          busy={busy}
          problem={problem}
          onConfirm={() => void turnOff()}
          onClose={() => setStep(null)}
        />
      )}
      {step === 'code' && code && (
        <RecoveryCodeDialog
          code={code}
          onDone={() => {
            setCode(null);
            void turnOn();
          }}
        />
      )}
    </>
  );
}

/** A check's code, in two groups of three, as the other screen shows it. */
function CheckCode({ code }: { code: string }) {
  const digits = (from: number) => [...code.slice(from, from + 3)].map((d, i) => <b key={from + i}>{d}</b>);
  return (
    <span class="kcode" role="img" aria-label={`${code.slice(0, 3)} ${code.slice(3)}`}>
      {digits(0)}
      <i />
      {digits(3)}
    </span>
  );
}

/** The codes this browser shows: one, or one per device that asks, with its name. */
function ShownCodes({ codes }: { codes: ShownCode[] }) {
  if (codes.length === 1) return <CheckCode code={codes[0].code} />;
  return (
    <span class="kcodes">
      {codes.map((c) => (
        <span key={c.from + c.code} class="kfrom">
          <span>{c.from}</span>
          <CheckCode code={c.code} />
        </span>
      ))}
    </span>
  );
}

/** What a browser without its keys says, over the library: it waits until one of the person's
 * phones or browsers allows it, with the code that one shows too, or for an admin to allow its
 * person's new key; admins can use the recovery code; and anyone can start over. */
export function KeysBanner() {
  const { t } = useI18n();
  const { me } = useAccount();
  const keys = useKeyring();
  const folders = useFolders();
  if (keys.status === 'insecure') return folders.list?.some((f) => f.key_version !== null) ? <InsecureBanner /> : null;
  const [dialog, setDialog] = useState<'recovery' | 'startOver' | null>(null);
  const [busy, setBusy] = useState(false);
  const encryptedFolders = (keys.answer?.folders ?? []).length > 0;
  const waiting = keys.status === 'waiting' && encryptedFolders;
  if (!waiting && !keys.waitsForFolders) return <KeysWaitingList />;
  const codes = keys.codes.filter((c) => c.kind === (waiting ? 'device' : 'person'));
  return (
    <>
      <div class="keysbanner" role="status">
        <Icon name="lock" />
        <span>
          <b>{t(waiting ? 'keys.waitingTitle' : 'keys.adminTitle')}</b>
          <span>
            {waiting ? `${t('keys.waiting')}${codes.length ? ` ${t('keys.sameCode')}` : ''}` : t(codes.length ? 'keys.adminCode' : 'keys.admin')}
          </span>
          {codes.length > 0 && <ShownCodes codes={codes} />}
        </span>
        <span class="keysbanner-actions">
          {me.user.role === 'admin' && (
            <button type="button" class="btn xs outline" onClick={() => setDialog('recovery')}>
              {t('recovery.use')}
            </button>
          )}
          {waiting && (
            <button type="button" class="btn xs link" onClick={() => setDialog('startOver')}>
              {t('keys.startOver')}
            </button>
          )}
        </span>
        {dialog === 'recovery' && <UseRecoveryDialog onClose={() => setDialog(null)} />}
        {dialog === 'startOver' && (
          <Confirm
            icon="key"
            title={t('keys.startOverTitle')}
            body={t('keys.startOverBody')}
            confirm={t('keys.startOver')}
            busy={busy}
            onConfirm={() => {
              setBusy(true);
              void keyring.startOver().finally(() => {
                setBusy(false);
                setDialog(null);
              });
            }}
            onClose={() => setDialog(null)}
          />
        )}
      </div>
      <KeysWaitingList />
    </>
  );
}

/** Who waits for this browser's OK (screens 51 and 52): a new phone or browser of the person, or
 * another person and their folders. Nothing opens by itself: Show opens the dialog, which starts
 * the check, and Not now closes it again; the ask stays listed while it is due. */
/** Over plain http, browsers don't encrypt: encrypted folders don't open here, and nothing goes
 * into them from here. The server's https address, where it has one, does it all. */
function InsecureBanner() {
  const { t } = useI18n();
  const url = usePublicUrl();
  return (
    <div class="keysbanner" role="status">
      <Icon name="lock" />
      <span>
        <b>{t('keys.insecureTitle')}</b>
        <span>{url ? t('keys.insecure', { url: hostOf(url) }) : t('keys.insecureNoUrl')}</span>
      </span>
      {url && (
        <span class="keysbanner-actions">
          <a class="btn xs outline" href={url.replace(/\/$/, '') + location.pathname}>
            {t('keys.insecureOpen')}
          </a>
        </span>
      )}
    </div>
  );
}

function KeysWaitingList() {
  const { t } = useI18n();
  const keys = useKeyring();
  if (!keys.asks.length) return null;
  const opened = keys.opened;
  return (
    <div class="keysbanner" role="status">
      <Icon name="key" />
      <span>
        <b>{t('keys.laterTitle')}</b>
        {keys.asks.map((x) => (
          <span key={`${x.kind}:${x.id}`} class="klater">
            <span>{x.kind === 'person' ? t('keys.personTitle', { name: x.name }) : `${t(x.client === 'app' ? 'keys.askPhone' : 'keys.askBrowser')}: ${x.name}`}</span>
            <button type="button" class="btn xs outline" onClick={() => void keyring.show(x)}>
              {t('keys.show')}
            </button>
          </span>
        ))}
      </span>
      {opened && <KeysAsk key={`${opened.kind}:${opened.id}`} ask={opened} />}
    </div>
  );
}

/** "Signed in just now", "… 5 minutes ago", "… 2 hours ago", "… 3 days ago". */
function signedIn(i18n: I18n, since: string): string {
  const minutes = Math.max(0, Math.floor((Date.now() - new Date(since).getTime()) / 60_000));
  if (minutes < 1) return i18n.t('keys.signedInNow');
  if (minutes < 60) return i18n.tn('keys.signedInMinutes', minutes);
  if (minutes < 24 * 60) return i18n.tn('keys.signedInHours', Math.floor(minutes / 60));
  return i18n.tn('keys.signedInDays', Math.floor(minutes / (24 * 60)));
}

/** The dialog Show opens before this browser passes keys on (docs/e2ee-plan.md): to another phone
 * or browser of the person, or folder keys to another person. Both screens show the same code;
 * Allow passes the keys on, Not me signs that phone or browser out, Not now (or closing) ends the
 * check, and the ask stays listed. */
function KeysAsk({ ask }: { ask: Ask }) {
  const i18n = useI18n();
  const { t } = i18n;
  useKeyring();
  const folders = useFolders();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const phone = ask.client === 'app';
  const act = (run: () => Promise<void>) => {
    setBusy(true);
    setProblem(null);
    run()
      .catch((e: unknown) => setProblem(t(e instanceof ApiError && e.status > 0 ? 'common.failed' : 'common.offline')))
      .finally(() => setBusy(false));
  };
  const code = ask.code ? <CheckCode code={ask.code} /> : <p class="kwait">{t('keys.waitCode')}</p>;
  return (
    <Modal
      key={`${ask.kind}:${ask.id}`}
      title={ask.kind === 'device' ? t(phone ? 'keys.askPhone' : 'keys.askBrowser') : t('keys.personTitle', { name: ask.name })}
      icon="key"
      onClose={() => void keyring.hide(ask)}
    >
      {ask.kind === 'device' ? (
        <>
          <div class="group">
            <div class="row">
              <span class="ri acc">
                <Icon name={phone ? 'smartphone' : 'monitor'} />
              </span>
              <span class="rt">
                <b>{ask.name}</b>
                {ask.since && <span>{signedIn(i18n, ask.since)}</span>}
              </span>
            </div>
          </div>
          <p class="label">{t(phone ? 'keys.showsPhone' : 'keys.showsBrowser')}</p>
          {code}
          <p class="help">
            <span>
              <Bold text={t(phone ? 'keys.notMePhone' : 'keys.notMeBrowser')} />
            </span>
          </p>
        </>
      ) : (
        <>
          <p class="modal-text">{t(keyring.keyChanged(ask) ? 'keys.personNew' : 'keys.personFirst', { name: ask.name })}</p>
          <div class="kfolders">
            {(ask.folders ?? []).map((id) => (
              <span key={id} class="ftag">
                <Icon name="folder" />
                {folders.byId(id)?.name ?? '…'}
              </span>
            ))}
          </div>
          {ask.root && <p class="modal-text">{t('keys.personRoot', { name: ask.name })}</p>}
          <p class="label">{t('keys.personCode', { name: ask.name })}</p>
          {code}
          <p class="help">
            <span>
              <Bold text={t('keys.personHelp')} />
            </span>
          </p>
        </>
      )}
      {problem && (
        <p class="help err" role="alert">
          <Icon name="alert" />
          {problem}
        </p>
      )}
      <div class="dbtns">
        <button type="button" class="tbtn" disabled={busy} onClick={() => act(() => (ask.kind === 'device' ? keyring.deny(ask) : keyring.hide(ask)))}>
          {t(ask.kind === 'device' ? 'keys.notMe' : 'keys.notNow')}
        </button>
        <button type="button" class="btn sm primary" disabled={busy || !ask.code} onClick={() => act(() => keyring.allow(ask))}>
          <Icon name="check" />
          {t('keys.allow')}
        </button>
      </div>
    </Modal>
  );
}

/** Admins' settings for encryption: new folders encrypted or not, and the recovery code. */
export function EncryptionSettings() {
  const { t } = useI18n();
  const keys = useKeyring();
  const [on, setOn] = useState<boolean | null>(null);
  const [dialog, setDialog] = useState<'newCode' | 'use' | null>(null);
  const [code, setCode] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => {
    getSettings().then(
      (s) => setOn(s.new_folders_encrypted),
      () => {},
    );
  }, []);
  const ready = keys.status === 'ready';
  const flip = async () => {
    const next = !on;
    setOn(next);
    try {
      setOn((await putSettings({ new_folders_encrypted: next })).new_folders_encrypted);
    } catch {
      setOn(!next);
    }
  };
  const newCode = async () => {
    setBusy(true);
    setProblem(null);
    try {
      setCode(await keyring.makeRecovery());
      setDialog(null);
    } catch (e) {
      setProblem(t(e instanceof ApiError && e.status === 0 ? 'common.offline' : 'common.failed'));
    }
    setBusy(false);
  };
  return (
    <>
      <p class="glabel">{t('encryption.title')}</p>
      <div class="group">
        <button type="button" class="row" role="switch" aria-checked={!!on} onClick={on === null ? undefined : () => void flip()}>
          <span class="ri">
            <Icon name="lock" />
          </span>
          <span class="rt">
            <b>{t('encryption.newFolders')}</b>
            <span>{t('encryption.newFoldersSub')}</span>
          </span>
          <Switch on={!!on} />
        </button>
        <Row
          icon="key"
          title={t('recovery.title')}
          sub={t(keys.hasRecovery() ? 'recovery.made' : 'recovery.none')}
          chevron={false}
          onClick={ready && keys.hasRecovery() ? () => setDialog('newCode') : undefined}
        />
        {keys.hasRecovery() && <Row icon="lock-open" title={t('recovery.useTitle')} chevron={false} onClick={() => setDialog('use')} />}
      </div>
      {dialog === 'newCode' && (
        <Confirm
          icon="key"
          title={t('recovery.newTitle')}
          body={t('recovery.newBody')}
          confirm={t('recovery.newButton')}
          busy={busy}
          problem={problem}
          onConfirm={() => void newCode()}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog === 'use' && <UseRecoveryDialog onClose={() => setDialog(null)} />}
      {code && <RecoveryCodeDialog code={code} onDone={() => setCode(null)} />}
    </>
  );
}
