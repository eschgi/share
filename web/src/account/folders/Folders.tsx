import '../save/save.css';
import './folders.css';
import type { ComponentChildren } from 'preact';
import { useState } from 'preact/hooks';
import type { FileInfo, FolderInfo } from '../../api';
import { useThumbSrc } from '../../e2ee/hooks';
import { Icon } from '../../components/Icon';
import { formatBytes, formatCount } from '../../format';
import { useI18n } from '../../i18n';
import { toneClass } from '../colors';
import { Modal } from '../components/Modal';
import { allTotals } from './model';
import { showFolder } from './store';

/** A folder's name, with a lock while its new files are encrypted. */
export function FolderName({ folder }: { folder: FolderInfo }) {
  const { t } = useI18n();
  return (
    <b>
      {folder.name}
      {folder.encrypted && (
        <span class="flock" role="img" aria-label={t('encryption.encrypted')} title={t('encryption.encrypted')}>
          <Icon name="lock" />
        </span>
      )}
    </b>
  );
}

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
  if (c?.has_thumb) return <CoverThumb file={c} size={size} />;
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

/** "2,340 files · 41 GB", "2,340 files", "4 people" */
export function useFolderLines() {
  const { t, tn, lang } = useI18n();
  const count = (files: number) => tn('folders.count', files, { n: formatCount(files, lang) });
  const seen = (f: FolderInfo) => (f.admins_only ? t('folders.onlyAdmins') : tn('folders.people', f.people, { n: formatCount(f.people, lang) }));
  return {
    holds: (files: number, bytes: number) => tn('folders.holds', files, { n: formatCount(files, lang), size: formatBytes(bytes, lang) }),
    count,
    seen,
    /** What a folder holds and who sees it: "2,340 files · 4 people", "37 files · only admins". */
    about: (f: FolderInfo) => `${count(f.files)} · ${f.admins_only ? t('folders.onlyAdminsLine') : seen(f)}`,
  };
}

/** The folders to choose one from, as cards: each with what it holds and who sees it. note
 * adds a word to a card, such as "Here now"; those are greyed out. */
export function FolderChoices({
  list,
  value,
  onChoose,
  label,
  note,
}: {
  list: FolderInfo[];
  value: string | null;
  onChoose: (id: string) => void;
  label: string;
  note?: (f: FolderInfo) => string | undefined;
}) {
  const lines = useFolderLines();
  return (
    <div role="radiogroup" aria-label={label} class="fchoices">
      {list.map((f) => {
        const extra = note?.(f);
        return (
          <label key={f.id} class={`dopt${value === f.id ? ' on' : ''}${extra ? ' dis' : ''}`}>
            <input type="radio" name="folder" class="sr-only" checked={value === f.id} disabled={!!extra} onChange={() => onChoose(f.id)} />
            <FolderCover folder={f} />
            <span class="dt">
              <FolderName folder={f} />
              <span>{extra ? `${extra} · ${lines.seen(f)}` : lines.about(f)}</span>
            </span>
            <i class="radio" />
          </label>
        );
      })}
    </div>
  );
}

/** A folder to send into, as a field: it opens the choice (screens 43 and 48). */
export function FolderPicker({ list, value, onChange, title }: { list: FolderInfo[]; value: FolderInfo | null; onChange: (id: string) => void; title: string }) {
  const lines = useFolderLines();
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" class="fsel" aria-haspopup="dialog" onClick={() => setOpen(true)}>
        {value && <FolderCover folder={value} />}
        <span class="rt">
          <b>{value?.name ?? title}</b>
          {value && <span>{lines.about(value)}</span>}
        </span>
        <Icon name="chev" class="down" />
      </button>
      {open && (
        <Modal title={title} onClose={() => setOpen(false)}>
          <FolderChoices
            list={list}
            value={value?.id ?? null}
            label={title}
            onChoose={(id) => {
              onChange(id);
              setOpen(false);
            }}
          />
        </Modal>
      )}
    </>
  );
}

/** A labelled part of a form, such as "Into the folder" over its picker. */
export function FolderField({ label, children }: { label: string; children: ComponentChildren }) {
  return (
    <div class="ffield">
      <p class="flabel">{label}</p>
      {children}
    </div>
  );
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
        {cover === 'all' ? <b>{name}</b> : <FolderName folder={cover} />}
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
          {cover === 'all' ? <b>{name}</b> : <FolderName folder={cover} />}
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

/** A folder's cover from its newest photo or video, decrypted if it is encrypted. */
function CoverThumb({ file, size }: { file: FileInfo; size: string }) {
  const thumb = useThumbSrc(file);
  if (!thumb.src) {
    return (
      <span class={`fcov ph ${toneClass(file.id)}${size}`}>
        <Icon name={thumb.locked ? 'lock' : 'image'} />
      </span>
    );
  }
  return (
    <span class={`fcov${size}`}>
      <img src={thumb.src} alt="" loading="lazy" decoding="async" draggable={false} />
    </span>
  );
}
