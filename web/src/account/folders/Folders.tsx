import './folders.css';
import { thumbUrl, type FolderInfo } from '../../api';
import { Icon } from '../../components/Icon';
import { formatBytes, formatCount } from '../../format';
import { useI18n } from '../../i18n';
import { toneClass } from '../colors';
import { Modal } from '../components/Modal';
import { allTotals } from './folders';
import { showFolder } from './store';

/** A folder's picture: its newest photo or video, a document for one without pictures, and the
 * library's for all folders. */
export function FolderCover({ folder, large }: { folder: FolderInfo | 'all'; large?: boolean }) {
  const size = large ? ' lg' : '';
  if (folder === 'all') {
    return (
      <span class={`fcov all${size}`}>
        <Icon name="images" />
      </span>
    );
  }
  const c = folder.cover;
  if (c?.has_thumb) {
    return (
      <span class={`fcov${size}`}>
        <img src={thumbUrl(c)} alt="" loading="lazy" decoding="async" draggable={false} />
      </span>
    );
  }
  if (c) {
    return (
      <span class={`fcov ph ${toneClass(c.id)}${size}`}>
        <Icon name="image" />
      </span>
    );
  }
  return (
    <span class={`fcov doc${size}`}>
      <Icon name="file" />
    </span>
  );
}

/** "2,340 files · 41 GB" */
export function useFolderLines() {
  const { tn, lang } = useI18n();
  return {
    holds: (files: number, bytes: number) => tn('folders.holds', files, { n: formatCount(files, lang), size: formatBytes(bytes, lang) }),
    count: (files: number) => tn('folders.count', files, { n: formatCount(files, lang) }),
  };
}

/** The library's title while there is a folder to choose: the folder shown, which opens the
 * choice (screen 38). */
export function FolderTitle({ shown, onOpen }: { shown: FolderInfo | null; onOpen: () => void }) {
  const { t } = useI18n();
  const name = shown?.name ?? t('folders.all');
  return (
    <button type="button" class="fpick" aria-haspopup="dialog" title={t('folders.switch')} onClick={onOpen}>
      <span class="t">{name}</span>
      <Icon name="chev" class="down" />
    </button>
  );
}

/** Choosing the folder the library shows, on a phone or a tablet (screen 39). */
export function FolderSheet({ list, shown, onClose }: { list: FolderInfo[]; shown: FolderInfo | null; onClose: () => void }) {
  const { t } = useI18n();
  const lines = useFolderLines();
  const all = allTotals(list);
  const choose = (id: string | null) => {
    showFolder(id);
    onClose();
  };
  const row = (id: string | null, cover: FolderInfo | 'all', name: string, line: string) => (
    <button key={id ?? 'all'} type="button" class="row" aria-pressed={(shown?.id ?? null) === id} onClick={() => choose(id)}>
      <FolderCover folder={cover} />
      <span class="rt">
        <b>{name}</b>
        <span>{line}</span>
      </span>
      {(shown?.id ?? null) === id && <Icon name="check" class="tick" />}
    </button>
  );
  return (
    <Modal title={t('folders.title')} onClose={onClose}>
      <div class="group">
        {row(null, 'all', t('folders.all'), lines.holds(all.files, all.bytes))}
        {list.map((f) => row(f.id, f, f.name, lines.holds(f.files, f.bytes)))}
      </div>
    </Modal>
  );
}

/** The folders beside the library on a computer (screen 44). */
export function FolderColumn({ list, shown }: { list: FolderInfo[]; shown: FolderInfo | null }) {
  const { t } = useI18n();
  const lines = useFolderLines();
  const all = allTotals(list);
  const row = (id: string | null, cover: FolderInfo | 'all', name: string, files: number) => {
    const on = (shown?.id ?? null) === id;
    return (
      <button key={id ?? 'all'} type="button" class={`frow${on ? ' on' : ''}`} aria-current={on ? 'true' : undefined} onClick={() => showFolder(id)}>
        <FolderCover folder={cover} />
        <span>
          <b>{name}</b>
          <span>{lines.count(files)}</span>
        </span>
      </button>
    );
  };
  return (
    <nav class="fside" aria-label={t('folders.title')}>
      <p class="glabel">{t('folders.title')}</p>
      {row(null, 'all', t('folders.all'), all.files)}
      {list.map((f) => row(f.id, f, f.name, f.files))}
    </nav>
  );
}
