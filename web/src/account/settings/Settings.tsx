import { useState } from 'preact/hooks';
import { ApiError, deleteMe, logout } from '../../api';
import { Icon } from '../../components/Icon';
import { setSignedInHint } from '../../hint';
import { useI18n } from '../../i18n';
import { storedTheme, type ThemeChoice } from '../../theme';
import { Avatar, RoleBadge, Row } from '../components/Bits';
import { Confirm } from '../components/Modal';
import { useAccount } from '../context';
import { Shell, TitleBar } from '../Shell';
import { DevicesDialog, LanguageDialog, PasswordDialog, ThemeDialog, themeSummary } from './dialogs';

type Dialog = 'devices' | 'language' | 'theme' | 'password' | 'signOut' | 'delete';

/** Screens 30 and 37: the person, how the website looks, and leaving. */
export function Settings() {
  const { t, lang } = useI18n();
  const { me, languageAuto } = useAccount();
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
    location.replace('/');
  };

  const language = t(`lang.${lang}`);
  return (
    <Shell tab="settings">
      <TitleBar title={t('nav.settings')} />
      <div class="settings">
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
        <div class="group">
          <Row
            icon="globe"
            title={t('settings.language')}
            sub={languageAuto ? t('settings.languageAuto', { language }) : language}
            onClick={() => setDialog('language')}
          />
          <Row icon="palette" title={t('settings.theme')} sub={themeSummary(t, theme)} onClick={() => setDialog('theme')} />
        </div>
        <div class="group">
          <Row
            icon="lock"
            title={t('settings.password')}
            sub={me.user.has_password ? t('settings.passwordSet', { username: me.user.username ?? '' }) : t('settings.passwordNone')}
            onClick={() => setDialog('password')}
          />
        </div>
        <div class="group">
          <Row icon="log-out" title={t('settings.signOut')} chevron={false} onClick={() => setDialog('signOut')} />
          <Row icon="user-x" title={t('settings.deleteAccount')} chevron={false} tone="danger" onClick={() => setDialog('delete')} />
        </div>
      </div>
      {dialog === 'devices' && <DevicesDialog onClose={close} />}
      {dialog === 'language' && <LanguageDialog onClose={close} />}
      {dialog === 'theme' && <ThemeDialog choice={theme} onChoose={setTheme} onClose={close} />}
      {dialog === 'password' && <PasswordDialog onClose={close} />}
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
    </Shell>
  );
}
