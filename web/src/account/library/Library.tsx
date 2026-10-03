import './library.css';
import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import {
  ApiError,
  createDownload,
  getFile,
  getFileIds,
  getFiles,
  getLibrary,
  zipUrl,
  type FileInfo,
  type FileKind,
  type FolderInfo,
  type LibraryDay,
  type LibraryFilter,
} from '../../api';
import { Icon } from '../../components/Icon';
import { useMedia } from '../../device';
import { formatBytes, formatDay } from '../../format';
import { useI18n } from '../../i18n';
import { useOverlay } from '../../router';
import { Avatar, Link } from '../components/Bits';
import { Confirm } from '../components/Modal';
import { useAccount } from '../context';
import { hasChoices } from '../folders/folders';
import { FolderColumn, FolderSheet, FolderTitle } from '../folders/Folders';
import { MoveDialog } from '../folders/MoveDialog';
import { folderGone, refreshFolders, useFolders } from '../folders/store';
import { canSaveToFolder } from '../save/folder';
import { useSavedMarks } from '../save/marks';
import { SaveChoice } from '../save/SaveChoice';
import { startSave } from '../save/store';
import { Shell, TitleBar } from '../Shell';
import { Viewer } from '../viewer/Viewer';
import {
  canShareFiles,
  deleteMany,
  download,
  filesToShare,
  knownTrashDays,
  learnTrashDays,
  moveMany,
  restoreMany,
  shareLimit,
  startDownload,
  zipLimit,
} from './actions';
import { LibraryModel, sameFilter } from './model';
import {
  dayCount,
  dayState,
  isEmpty,
  isSelected,
  loadedSelected,
  noSelection,
  selectAll,
  selectedIds,
  setDay,
  setFiles,
  totals,
  type Selection,
} from './selection';
import { Tile } from './Tile';

const kinds: { kind: FileKind | null; label: string }[] = [
  { kind: null, label: 'library.all' },
  { kind: 'photo', label: 'library.photos' },
  { kind: 'video', label: 'library.videos' },
  { kind: 'document', label: 'library.documents' },
];

/** How long a finger stays on a tile before it starts selecting, as a long press. */
const longPress = 450;

/** What gets deleted: the selection, or the file open in the viewer. */
type Deleting = { kind: 'selection'; count: number } | { kind: 'file'; file: FileInfo };

