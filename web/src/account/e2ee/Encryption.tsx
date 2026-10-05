// End-to-end encryption on the account's pages (docs/e2ee-plan.md): a folder's switch, the
// recovery code, and what a browser without its keys can do.
import './e2ee.css';
import { useEffect, useState } from 'preact/hooks';
import { ApiError, getSettings, putSettings, setFolderEncryption, type FolderInfo } from '../../api';
import { Icon } from '../../components/Icon';
import { QrCode } from '../../components/QrCode';
import { SealError } from '../../e2ee/formats';
import { useKeyring } from '../../e2ee/hooks';
import { keyring } from '../../e2ee/keyring';
import { useI18n } from '../../i18n';
import { Row, Switch } from '../components/Bits';
import { Confirm, Modal } from '../components/Modal';
import { useAccount } from '../context';
import { refreshFolders } from '../folders/store';

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
      setProblem(t(e instanceof ApiError && e.status === 0 ? 'common.offline' : 'common.failed'));
    }
    setBusy(false);
  };

  const turnOff = async () => {
    setBusy(true);
    setProblem(null);
    try {
      await setFolderEncryption(folder.id, false);
      await refreshFolders();
      onChanged();
      setStep(null);
    } catch (e) {
      setProblem(t(e instanceof ApiError && e.status === 0 ? 'common.offline' : 'common.failed'));
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

/** What a browser without its keys says, over the library: it waits for another phone or
 * browser of the person; admins can use the recovery code; and anyone can start over. */
export function KeysBanner() {
  const { t } = useI18n();
  const { me } = useAccount();
  const keys = useKeyring();
  const [dialog, setDialog] = useState<'recovery' | 'startOver' | null>(null);
  const [busy, setBusy] = useState(false);
  const encryptedFolders = (keys.answer?.folders ?? []).length > 0;
  if (keys.status !== 'waiting' || !encryptedFolders) return null;
  return (
    <div class="keysbanner" role="status">
      <Icon name="lock" />
      <span>
        <b>{t('keys.waitingTitle')}</b>
        <span>{t('keys.waiting')}</span>
      </span>
      <span class="keysbanner-actions">
        {me.user.role === 'admin' && (
          <button type="button" class="btn xs outline" onClick={() => setDialog('recovery')}>
            {t('recovery.use')}
          </button>
        )}
        <button type="button" class="btn xs link" onClick={() => setDialog('startOver')}>
          {t('keys.startOver')}
        </button>
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
