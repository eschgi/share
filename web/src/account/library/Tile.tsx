import { useState } from 'preact/hooks';
import { thumbUrl, type FileInfo } from '../../api';
import { Icon } from '../../components/Icon';
import { formatDuration } from '../../format';
import { useI18n } from '../../i18n';
import { toneClass } from '../colors';
import { extOf } from '../viewer/view';

/** A file's thumbnail, or until there is one, the app's placeholder: a muted tone for photos and
 * videos, the name for documents. */
export function Thumb({ file }: { file: FileInfo }) {
  const [broken, setBroken] = useState(false);
  if (file.has_thumb && !broken) {
    return <img class="thumb" src={thumbUrl(file)} alt="" loading="lazy" decoding="async" draggable={false} onError={() => setBroken(true)} />;
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
}

/** One file in the library's grid. Selected, it draws back from its neighbours, as in the app. */
export function Tile({ file, index, selected, selecting, onClick, onCircle }: TileProps) {
  const { t } = useI18n();
  const ext = file.kind === 'document' ? extOf(file.name) : '';
  return (
    <div class={`ltile${selected ? ' sel' : ''}${selecting ? ' selecting' : ''}`} data-index={index}>
      <button
        type="button"
        class="lopen"
        aria-label={file.name}
        aria-pressed={selecting ? selected : undefined}
        title={file.name}
        onClick={onClick}
        onContextMenu={(e) => selecting && e.preventDefault()}
      >
        <Thumb key={file.updated_at} file={file} />
        {ext && <span class="extb">{ext}</span>}
        {file.kind === 'video' && file.duration_ms !== null && <span class="dur">{formatDuration(file.duration_ms)}</span>}
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
