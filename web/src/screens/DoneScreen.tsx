import { Icon } from '../components/Icon';
import { Page } from '../components/Page';
import { touchFirst, useInstall, useMedia } from '../device';
import { formatBytes, formatCount } from '../format';
import { useI18n } from '../i18n';

interface Props {
  name: string;
  files: number;
  bytes: number;
  /** Offer to install the site as an app: only with a permanent PIN, which lasts. */
  offerInstall: boolean;
  onMore: () => void;
  /** With a PIN: stops using it, to enter another one. */
  onForgetPin?: () => void;
}

/** Screen 6: a clear end, with totals. */
export function DoneScreen({ name, files, bytes, offerInstall, onMore, onForgetPin }: Props) {
  const { t, tn, lang } = useI18n();
  const install = useInstall();
  // A computer installs it as an app in its own window, a phone puts it on the home screen.
  const computer = !useMedia(touchFirst);
  return (
    <Page name={name}>
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
            <b>{t(computer ? 'install.titleDesktop' : 'install.title', { name })}</b>
            <span>{t(computer ? 'install.leadDesktop' : 'install.lead')}</span>
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
      {onForgetPin && (
        <button type="button" class="small link" onClick={onForgetPin}>
          {t('pin.useAnother')}
        </button>
      )}
    </Page>
  );
}
