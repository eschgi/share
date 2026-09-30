import type { Session } from '../api';
import { Stack } from '../components/Bits';
import { Brand } from '../components/Brand';
import { Icon } from '../components/Icon';
import { formatWhen } from '../format';
import { useI18n } from '../i18n';
import { FilePicker } from './FilePicker';

/** Screens 2 and 3: ready to send; with a 24-hour PIN it also says until when it works. */
export function ReadyScreen({ name, session, onFiles }: { name: string; session: Session; onFiles: (f: File[]) => void }) {
  const { t, lang } = useI18n();
  const until = session.pin_kind === 'day' && session.expires_at ? new Date(session.expires_at) : null;
  return (
    <main class="screen">
      <Brand name={name} languageSwitch />
      <Stack day={!!until} />
      <h1 class="hero">{t('ready.title')}</h1>
      <p class="lead">{t(until ? 'ready.leadDay' : 'ready.lead')}</p>
      {until && (
        <div class="timepill">
          <Icon name="clock" />
          {t('ready.worksUntil', { when: formatWhen(until, new Date(), lang) })}
        </div>
      )}
      <div class="grow" />
      <FilePicker label={t('ready.choose')} primary onFiles={onFiles} />
      <p class="small">{t(until ? 'ready.private' : 'ready.tip')}</p>
    </main>
  );
}
