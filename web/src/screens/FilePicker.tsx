import { useRef } from 'preact/hooks';
import { Icon } from '../components/Icon';

interface Props {
  label: string;
  /** The button's look: 'primary' (with a plus), 'outline', or e.g. 'tonal sm'. */
  look?: string;
  /** File types to offer first, e.g. "video/mp4"; any kind if empty. */
  accept?: string;
  onFiles: (files: File[]) => void;
}

/** A button that opens the phone's picker for any number of files. */
export function FilePicker({ label, look = 'outline', accept, onFiles }: Props) {
  const input = useRef<HTMLInputElement>(null);
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
          if (files.length) onFiles(files);
        }}
      />
      <button type="button" class={`btn ${look}`} onClick={() => input.current?.click()}>
        {look === 'primary' && <Icon name="plus" />}
        {label}
      </button>
    </>
  );
}
