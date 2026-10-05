// A guest's look into the folder of a PIN that shows it (screen 46): what everyone sent, by
// day, to open and download, but not to delete. It comes in a chunk of its own, with the
// account pages' library parts, so PINs that only send never load them.
import '../account/account.css';
import '../account/library/library.css';
import './guest.css';
import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import de from '../account/i18n/de.json';
import en from '../account/i18n/en.json';
import it from '../account/i18n/it.json';
import { download, downloadEach, eachLimit, startDownload, zipLimit, type EachNote } from '../account/library/actions';
import { LibraryModel } from '../account/library/model';
import { Tile } from '../account/library/Tile';
import { canSaveToFolder } from '../account/save/folder';
import { SaveChoice } from '../account/save/SaveChoice';
import { SavePanel } from '../account/save/SavePanel';
import { startSave } from '../account/save/store';
import { Viewer } from '../account/viewer/Viewer';
import { ApiError, createDownload, getFileIds, getFiles, getFolders, getInfo, getLibrary, zipUrl, type FolderInfo } from '../api';
import { Icon } from '../components/Icon';
import { Page } from '../components/Page';
import { formatBytes, formatCount, formatDay } from '../format';
import { addDictionaries, useI18n } from '../i18n';
import { guestZipName } from './see';

addDictionaries({ en, de, it });

export function See({ name, onEnded }: { name: string; onEnded: () => void }) {
  const { t, tn, lang } = useI18n();
  const [folder, setFolder] = useState<FolderInfo | null>(null);
  const [viewing, setViewing] = useState<string | null>(null);
  const [choosing, setChoosing] = useState<{ ids: string[]; bytes: number } | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  /** With the files in a bucket, which sends no ZIP, several download one by one. */
  const [s3, setS3] = useState(false);
  const [note, setNote] = useState<EachNote | null>(null);
  const [, redraw] = useState(0);
  const end = useRef<HTMLDivElement>(null);
  const ended = useRef(onEnded);
  ended.current = onEnded;
  const model = useMemo(() => {
    // The PIN ended or was changed: back to the PIN's screen.
    const gone = (e: unknown) => {
      if (e instanceof ApiError && e.status === 401) ended.current();
      throw e;
    };
    return new LibraryModel({ overview: (f) => getLibrary(f).catch(gone), page: (f, c, n) => getFiles(f, c, n).catch(gone) });
  }, []);
  const loadFolder = () =>
    getFolders().then(
      (r) => setFolder(r.folders[0] ?? null),
      () => {},
    );

  useEffect(() => {
    void getInfo().then(
      (info) => setS3(info.storage === 's3'),
      () => {},
    );
  }, []);
  useEffect(() => {
    const stop = model.subscribe(() => redraw((n) => n + 1));
    void model.reload();
    void loadFolder();
    // What others send shows up by itself.
    const id = setInterval(() => {
      if (document.visibilityState === 'visible') {
        void model.refreshIfChanged().then((changed) => {
          if (changed) void loadFolder();
        });
      }
    }, 30_000);
    return () => {
      stop();
      clearInterval(id);
    };
  }, []);
  useEffect(() => {
    const el = end.current;
    if (!el) return;
    const io = new IntersectionObserver((entries) => entries.some((e) => e.isIntersecting) && void model.more(), { rootMargin: '0px 0px 1200px 0px' });
    io.observe(el);
    return () => io.disconnect();
  }, []);

  const failed = (e: unknown) => setProblem(t(e instanceof ApiError && e.status > 0 ? 'common.failed' : 'common.offline'));

  /** Everything, as one ZIP or into a folder on a computer. */
  async function downloadAll() {
    if (busy) return;
    setBusy(true);
    setProblem(null);
    try {
      const { ids, bytes } = await getFileIds(model.filter);
      if (ids.length > zipLimit) return setProblem(t('zip.tooMany'));
      if (ids.length > 1 && canSaveToFolder()) return setChoosing({ ids, bytes });
      if (ids.length > 1 && s3) return each(ids);
      await download(ids, model.files.find((f) => f.id === ids[0]));
    } catch (e) {
      failed(e);
    } finally {
      setBusy(false);
    }
  }

  function each(ids: string[]) {
    setChoosing(null);
    if (ids.length > eachLimit) return setProblem(t('each.tooMany', { n: eachLimit }));
    downloadEach(ids, setNote, { t, tn });
  }

  async function zip(ids: string[]) {
    setChoosing(null);
    try {
      const z = await createDownload(ids);
      startDownload(zipUrl(z), z.name);
    } catch (e) {
      failed(e);
    }
  }

  async function intoFolder(ids: string[], dir: FileSystemDirectoryHandle) {
    setChoosing(null);
    try {
      startSave((await createDownload(ids)).files, dir);
    } catch (e) {
      failed(e);
    }
  }

  const now = new Date();
  const days = model.overview?.days ?? [];
  const total = days.reduce((s, d) => s + d.bytes, 0);
  let offset = 0;
  return (
    <Page name={name} languageSwitch layout="see">
      <h1 class="gtitle">{folder?.name ?? name}</h1>
      {folder && (
        <p class="lead gmeta">
          {tn('see.files', folder.files, { n: formatCount(folder.files, lang) })} {tn('see.from', folder.senders, { n: formatCount(folder.senders, lang) })}
        </p>
      )}
      {days.length > 0 && (
        <button type="button" class="btn tonal sm gall" disabled={busy} onClick={() => void downloadAll()}>
          <Icon name="download" />
          {t('see.downloadAll', { size: formatBytes(total, lang) })}
        </button>
      )}
      {problem && (
        <p class="help err" role="alert">
          <Icon name="alert" />
          {problem}
        </p>
      )}
      {note && (
        <p class="help" role="status">
          {note.text}
          {note.action && (
            <button
              type="button"
              class="tbtn"
              onClick={() => {
                const run = note.action!.run;
                setNote(null);
                run();
              }}
            >
              {note.action.label}
            </button>
          )}
        </p>
      )}
      <div class="gfiles">
        {model.sections.map((s) => {
          const first = offset;
          offset += s.files.length;
          return (
            <section class="lday" key={s.day}>
              <header class="dayh">
                <div>
                  <h2>{formatDay(s.day, now, lang, t('day.today'), t('day.yesterday'))}</h2>
                  <p>{tn('library.dayMeta', s.count, { size: formatBytes(s.bytes, lang) })}</p>
                </div>
              </header>
              <div class="lgrid">
                {s.files.map((f, j) => (
                  <Tile key={f.id} file={f} index={first + j} selected={false} selecting={false} onClick={() => setViewing(f.id)} onCircle={() => {}} />
                ))}
              </div>
            </section>
          );
        })}
        {model.files.length === 0 && !model.loading && <p class="help">{t(model.failed ? 'common.offline' : 'see.empty')}</p>}
        {model.loading && <span class="spinner" role="status" aria-label={t('common.loading')} />}
        <div ref={end} />
      </div>
      {viewing && (
        <Viewer
          files={model.files}
          id={viewing}
          more={model.complete ? undefined : () => void model.more()}
          onMove={setViewing}
          onClose={() => setViewing(null)}
        />
      )}
      {choosing && (
        <SaveChoice
          count={choosing.ids.length}
          bytes={choosing.bytes}
          zipName={s3 ? null : guestZipName(name, days.map((d) => d.day), now)}
          onOther={() => (s3 ? each(choosing.ids) : void zip(choosing.ids))}
          onFolder={(dir) => void intoFolder(choosing.ids, dir)}
          onClose={() => setChoosing(null)}
        />
      )}
      <SavePanel />
    </Page>
  );
}
