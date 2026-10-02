import { useEffect, useState } from 'preact/hooks';
import { Icon } from '../../components/Icon';
import { formatBytes } from '../../format';
import { useI18n } from '../../i18n';
import { Modal } from '../components/Modal';
import { keptFolder, mayWrite, pickFolder } from './folder';

const remembered = 'share.saveInto';

interface Props {
  count: number;
  bytes: number;
  /** What the ZIP would be called. */
  zipName: string;
  onZip: () => void;
  onFolder: (dir: FileSystemDirectoryHandle) => void;
  onClose: () => void;
}

/** Screen 26: several files go into a folder (a folder for each day), or into one ZIP. The choice
 * and the folder are kept for next time. */
export function SaveChoice({ count, bytes, zipName, onZip, onFolder, onClose }: Props) {
  const { t, lang } = useI18n();
  const [into, setInto] = useState<'folder' | 'zip'>(() => (localStorage.getItem(remembered) === 'zip' ? 'zip' : 'folder'));
  const [dir, setDir] = useState<FileSystemDirectoryHandle | null>(null);
  const [problem, setProblem] = useState<string | null>(null);

  useEffect(() => {
    void keptFolder().then(setDir);
  }, []);

  const another = async () => {
    try {
      const d = await pickFolder();
      if (d) {
        setDir(d);
        setInto('folder');
      }
    } catch {
      setProblem(t('save.cantPick'));
    }
  };

  // In the click: the browser asks for the folder, or for the right to write there, only then.
  const go = async () => {
    try {
      localStorage.setItem(remembered, into);
    } catch {
      // not kept, then
    }
    if (into === 'zip') return onZip();
    try {
      let target = dir && (await mayWrite(dir)) ? dir : null;
      target ??= await pickFolder();
      if (target) onFolder(target);
    } catch {
      setProblem(t('save.cantPick'));
    }
  };

  const option = (value: 'folder' | 'zip', icon: 'folder' | 'archive', title: string, detail: string) => (
    <label class={`dopt${into === value ? ' on' : ''}`}>
      <input type="radio" name="save-into" class="sr-only" checked={into === value} onChange={() => setInto(value)} />
      <span class={`ri${into === value ? ' acc' : ''}`}>
        <Icon name={icon} />
      </span>
      <span class="dt">
        <b>{title}</b>
        <span>{detail}</span>
      </span>
      <i class="radio" />
    </label>
  );

  return (
    <Modal title={t('save.title', { n: count, size: formatBytes(bytes, lang) })} onClose={onClose}>
      <div role="radiogroup" aria-label={t('save.title', { n: count, size: formatBytes(bytes, lang) })}>
        {option('folder', 'folder', t('save.folder'), dir ? t('save.folderDetail', { folder: dir.name }) : t('save.folderNew'))}
        {option('zip', 'archive', t('save.zip'), t('save.zipDetail', { name: zipName }))}
      </div>
      {problem && (
        <p class="help err" role="alert">
          <Icon name="alert" />
          {problem}
        </p>
      )}
      <div class="dbtns">
        {into === 'folder' && dir && (
          <button type="button" class="tbtn dleft" onClick={() => void another()}>
            {t('save.another')}
          </button>
        )}
        <button type="button" class="tbtn" onClick={onClose}>
          {t('common.cancel')}
        </button>
        <button type="button" class="btn sm primary" onClick={() => void go()}>
          <Icon name="download" />
          {t('viewer.download')}
        </button>
      </div>
    </Modal>
  );
}
