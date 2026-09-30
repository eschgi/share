import { Brand } from '../components/Brand';
import { Icon } from '../components/Icon';
import { useInstall } from '../device';
import { formatBytes, formatCount } from '../format';
import { useI18n } from '../i18n';

interface Props {
  name: string;
  files: number;
  bytes: number;
  /** Offer to install the site as an app: only with a permanent PIN, which lasts. */
  offerInstall: boolean;
  onMore: () => void;
}

/** Screen 6: a clear end, with totals. */
export function DoneScreen({ name, files, bytes, offerInstall, onMore }: Props) {
  const { t, tn, lang } = useI18n();
  const install = useInstall();
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
      {offerInstall && install && (
        <div class="install">
          <div class="appico">
            <Icon name="images" />
          </div>
          <div class="it">
            <b>{t('install.title', { name })}</b>
            <span>{t('install.lead')}</span>
            <button type="button" class="btn tonal xs" onClick={() => void install()}>
              {t('install.button')}
            </button>
          </div>
        </div>
      )}
      <div class="grow" />
      <button type="button" class="btn primary" onClick={onMore}>
        {t('done.more')}
      </button>
      <p class="small">{t('done.close')}</p>
    </main>
  );
}
