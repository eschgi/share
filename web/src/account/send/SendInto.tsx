import { Icon } from '../../components/Icon';
import { useI18n } from '../../i18n';
import { FolderField, FolderPicker } from '../folders/Folders';
import { chooseSendFolder, useFolders } from '../folders/store';
import { dropPending, sendPending } from './sender';

/** Where sending goes, with a second folder: the one open in the library, or another one
 * (screen 48). */
export function SendInto() {
  const { t } = useI18n();
  const folders = useFolders();
  if (!folders.list) return null;
  return (
    <FolderField label={t('send.into')}>
      <FolderPicker list={folders.list} value={folders.sendTo} onChange={chooseSendFolder} title={t('send.into')} />
    </FolderField>
  );
}

/** Files that wait for the person to say which folder they go into: shared from another app,
 * or dropped where no folder was in view. */
export function PendingSend({ count }: { count: number }) {
  const { t, tn } = useI18n();
  const folders = useFolders();
  const to = folders.sendTo;
  return (
    <div class="sharedcard pending">
      <Icon name="upload" />
      <p>{tn('send.waiting', count)}</p>
      {folders.list && folders.list.length > 0 ? (
        <>
          <FolderPicker list={folders.list} value={to} onChange={chooseSendFolder} title={t('send.into')} />
          <div class="dbtns">
            <button type="button" class="tbtn" onClick={dropPending}>
              {t('share.dontSend')}
            </button>
            <button type="button" class="btn sm primary" disabled={!to} onClick={() => to && sendPending(to.id)}>
              <Icon name="upload" />
              {tn('send.sendWaiting', count)}
            </button>
          </div>
        </>
      ) : (
        <p class="help">{t(folders.list ? 'send.noFolder' : 'common.offline')}</p>
      )}
    </div>
  );
}
