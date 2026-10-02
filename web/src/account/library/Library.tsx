import { useI18n } from '../../i18n';
import { Shell, TitleBar } from '../Shell';

/** Screen 24: the library by day. */
export function Library() {
  const { t } = useI18n();
  return (
    <Shell tab="library">
      <TitleBar title={t('nav.library')} />
      <div class="lib">
        <p class="empty">{t('library.empty')}</p>
      </div>
    </Shell>
  );
}
