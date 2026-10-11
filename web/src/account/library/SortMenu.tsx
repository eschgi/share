// How the library is sorted (screen 125): by the day files were taken or arrived, or by who sent
// them, newest or oldest first. Each browser remembers its choice, for every folder.
import { useEffect, useRef, useState } from 'preact/hooks';
import type { LibraryOrder, LibrarySort } from '../../api';
import { Icon } from '../../components/Icon';
import { useI18n } from '../../i18n';

export interface SortChoice {
  sort: LibrarySort;
  order: LibraryOrder;
}

const storageKey = 'share.librarySort';

/** This browser's choice; by the day files arrived, newest first, until one is made. */
export function storedSort(): SortChoice {
  try {
    const v = JSON.parse(localStorage.getItem(storageKey) ?? 'null') as Partial<SortChoice> | null;
    const sort = v?.sort === 'taken' || v?.sort === 'sender' ? v.sort : 'received';
    return { sort, order: v?.order === 'oldest' ? 'oldest' : 'newest' };
  } catch {
    return { sort: 'received', order: 'newest' };
  }
}

function storeSort(c: SortChoice): void {
  try {
    localStorage.setItem(storageKey, JSON.stringify(c));
  } catch {
    // private mode: the choice holds until the page closes
  }
}

const sorts: { sort: LibrarySort; label: string; help: string }[] = [
  { sort: 'taken', label: 'sort.taken', help: 'sort.takenHelp' },
  { sort: 'received', label: 'sort.received', help: 'sort.receivedHelp' },
  { sort: 'sender', label: 'sort.sender', help: 'sort.senderHelp' },
];

export function SortMenu({ value, onChange }: { value: SortChoice; onChange: (c: SortChoice) => void }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const away = (e: Event) => {
      if (!box.current?.contains(e.target as Node)) setOpen(false);
    };
    const escape = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false);
    document.addEventListener('pointerdown', away);
    document.addEventListener('keydown', escape);
    return () => {
      document.removeEventListener('pointerdown', away);
      document.removeEventListener('keydown', escape);
    };
  }, [open]);
  const pick = (c: SortChoice) => {
    storeSort(c);
    setOpen(false);
    onChange(c);
  };
  const current = sorts.find((s) => s.sort === value.sort) ?? sorts[1];
  const row = (on: boolean, label: string, help: string | null, choose: () => void) => (
    <button key={label} type="button" role="menuitemradio" aria-checked={on} class={`srow${on ? ' on' : ''}`} onClick={choose}>
      <span class={`rad${on ? ' on' : ''}`} aria-hidden="true" />
      <span class="rt">
        <b>{t(label)}</b>
        {help && <span>{t(help)}</span>}
      </span>
    </button>
  );
  return (
    <div class="sortbox" ref={box}>
      <button type="button" class="sortsel" aria-haspopup="menu" aria-expanded={open} aria-label={t('sort.title')} onClick={() => setOpen(!open)}>
        <Icon name="sort" />
        <span class="sortname">{t(current.label)}</span>
      </button>
      {open && (
        <div class="sortmenu" role="menu" aria-label={t('sort.title')}>
          {sorts.map((s) => row(value.sort === s.sort, s.label, s.help, () => pick({ ...value, sort: s.sort })))}
          <hr />
          {row(value.order === 'newest', 'sort.newest', null, () => pick({ ...value, order: 'newest' }))}
          {row(value.order === 'oldest', 'sort.oldest', null, () => pick({ ...value, order: 'oldest' }))}
        </div>
      )}
    </div>
  );
}
