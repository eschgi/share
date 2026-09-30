import { Brand } from '../components/Brand';
import { Icon } from '../components/Icon';
import { formatBytes, formatCount } from '../format';
import { useI18n } from '../i18n';

/** Screen 6: a clear end, with totals. */
export function DoneScreen({ name, files, bytes, onMore }: { name: string; files: number; bytes: number; onMore: () => void }) {
  const { t, tn, lang } = useI18n();
  return (
    <main class="screen">
      <Brand name={name} />
      <div class="donecircle">
        <Icon name="check" />
      </div>
      <h1 class="hero center">{t('done.title')}</h1>
      <p class="lead center">{t('done.lead')}</p>
      <div class="stats">
        <div>
          <b>{formatCount(files, lang)}</b>
          <span>{tn('done.files', files)}</span>
        </div>
        <div>
          <b>{formatBytes(bytes, lang)}</b>
          <span>{t('done.total')}</span>
        </div>
      </div>
      <div class="grow" />
      <button type="button" class="btn primary" onClick={onMore}>
        {t('done.more')}
      </button>
      <p class="small">{t('done.close')}</p>
    </main>
  );
}
