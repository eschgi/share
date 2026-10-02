import { Stack } from '../components/Bits';
import { Icon } from '../components/Icon';
import { Page } from '../components/Page';
import { touchFirst, useMedia } from '../device';
import { formatWhen } from '../format';
import { useI18n } from '../i18n';
import type { Sender } from '../state';
import { FilePicker } from './FilePicker';

interface Props {
  name: string;
  session: Sender;
  onFiles: (f: File[]) => void;
  /** A share didn't bring its files along: they have to be picked here. */
  shareFailed?: boolean;
  /** With a PIN: stops using it, to enter another one. */
  onForgetPin?: () => void;
}

/** Screens 2, 3 and 29: ready to send; with a 24-hour PIN it also says until when it works, and
 * signed in, that no PIN is needed. */
export function ReadyScreen({ name, session, onFiles, shareFailed, onForgetPin }: Props) {
  const { t, lang } = useI18n();
  const until = session.kind === 'pin' && session.pin_kind === 'day' && session.expires_at ? new Date(session.expires_at) : null;
  const computer = !useMedia(touchFirst);
  return (
    <Page name={name} languageSwitch layout="split">
      <div class="pane">
        <Stack day={!!until} />
        <h1 class="hero">{t('ready.title')}</h1>
        <p class="lead">{t(session.kind === 'account' ? 'send.lead' : until ? 'ready.leadDay' : 'ready.lead')}</p>
        {until && (
          <div class="timepill">
            <Icon name="clock" />
            {t('ready.worksUntil', { when: formatWhen(until, new Date(), lang) })}
          </div>
        )}
      </div>
      <div class="grow" />
      <div class="pane">
        {shareFailed && (
          <p class="help sharefail">
            <Icon name="alert" />
            {t('share.failed')}
          </p>
        )}
        {/* On computers a drop area; its picture and words only show there. */}
        <div class="drop">
          <span class="roundico">
            <Icon name="upload" />
          </span>
          {computer && <p class="drop-t">{t('drop.title')}</p>}
          {computer && <p class="drop-or">{t('drop.or')}</p>}
          <FilePicker label={t('ready.choose')} look="primary" onFiles={onFiles} />
          {computer && <FilePicker label={t('ready.chooseFolder')} look="link" icon="folder" directory onFiles={onFiles} />}
        </div>
        <p class="small">{t(until ? 'ready.private' : 'ready.tip')}</p>
        {session.kind === 'pin' && onForgetPin && (
          <button type="button" class="small link" onClick={onForgetPin}>
            {t('pin.useAnother')}
          </button>
        )}
      </div>
    </Page>
  );
}
