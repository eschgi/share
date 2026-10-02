import { useI18n } from '../../i18n';
import { Shell, TitleBar } from '../Shell';

/** Screen 29: sending without a PIN. */
export function SendTab() {
  const { t } = useI18n();
  return (
    <Shell tab="send">
      <TitleBar title={t('nav.send')} />
      <div class="send">
        <p class="lead">{t('send.lead')}</p>
      </div>
    </Shell>
  );
}
