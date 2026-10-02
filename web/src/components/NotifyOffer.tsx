import { useState } from 'preact/hooks';
import { useI18n } from '../i18n';
import { askToNotify, bigTransfer, mayAskToNotify } from '../notify';
import { Icon } from './Icon';

/** "Tell me when it's done" under a big transfer, while the browser hasn't been asked; after a
 * yes, that it will. */
export function NotifyOffer({ bytes }: { bytes: number }) {
  const { t } = useI18n();
  const [state, setState] = useState<'offer' | 'on' | 'off'>(() => (mayAskToNotify() ? 'offer' : 'off'));
  if (state === 'on') {
    return (
      <p class="help notifyon">
        <Icon name="bell" />
        {t('notify.on')}
      </p>
    );
  }
  if (state === 'off' || bytes < bigTransfer) return null;
  return (
    <button type="button" class="small link notifyask" onClick={async () => setState((await askToNotify()) ? 'on' : 'off')}>
      {t('notify.ask')}
    </button>
  );
}
