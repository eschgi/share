import { useState } from 'preact/hooks';
import type { FolderInfo } from '../../api';
import { Icon } from '../../components/Icon';
import { useI18n } from '../../i18n';
import { Modal } from '../components/Modal';
import { moveDefault } from './folders';
import { FolderChoices } from './Folders';

/** Screen 49: the folder selected files go into. here is the folder they are all in already,
 * where the page can tell. */
export function MoveDialog({
  count,
  list,
  here,
  busy,
  problem,
  onMove,
  onClose,
}: {
  count: number;
  list: FolderInfo[];
  here: string | null;
  busy: boolean;
  problem: string | null;
  onMove: (folder: FolderInfo) => void;
  onClose: () => void;
}) {
  const { t, tn } = useI18n();
  const [to, setTo] = useState(() => moveDefault(list, here));
  const target = list.find((f) => f.id === to);
  return (
    <Modal title={tn('move.title', count)} onClose={onClose}>
      <p class="modal-text">{t('move.lead')}</p>
      <div class="mt12">
        <FolderChoices list={list} value={to} onChoose={setTo} label={tn('move.title', count)} note={(f) => (f.id === here ? t('move.hereNow') : undefined)} />
      </div>
      {problem && (
        <p class="help err" role="alert">
          <Icon name="alert" />
          {problem}
        </p>
      )}
      <div class="dbtns">
        <button type="button" class="tbtn" onClick={onClose}>
          {t('common.cancel')}
        </button>
        <button type="button" class="btn sm primary" disabled={busy || !target} onClick={() => target && onMove(target)}>
          <Icon name="folder" />
          {tn('move.button', count)}
        </button>
      </div>
    </Modal>
  );
}
