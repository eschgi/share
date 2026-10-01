import { getDroppedFiles } from '@uppy/core/utils';
import { useEffect, useRef, useState } from 'preact/hooks';
import { isFileDrag, skipJunk } from '../drop';
import { Icon } from './Icon';

interface Props {
  /** Where dropped files go on this screen; null where none are taken. */
  onFiles: ((files: File[]) => void) | null;
  /** What dropping does here, e.g. "Drop to add them". */
  label: string;
}

/**
 * Files dragged anywhere over the page. Mounted once per page: without it, a file dropped by
 * mistake makes the browser open it instead of the page, also in the middle of sending.
 */
export function DropZone({ onFiles, label }: Props) {
  const [shown, setShown] = useState(false);
  const take = useRef(onFiles);
  take.current = onFiles;
  const hideTimer = useRef(0);

  const hide = () => {
    clearTimeout(hideTimer.current);
    setShown(false);
  };

  useEffect(() => {
    const over = (e: DragEvent) => {
      if (!isFileDrag(e.dataTransfer?.types)) return;
      e.preventDefault();
      e.dataTransfer!.dropEffect = take.current ? 'copy' : 'none';
      if (!take.current) return;
      setShown(true);
      // dragleave doesn't always come (Escape, a drag that ends elsewhere), but dragover keeps
      // coming every few hundred milliseconds while the drag is over the page.
      clearTimeout(hideTimer.current);
      hideTimer.current = window.setTimeout(hide, 1000);
    };
    const drop = (e: DragEvent) => {
      if (!isFileDrag(e.dataTransfer?.types)) return;
      e.preventDefault();
      hide();
      const to = take.current;
      // Read now: the browser empties the DataTransfer once the event is over.
      if (to && e.dataTransfer) void getDroppedFiles(e.dataTransfer).then((files) => to(skipJunk(files)));
    };
    addEventListener('dragenter', over);
    addEventListener('dragover', over);
    addEventListener('drop', drop);
    addEventListener('blur', hide);
    return () => {
      removeEventListener('dragenter', over);
      removeEventListener('dragover', over);
      removeEventListener('drop', drop);
      removeEventListener('blur', hide);
      clearTimeout(hideTimer.current);
    };
  }, []);

  const takes = !!onFiles;
  useEffect(() => {
    if (!takes) hide();
  }, [takes]);

  if (!shown) return null;
  // The overlay covers the window and its contents ignore the pointer, so leaving the window is
  // one dragleave here rather than one for every element passed on the way.
  return (
    <div class="dropov" onDragLeave={(e) => e.target === e.currentTarget && hide()}>
      <div>
        <span class="roundico">
          <Icon name="upload" />
        </span>
        <b>{label}</b>
      </div>
    </div>
  );
}