/** Screens 11, 12, 24 and 25: everything that was sent, newest day first, and selecting many. */
export function Library() {
  const { t, tn, lang } = useI18n();
  const { info, me, toast } = useAccount();
  const admin = me.user.role === 'admin';
  const folders = useFolders();
  const choices = hasChoices(folders.list);
  const shownId = folders.shown?.id ?? null;
  const model = useMemo(() => {
    // A folder the person doesn't see any more answers 404: then all folders.
    const gone = (f: LibraryFilter) => (e: unknown) => {
      if (f.folder && e instanceof ApiError && e.status === 404) folderGone(f.folder);
      throw e;
    };
    return new LibraryModel({
      overview: (f) => getLibrary(f).catch(gone(f)),
      page: (f, cursor, limit) => getFiles(f, cursor, limit).catch(gone(f)),
    });
  }, []);
  const [, redraw] = useState(0);
  const [picking, setPicking] = useState(false);
  const [moving, setMoving] = useState(false);
  const [moveProblem, setMoveProblem] = useState<string | null>(null);
  const [query, setQuery] = useState('');
  const [searching, setSearching] = useState(false);
  const [viewing, setViewing] = useState<string | null>(null);
  const [sel, setSel] = useState<Selection>(noSelection);
  const [anchor, setAnchor] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [deleting, setDeleting] = useState<Deleting | null>(null);
  const [deleteProblem, setDeleteProblem] = useState<string | null>(null);
  const [choosing, setChoosing] = useState(false);
  const saved = useSavedMarks();
  const touch = useMedia('(pointer: coarse)');
  const wide = useMedia('(min-width: 1024px) and (min-height: 540px)');
  const column = choices && wide;
  const shareable = useMemo(canShareFiles, []);
  const lib = useRef<HTMLDivElement>(null);
  const end = useRef<HTMLDivElement>(null);
  const suppressClick = useRef(0);

  const days: LibraryDay[] = model.overview?.days ?? [];
  const selecting = !isEmpty(sel);
  const picked = totals(sel, days);
  // What the listeners below need of the latest render.
  const live = useRef({ sel, files: model.files, days, selecting, admin });
  live.current = { sel, files: model.files, days, selecting, admin };

  const clear = () => {
    setSel(noSelection);
    setAnchor(null);
  };
  // A selection takes a step in the history, so Back ends it, as on a phone.
  useOverlay(selecting, clear);

  useEffect(() => model.subscribe(() => redraw((n) => n + 1)), []);
  // The first page waits for the folders, to show the folder that was chosen last time.
  const ready = folders.list !== null || folders.failed;
  useEffect(() => {
    if (!ready) return;
    if (!model.overview && !model.loading) {
      model.filter = { ...model.filter, folder: shownId };
      void model.reload();
    } else {
      changeFilter({ ...model.filter, folder: shownId });
    }
  }, [ready, shownId]);

  // New files show up by themselves: every 30 seconds, and when the tab comes back; not while
  // selecting, so what is selected stays as it is.
  useEffect(() => {
    const check = () => {
      if (document.visibilityState === 'visible' && !live.current.selecting) {
        void model.refreshIfChanged().then((changed) => {
          if (changed) void refreshFolders();
        });
      }
    };
    const id = setInterval(check, 30_000);
    document.addEventListener('visibilitychange', check);
    return () => {
      clearInterval(id);
      document.removeEventListener('visibilitychange', check);
    };
  }, []);

  const changeFilter = (f: LibraryFilter) => {
    if (sameFilter(f, model.filter)) return;
    clear();
    void model.setFilter(f);
  };
  // Typing searches after a short pause.
  useEffect(() => {
    const id = setTimeout(() => changeFilter({ ...model.filter, q: query }), 350);
    return () => clearTimeout(id);
  }, [query]);

  // More pages while the end of the list is less than a screen or so away.
  useEffect(() => {
    const el = end.current;
    if (!el) return;
    const io = new IntersectionObserver((entries) => entries.some((e) => e.isIntersecting) && void model.more(), {
      rootMargin: '0px 0px 1200px 0px',
    });
    io.observe(el);
    return () => io.disconnect();
  }, []);
  // A page that didn't fill the screen doesn't move the end, so look again after each one.
  useEffect(() => {
    const el = end.current;
    if (!el || model.loading || model.complete || model.failed) return;
    if (el.getBoundingClientRect().top < innerHeight + 1200) void model.more();
  });

  // Touch: a long press on a tile starts selecting, and moving the finger then selects every
  // file it passes, scrolling near the top and bottom, as in the app.
  useEffect(() => {
    const root = lib.current!;
    let timer = 0;
    let frame = 0;
    let start = { x: 0, y: 0 };
    let finger = { x: 0, y: 0 };
    let drag: { from: number; adds: boolean; base: Selection } | null = null;

    const indexAt = (x: number, y: number) => {
      const tile = document.elementFromPoint(x, y)?.closest<HTMLElement>('.ltile');
      return tile ? Number(tile.dataset.index) : -1;
    };
    const apply = (to: number) => {
      const d = drag!;
      const { files, days } = live.current;
      const [lo, hi] = d.from <= to ? [d.from, to] : [to, d.from];
      setSel(setFiles(d.base, files.slice(lo, hi + 1), d.adds, days));
    };
    const edge = () => {
      const zone = 80;
      const bottom = innerHeight - 80; // above the bar at the bottom
      const step = finger.y < zone + 60 ? -(zone + 60 - finger.y) / 3 : finger.y > bottom - zone ? (finger.y - (bottom - zone)) / 3 : 0;
      if (step) {
        scrollBy(0, step);
        const i = indexAt(finger.x, finger.y);
        if (i >= 0) apply(i);
      }
      frame = requestAnimationFrame(edge);
    };
    const onStart = (e: TouchEvent) => {
      const tile = (e.target as Element).closest<HTMLElement>('.ltile');
      if (!tile || e.touches.length !== 1) return;
      start = finger = { x: e.touches[0].clientX, y: e.touches[0].clientY };
      timer = window.setTimeout(() => {
        const from = Number(tile.dataset.index);
        const { sel, files } = live.current;
        if (!files[from]) return;
        drag = { from, adds: !isSelected(sel, files[from]), base: sel };
        setAnchor(from);
        apply(from);
        frame = requestAnimationFrame(edge);
      }, longPress);
    };
    const onMove = (e: TouchEvent) => {
      const p = e.touches[0];
      finger = { x: p.clientX, y: p.clientY };
      if (!drag) {
        if (Math.hypot(p.clientX - start.x, p.clientY - start.y) > 10) clearTimeout(timer); // scrolling
        return;
      }
      e.preventDefault(); // the finger selects; the page stays
      const i = indexAt(p.clientX, p.clientY);
      if (i >= 0) apply(i);
    };
    const onEnd = () => {
      clearTimeout(timer);
      if (!drag) return;
      drag = null;
      cancelAnimationFrame(frame);
      suppressClick.current = Date.now() + 600; // the finger lifting is no tap
    };
    // A long press would bring up the browser's own menu for the tile.
    const onMenu = (e: Event) => {
      if (drag || Date.now() < suppressClick.current) e.preventDefault();
    };
    root.addEventListener('touchstart', onStart, { passive: true });
    root.addEventListener('touchmove', onMove, { passive: false });
    root.addEventListener('touchend', onEnd);
    root.addEventListener('touchcancel', onEnd);
    root.addEventListener('contextmenu', onMenu);
    return () => {
      clearTimeout(timer);
      cancelAnimationFrame(frame);
      root.removeEventListener('touchstart', onStart);
      root.removeEventListener('touchmove', onMove);
      root.removeEventListener('touchend', onEnd);
      root.removeEventListener('touchcancel', onEnd);
      root.removeEventListener('contextmenu', onMenu);
    };
  }, []);

  // Keys: Escape ends selecting, Ctrl+A selects all, Delete deletes, the arrows go from tile to tile.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (document.querySelector('dialog[open]')) return;
      const { selecting, days, admin } = live.current;
      if (e.key === 'Escape' && selecting) {
        clear();
        return;
      }
      if ((e.target as Element).closest('input, textarea, select')) return;
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'a' && days.length > 0) {
        e.preventDefault();
        setSel(selectAll(days));
      } else if ((e.key === 'Delete' || e.key === 'Backspace') && selecting && admin) {
        e.preventDefault();
        askDelete();
      } else if (e.key.startsWith('Arrow')) {
        const tile = (e.target as Element).closest<HTMLElement>('.ltile');
        const next = tile && neighbour(tile, e.key);
        if (next) {
          e.preventDefault();
          next.querySelector<HTMLElement>('.lopen')?.focus();
        }
      }
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, []);

  const toggle = (i: number) => {
    const f = model.files[i];
    setSel(setFiles(sel, [f], !isSelected(sel, f), days));
    setAnchor(i);
  };
  const onTileClick = (i: number, e: MouseEvent) => {
    if (Date.now() < suppressClick.current) return;
    if (e.shiftKey && anchor !== null) {
      const [lo, hi] = anchor <= i ? [anchor, i] : [i, anchor];
      setSel(setFiles(sel, model.files.slice(lo, hi + 1), true, days));
    } else if (e.ctrlKey || e.metaKey || e.shiftKey || selecting) {
      toggle(i);
    } else {
      setViewing(model.files[i].id);
    }
  };

  const idsOfDay = (day: string) => getFileIds(model.filter, day).then((r) => r.ids);
  const problemText = (e: unknown) =>
    e instanceof ApiError && e.code === 'busy' ? t('zip.busy') : t(e instanceof ApiError && e.status > 0 ? 'common.failed' : 'common.offline');

  /** Several files go into a folder or a ZIP, where the browser can save into folders (screen 26). */
  function downloadSelected() {
    if (busy) return;
    if (picked.count > zipLimit) return toast({ text: t('zip.tooMany') });
    if (picked.count > 1 && canSaveToFolder()) return setChoosing(true);
    void downloadAsZip();
  }

  async function downloadAsZip() {
    setChoosing(false);
    setBusy(true);
    try {
      const ids = await selectedIds(sel, idsOfDay);
      const zip = await download(ids, model.files.find((f) => f.id === ids[0]));
      clear();
      if (zip) {
        toast({
          text: t('zip.started', { n: zip.count, size: formatBytes(zip.size, lang) }),
          action: { label: t('zip.again'), run: () => startDownload(zipUrl(zip), zip.name) },
        });
      }
    } catch (e) {
      toast({ text: problemText(e) });
    } finally {
      setBusy(false);
    }
  }

  async function saveIntoFolder(dir: FileSystemDirectoryHandle) {
    setChoosing(false);
    setBusy(true);
    try {
      // The ZIP's list has every file's path in a folder for its day, as on the server.
      const zip = await createDownload(await selectedIds(sel, idsOfDay));
      startSave(zip.files, dir);
      clear();
    } catch (e) {
      toast({ text: problemText(e) });
    } finally {
      setBusy(false);
    }
  }

  /** What the server will call the ZIP: its name and the files' day, or today. */
  const zipName = () => {
    const days = [...sel.days.keys()];
    const today = new Date();
    const day =
      days.length === 1 ? days[0] : `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`;
    return `${(choices && folders.shown?.name) || info?.name || 'Share'} ${day}.zip`;
  };

  async function shareSelected() {
    if (busy) return;
    if (picked.count > shareLimit.files || picked.bytes > shareLimit.bytes) return toast({ text: t('share.tooMany') });
    setBusy(true);
    let files: File[];
    try {
      let list = loadedSelected(sel, model.files);
      if (list.length < picked.count) {
        const known = new Map(list.map((f) => [f.id, f]));
        list = await Promise.all((await selectedIds(sel, idsOfDay)).map((id) => known.get(id) ?? getFile(id)));
      }
      toast({ text: tn('share.preparing', list.length) });
      files = await filesToShare(list);
    } catch (e) {
      setBusy(false);
      return toast({ text: problemText(e) });
    }
    setBusy(false);
    const share = () =>
      navigator.share({ files }).then(
        () => {
          toast(null);
          clear();
        },
        (e: unknown) => {
          // Getting the files took long enough that the browser wants another tap.
          if (e instanceof DOMException && e.name === 'NotAllowedError') toast({ text: t('share.ready'), action: { label: t('select.share'), run: () => void share() } });
          else toast(null);
        },
      );
    await share();
  }

  function askDelete() {
    setDeleteProblem(null);
    setDeleting({ kind: 'selection', count: totals(live.current.sel, live.current.days).count });
    void learnTrashDays().then(() => redraw((n) => n + 1));
  }

  async function confirmDelete(d: Deleting) {
    setBusy(true);
    setDeleteProblem(null);
    let ids: string[];
    let n: number;
    try {
      ids = d.kind === 'file' ? [d.file.id] : await selectedIds(sel, idsOfDay);
      n = await deleteMany(ids);
    } catch (e) {
      setBusy(false);
      return setDeleteProblem(problemText(e));
    }
    setBusy(false);
    setDeleting(null);
    if (d.kind === 'file') {
      // The viewer goes on to the next file, or back to the one before, or closes.
      const i = model.files.findIndex((f) => f.id === d.file.id);
      const next = model.files[i + 1] ?? model.files[i - 1];
      setViewing(next ? next.id : null);
      model.remove(ids);
    } else {
      clear();
      void model.reload();
    }
    toast({
      text: tn('delete.done', n),
      action: {
        label: t('common.undo'),
        run: () =>
          void restoreMany(ids).then(
            () => model.reload(),
            () => toast({ text: t('common.failed') }),
          ),
      },
    });
  }

  /** Screen 49: the selected files go into another folder; out of the one shown, with Undo. */
  async function move(to: FolderInfo) {
    setBusy(true);
    setMoveProblem(null);
    const from = shownId;
    let ids: string[];
    let n: number;
    try {
      ids = await selectedIds(sel, idsOfDay);
      n = await moveMany(ids, to.id);
    } catch (e) {
      setBusy(false);
      return setMoveProblem(problemText(e));
    }
    setBusy(false);
    setMoving(false);
    clear();
    if (from) model.remove(ids);
    else void model.reload();
    void refreshFolders();
    toast({
      text: tn('move.done', n, { folder: to.name }),
      action: from
        ? {
            label: t('common.undo'),
            run: () =>
              void moveMany(ids, from).then(
                () => {
                  void model.reload();
                  void refreshFolders();
                },
                () => toast({ text: t('common.failed') }),
              ),
          }
        : undefined,
    });
  }

  const closeSearch = () => {
    setSearching(false);
    setQuery('');
  };
  const searchField = (autoFocus: boolean) => (
    <input
      class="search-input"
      type="search"
      enterKeyHint="search"
      placeholder={t('library.search')}
      aria-label={t('library.search')}
      value={query}
      autoFocus={autoFocus}
      onInput={(e) => setQuery(e.currentTarget.value)}
      onKeyDown={(e) => e.key === 'Escape' && autoFocus && closeSearch()}
    />
  );

  const now = new Date();
  const sections = model.sections;
  const empty = model.files.length === 0 && !model.loading;
  const filtered = model.filter.kind !== null || model.filter.q.trim() !== '';
  const size = formatBytes(picked.bytes, lang);
  let offset = 0;

  return (
    <Shell
      tab="library"
      selecting={selecting}
      tools={
        <label class="dsearch">
          <Icon name="search" />
          {searchField(false)}
        </label>
      }
    >
      {selecting ? (
        <header class="ab ctx">
          <button type="button" class="ib" aria-label={t('select.stop')} onClick={clear}>
            <Icon name="x" />
          </button>
          <h1>{tn('select.count', picked.count)}</h1>
          <button type="button" class="tbtn" onClick={() => setSel(selectAll(days))}>
            {t('select.all')}
          </button>
        </header>
      ) : (
        <TitleBar
          title={t('nav.library')}
          field={searching ? searchField(true) : choices ? <FolderTitle shown={folders.shown} onOpen={() => setPicking(true)} /> : undefined}
        >
          <button
            type="button"
            class="ib"
            aria-label={t(searching ? 'library.searchClose' : 'library.search')}
            onClick={() => (searching ? closeSearch() : setSearching(true))}
          >
            <Icon name={searching ? 'x' : 'search'} />
          </button>
          <Link href="/settings" class="ab-me" aria-label={t('nav.settings')}>
            <Avatar id={me.user.id} name={me.user.name} size="sm" />
          </Link>
        </TitleBar>
      )}
      <div ref={lib} class={`lib${selecting ? ' selecting' : ''}${column ? ' withf' : ''}`}>
        {column && <FolderColumn list={folders.list!} shown={folders.shown} />}
        <div class="fmain">
          {choices && !wide && (
            <div class="lhead">
              <FolderTitle shown={folders.shown} onOpen={() => setPicking(true)} />
            </div>
          )}
          <div class="chips">
            {kinds.map((k) => (
              <button
                key={k.label}
                type="button"
                class={`chip${model.filter.kind === k.kind ? ' on' : ''}`}
                aria-pressed={model.filter.kind === k.kind}
                onClick={() => changeFilter({ ...model.filter, kind: k.kind, q: query })}
              >
                {t(k.label)}
              </button>
            ))}
          </div>
          {selecting && (
            <p class="tipbar">
              <Icon name="info" />
              {t(touch ? 'select.tipTouch' : 'select.tipMouse')}
            </p>
          )}
          {sections.map((s) => {
            const day: LibraryDay = { day: s.day, count: s.count, bytes: s.bytes };
            const state = dayState(sel, day);
            const first = offset;
            offset += s.files.length;
            return (
              <section class="lday" key={s.day}>
                <header class="dayh">
                  <div>
                    <h2>{formatDay(s.day, now, lang, t('day.today'), t('day.yesterday'))}</h2>
                    <p>
                      {state === 'some'
                        ? t('day.selected', { selected: dayCount(sel, day), count: s.count })
                        : tn('library.dayMeta', s.count, { size: formatBytes(s.bytes, lang) })}
                    </p>
                  </div>
                  <button
                    type="button"
                    class={`dsel ${state}`}
                    aria-label={t('select.day')}
                    aria-pressed={state === 'all' ? 'true' : state === 'some' ? 'mixed' : 'false'}
                    onClick={() => setSel(setDay(sel, s.day, state !== 'all'))}
                  >
                    {state === 'all' && <Icon name="check" />}
                    {state === 'some' && <Icon name="minus" />}
                  </button>
                </header>
                <div class="lgrid">
                  {s.files.map((f, j) => (
                    <Tile
                      key={f.id}
                      file={f}
                      index={first + j}
                      selected={selecting && isSelected(sel, f)}
                      selecting={selecting}
                      onClick={(e) => onTileClick(first + j, e)}
                      onCircle={() => toggle(first + j)}
                      savedInto={saved.has(f.id) ? saved.folder : undefined}
                    />
                  ))}
                </div>
              </section>
            );
          })}
          {empty && model.failed && (
            <div class="lempty">
              <Icon name="wifi" />
              <p>{t('common.offline')}</p>
              <button type="button" class="tbtn" onClick={() => void model.reload()}>
                {t('common.retry')}
              </button>
            </div>
          )}
          {empty && !model.failed && (
            <div class="lempty">
              <Icon name={filtered ? 'search' : 'images'} />
              <p>{t(filtered ? 'library.nothingFound' : 'library.empty')}</p>
            </div>
          )}
          {!empty && model.failed && (
            <p class="lmore">
              {t('common.offline')}{' '}
              <button type="button" class="tbtn" onClick={() => void model.more()}>
                {t('common.retry')}
              </button>
            </p>
          )}
          {model.loading && <span class="spinner" role="status" aria-label={t('common.loading')} />}
          <div ref={end} />
        </div>
      </div>
      {selecting && (
        <div class="selbar" role="toolbar" aria-label={tn('select.count', picked.count)}>
          <button type="button" class="ib sel-wide" aria-label={t('select.stop')} onClick={clear}>
            <Icon name="x" />
          </button>
          <b class="sel-wide">{tn('select.count', picked.count)}</b>
          <span class="sel-size sel-wide">{size}</span>
          <button type="button" class="tbtn sel-wide" onClick={() => setSel(selectAll(days))}>
            {t('select.all')}
          </button>
          <span class="gap sel-wide" />
          {shareable && (
            <button type="button" class="abtn" disabled={busy} onClick={() => void shareSelected()}>
              <Icon name="share" />
              <span class="lbl">{t('select.share')}</span>
            </button>
          )}
          {admin && choices && (
            <button
              type="button"
              class="abtn"
              disabled={busy}
              onClick={() => {
                setMoveProblem(null);
                setMoving(true);
              }}
            >
              <Icon name="folder" />
              <span class="lbl">{t('select.move')}</span>
            </button>
          )}
          {admin && (
            <button type="button" class="abtn danger" disabled={busy} onClick={askDelete}>
              <Icon name="trash" />
              <span class="lbl">{t('select.delete')}</span>
            </button>
          )}
          <button type="button" class="btn primary sm sel-dl" disabled={busy} onClick={downloadSelected}>
            <Icon name="download" />
            {tn('select.download', picked.count)}
            <small class="sel-phone"> · {size}</small>
          </button>
        </div>
      )}
      {viewing && (
        <Viewer
          files={model.files}
          id={viewing}
          more={model.complete ? undefined : () => void model.more()}
          onMove={setViewing}
          onClose={() => setViewing(null)}
          folderOf={choices ? (f: FileInfo) => folders.byId(f.folder)?.name : undefined}
          onDelete={
            admin
              ? (file) => {
                  setDeleteProblem(null);
                  setDeleting({ kind: 'file', file });
                  void learnTrashDays().then(() => redraw((n) => n + 1));
                }
              : undefined
          }
        />
      )}
      {picking && folders.list && <FolderSheet list={folders.list} shown={folders.shown} onClose={() => setPicking(false)} />}
      {moving && folders.list && (
        <MoveDialog
          count={picked.count}
          list={folders.list}
          here={shownId}
          busy={busy}
          problem={moveProblem}
          onMove={(to) => void move(to)}
          onClose={() => setMoving(false)}
        />
      )}
      {choosing && (
        <SaveChoice
          count={picked.count}
          bytes={picked.bytes}
          zipName={zipName()}
          onZip={() => void downloadAsZip()}
          onFolder={(dir) => void saveIntoFolder(dir)}
          onClose={() => setChoosing(false)}
        />
      )}
      {deleting && (
        <Confirm
          icon="trash"
          title={tn('delete.filesTitle', deleting.kind === 'file' ? 1 : deleting.count)}
          body={t('delete.filesBody', { days: knownTrashDays() })}
          confirm={tn('delete.filesButton', deleting.kind === 'file' ? 1 : deleting.count)}
          danger
          busy={busy}
          problem={deleteProblem}
          onConfirm={() => void confirmDelete(deleting)}
          onClose={() => setDeleting(null)}
        />
      )}
    </Shell>
  );
}

