import { useRef } from 'preact/hooks';
import { Icon } from '../components/Icon';

/** A button that opens the phone's picker for any number of files of any kind. */
export function FilePicker({ label, primary, onFiles }: { label: string; primary?: boolean; onFiles: (files: File[]) => void }) {
  const input = useRef<HTMLInputElement>(null);
  return (
    <>
      <input
        ref={input}
        type="file"
        multiple
        hidden
        onChange={(e) => {
          const files = Array.from(e.currentTarget.files ?? []);
          e.currentTarget.value = ''; // the same files can be picked again later
          if (files.length) onFiles(files);
        }}
      />
      <button type="button" class={`btn ${primary ? 'primary' : 'outline'}`} onClick={() => input.current?.click()}>
        {primary && <Icon name="plus" />}
        {label}
      </button>
    </>
  );
}
