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
  /** What the ZIP would be called; null where they download one by one instead (a bucket). */
  zipName: string | null;
  /** Downloads them as a ZIP, or one by one. */
  onOther: () => void;
  onFolder: (dir: FileSystemDirectoryHandle) => void;
  onClose: () => void;
}

/** Screen 26: several files go into a folder (a folder for each day), or into one ZIP, or with
 * the files in a bucket one by one. The choice and the folder are kept for next time. */
export function SaveChoice({ count, bytes, zipName, onOther, onFolder, onClose }: Props) {
  const { t, lang } = useI18n();
  const other = zipName === null ? 'each' : 'zip';
  const [into, setInto] = useState<'folder' | 'zip' | 'each'>(() => {
    const kept = localStorage.getItem(remembered);
    return kept === 'zip' || kept === 'each' ? other : 'folder';
  });
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
      setProblem(t(other === 'each' ? 'save.cantPickEach' : 'save.cantPick'));
    }
  };

  // In the click: the browser asks for the folder, or for the right to write there, only then.
  const go = async () => {
    try {
      localStorage.setItem(remembered, into);
    } catch {
      // not kept, then
    }
    if (into !== 'folder') return onOther();
    try {
      let target = dir && (await mayWrite(dir)) ? dir : null;
      target ??= await pickFolder();
      if (target) onFolder(target);
    } catch {
      setProblem(t(other === 'each' ? 'save.cantPickEach' : 'save.cantPick'));
    }
  };

  const option = (value: 'folder' | 'zip' | 'each', icon: 'folder' | 'archive' | 'download', title: string, detail: string) => (
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
        {zipName === null
          ? option('each', 'download', t('save.each'), t('save.eachDetail'))
          : option('zip', 'archive', t('save.zip'), t('save.zipDetail', { name: zipName }))}
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
