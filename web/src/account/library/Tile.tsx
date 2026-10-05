import { useState } from 'preact/hooks';
import type { FileInfo } from '../../api';
import { Icon } from '../../components/Icon';
import { useThumbSrc } from '../../e2ee/hooks';
import { formatDuration } from '../../format';
import { useI18n } from '../../i18n';
import { toneClass } from '../colors';
import { extOf } from '../viewer/view';

/** A file's thumbnail, or until there is one, the app's placeholder: a muted tone for photos and
 * videos, the name for documents. An encrypted file's is decrypted here; while its folder's key
 * isn't open here, a lock shows instead. */
export function Thumb({ file }: { file: FileInfo }) {
  const [broken, setBroken] = useState(false);
  const thumb = useThumbSrc(file);
  if (file.has_thumb && !broken && thumb.src) {
    return <img class="thumb" src={thumb.src} alt="" loading="lazy" decoding="async" draggable={false} onError={() => setBroken(true)} />;
  }
  if (thumb.locked) {
    return (
      <span class={`thumb ph ${toneClass(file.id)}`}>
        <Icon name="lock" class="tico" />
      </span>
    );
  }
  if (file.kind === 'document') {
    return (
      <span class="thumb doc">
        <Icon name="file" class="tico" />
        <span class="fname">{file.name}</span>
      </span>
    );
  }
  return (
    <span class={`thumb ph ${toneClass(file.id)}`}>
      <Icon name={file.kind === 'video' ? 'play' : 'image'} class="tico" />
    </span>
  );
}

interface TileProps {
  file: FileInfo;
  /** Its place in the list, for selecting runs of files. */
  index: number;
  selected: boolean;
  /** Files are being selected: a click picks instead of opening. */
  selecting: boolean;
  onClick: (e: MouseEvent) => void;
  /** The circle in the corner, which shows on hover with a mouse and while selecting. */
  onCircle: () => void;
  /** The save folder's name, when the file is in it. */
  savedInto?: string;
}

/** One file in the library's grid. Selected, it draws back from its neighbours, as in the app. */
export function Tile({ file, index, selected, selecting, onClick, onCircle, savedInto }: TileProps) {
  const { t } = useI18n();
  const ext = file.kind === 'document' ? extOf(file.name) : '';
  const saved = savedInto !== undefined ? t('library.savedInto', { folder: savedInto }) : '';
  return (
    <div class={`ltile${selected ? ' sel' : ''}${selecting ? ' selecting' : ''}`} data-index={index}>
      <button
        type="button"
        class="lopen"
        aria-label={saved ? `${file.name} · ${saved}` : file.name}
        aria-pressed={selecting ? selected : undefined}
        title={saved ? `${file.name}\n${saved}` : file.name}
        onClick={onClick}
        onContextMenu={(e) => selecting && e.preventDefault()}
      >
        <Thumb key={file.updated_at} file={file} />
        {ext && <span class="extb">{ext}</span>}
        {file.kind === 'video' && file.duration_ms !== null && <span class="dur">{formatDuration(file.duration_ms)}</span>}
        {saved && (
          <span class="savedb">
            <Icon name="folder" />
          </span>
        )}
      </button>
      <button
        type="button"
        class={`selm${selected ? ' on' : ''}`}
        aria-label={t('select.file', { name: file.name })}
        aria-pressed={selected}
        tabIndex={-1}
        onClick={onCircle}
      >
        {selected && <Icon name="check" />}
      </button>
    </div>
  );
}