/** The tile an arrow key goes to: beside it, or above or below it in its grid, or in the day
 * before or after. */
function neighbour(tile: HTMLElement, key: string): HTMLElement | null {
  const all = [...document.querySelectorAll<HTMLElement>('.ltile')];
  if (key === 'ArrowLeft' || key === 'ArrowRight') return all[all.indexOf(tile) + (key === 'ArrowLeft' ? -1 : 1)] ?? null;
  const grid = tile.parentElement!;
  const tiles = [...grid.children] as HTMLElement[];
  const cols = getComputedStyle(grid).gridTemplateColumns.split(' ').length;
  const i = tiles.indexOf(tile);
  const col = i % cols;
  const grids = [...document.querySelectorAll<HTMLElement>('.lgrid')];
  const g = grids.indexOf(grid);
  if (key === 'ArrowUp') {
    if (i >= cols) return tiles[i - cols];
    const prev = grids[g - 1];
    if (!prev) return null;
    const n = prev.children.length;
    return prev.children[Math.min(Math.floor((n - 1) / cols) * cols + col, n - 1)] as HTMLElement;
  }
  if (key === 'ArrowDown') {
    if (i + cols < tiles.length) return tiles[i + cols];
    if (Math.floor(i / cols) < Math.floor((tiles.length - 1) / cols)) return tiles[tiles.length - 1];
    const next = grids[g + 1];
    return next ? ((next.children[Math.min(col, next.children.length - 1)] as HTMLElement | undefined) ?? null) : null;
  }
  return null;
}
