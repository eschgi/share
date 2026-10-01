import { useEffect, useRef } from 'preact/hooks';
import { Icon, type IconName } from '../components/Icon';
import { skipJunk } from '../drop';

interface Props {
  label: string;
  /** The button's look: 'primary' (with a plus), 'outline', or e.g. 'tonal sm'. */
  look?: string;
  /** An icon before the label; primary buttons have a plus. */
  icon?: IconName;
  /** File types to offer first, e.g. "video/mp4"; any kind if empty. */
  accept?: string;
  /** A whole folder instead of files, with what's in its subfolders. Computers only. */
  directory?: boolean;
  onFiles: (files: File[]) => void;
}

/** A button that opens the phone's picker for any number of files, or the computer's for a folder. */
export function FilePicker({ label, look = 'outline', icon = look === 'primary' ? 'plus' : undefined, accept, directory, onFiles }: Props) {
  const input = useRef<HTMLInputElement>(null);
  // An attribute rather than a JSX prop, which the typings don't know. Browsers ask once
  // whether to upload that many files, and leave out accept.
  useEffect(() => {
    input.current?.toggleAttribute('webkitdirectory', !!directory);
  }, [directory]);
  return (
    <>
      <input
        ref={input}
        type="file"
        multiple
        hidden
        accept={accept || undefined}
        onChange={(e) => {
          const files = Array.from(e.currentTarget.files ?? []);
          e.currentTarget.value = ''; // the same files can be picked again later
          const keep = directory ? skipJunk(files) : files;
          if (keep.length) onFiles(keep);
        }}
      />
      <button type="button" class={`btn ${look}`} onClick={() => input.current?.click()}>
        {icon && <Icon name={icon} />}
        {label}
      </button>
    </>
  );
}
