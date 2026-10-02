import { useState } from 'preact/hooks';
import { ApiError, newPassword, type Person } from '../../api';
import { Icon } from '../../components/Icon';
import { useI18n } from '../../i18n';
import { Modal } from '../components/Modal';
import { useAccount } from '../context';
import { suggestedUsername, validUsername } from '../settings/username';
import { copyText, sharesLinks, shareText } from './share';

/**
 * A new password for someone who forgot theirs, or never had one: first the username they sign
 * in with, then the password the server made up, shown only now. Their phones and browsers
 * stay signed in. Not for oneself: Settings › Password is for that, with the current one.
 */
export function NewPasswordDialog({ person, onDone, onClose }: { person: Person; onDone: (username: string) => void; onClose: () => void }) {
  const { t } = useI18n();
  const { info, toast } = useAccount();
  const [username, setUsername] = useState(person.username ?? suggestedUsername(person.name));
  const [made, setMade] = useState<{ username: string; password: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const shares = sharesLinks();

  async function make(e: Event) {
    e.preventDefault();
    if (busy) return;
    if (!validUsername(username)) return setProblem(t('password.usernameBad'));
    setBusy(true);
    setProblem(null);
    try {
      const m = await newPassword(person.id, username.trim());
      setMade(m);
      onDone(m.username);
    } catch (err) {
      if (!(err instanceof ApiError) || err.status === 0) setProblem(t('common.offline'));
      else if (err.code === 'username_taken') setProblem(t('password.usernameTaken'));
      else if (err.code === 'bad_request') setProblem(t('password.usernameBad'));
      else setProblem(t('common.failed'));
    } finally {
      setBusy(false);
    }
  }

  const copy = async (text: string) => toast({ text: t((await copyText(text)) ? 'newPassword.copied' : 'newPassword.notCopied') });
  const send = (m: { username: string; password: string }) =>
    void shareText(t('newPassword.shareText', { server: info?.name ?? 'Share', username: m.username, password: m.password }));

  const field = (label: string, value: string) => (
    <div class="row">
      <span class="rt">
        <span>{label}</span>
        <b class="mono">{value}</b>
      </span>
      <button type="button" class="ib" aria-label={t('newPassword.copy', { what: label })} title={t('newPassword.copy', { what: label })} onClick={() => void copy(value)}>
        <Icon name="copy" />
      </button>
    </div>
  );

  return (
    <Modal title={t('newPassword.title', { name: person.name })} onClose={onClose}>
      {made ? (
        <>
          <div class="group newpw">
            {field(t('signIn.username'), made.username)}
            {field(t('settings.password'), made.password)}
          </div>
          <p class="help">
            <Icon name="eye-off" />
            {t('newPassword.shownOnce', { name: person.name })}
          </p>
          <div class="dbtns">
            {shares && (
              <button type="button" class="tbtn dleft" onClick={() => send(made)}>
                {t('newPassword.send')}
              </button>
            )}
            <button type="button" class="btn sm primary" onClick={onClose}>
              {t('common.close')}
            </button>
          </div>
        </>
      ) : (
        <form onSubmit={make}>
          <p class="modal-text">{t('newPassword.lead', { name: person.name })}</p>
          <label class="label" for="np-user">
            {t('signIn.username')}
          </label>
          <input
            id="np-user"
            class="input"
            autoComplete="off"
            autoCapitalize="none"
            spellcheck={false}
            value={username}
            onInput={(e) => setUsername(e.currentTarget.value)}
          />
          {problem ? (
            <p class="help err" role="alert">
              <Icon name="alert" />
              {problem}
            </p>
          ) : (
            <p class="help">{t('password.usernameBad')}</p>
          )}
          <div class="dbtns">
            <button type="button" class="tbtn" onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button type="submit" class="btn sm primary" disabled={busy || !username.trim()}>
              <Icon name="key" />
              {t('newPassword.make')}
            </button>
          </div>
        </form>
      )}
    </Modal>
  );
}
