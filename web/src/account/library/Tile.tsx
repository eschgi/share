import { useState } from 'preact/hooks';
import { thumbUrl, type FileInfo } from '../../api';
import { Icon } from '../../components/Icon';
import { formatDuration } from '../../format';
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

/** One file in the library's grid. */
export function Tile({ file, onOpen }: { file: FileInfo; onOpen: () => void }) {
  const ext = file.kind === 'document' ? extOf(file.name) : '';
  return (
    <button type="button" class="ltile" aria-label={file.name} title={file.name} onClick={onOpen}>
      <Thumb file={file} />
      {ext && <span class="extb">{ext}</span>}
      {file.kind === 'video' && file.duration_ms !== null && <span class="dur">{formatDuration(file.duration_ms)}</span>}
    </button>
  );
}
