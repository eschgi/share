import './library.css';
import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { getFiles, getLibrary, type FileKind } from '../../api';
import { Icon } from '../../components/Icon';
import { formatBytes, formatDay } from '../../format';
import { useI18n } from '../../i18n';
import { Avatar, Link } from '../components/Bits';
import { useAccount } from '../context';
import { Shell, TitleBar } from '../Shell';
import { Viewer } from '../viewer/Viewer';
import { LibraryModel } from './model';
import { Tile } from './Tile';

const kinds: { kind: FileKind | null; label: string }[] = [
  { kind: null, label: 'library.all' },
  { kind: 'photo', label: 'library.photos' },
  { kind: 'video', label: 'library.videos' },
  { kind: 'document', label: 'library.documents' },
];

/** Screens 11 and 24: everything that was sent, newest day first. */
export function Library() {
  const { t, tn, lang } = useI18n();
  const { me } = useAccount();
  const model = useMemo(() => new LibraryModel({ overview: getLibrary, page: getFiles }), []);
  const [, redraw] = useState(0);
  const [query, setQuery] = useState('');
  const [searching, setSearching] = useState(false);
  const [viewing, setViewing] = useState<string | null>(null);
  const end = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const stop = model.subscribe(() => redraw((n) => n + 1));
    void model.reload();
    return stop;
  }, []);

  // New files show up by themselves: every 30 seconds, and when the tab comes back.
  useEffect(() => {
    const check = () => {
      if (document.visibilityState === 'visible') void model.refreshIfChanged();
    };
    const id = setInterval(check, 30_000);
    document.addEventListener('visibilitychange', check);
    return () => {
      clearInterval(id);
      document.removeEventListener('visibilitychange', check);
    };
  }, []);

  // Typing searches after a short pause.
  useEffect(() => {
    const id = setTimeout(() => void model.setFilter({ kind: model.filter.kind, q: query }), 350);
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

  return (
    <Shell
      tab="library"
      tools={
        <label class="dsearch">
          <Icon name="search" />
          {searchField(false)}
        </label>
      }
    >
      <TitleBar title={t('nav.library')} field={searching ? searchField(true) : undefined}>
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
      <div class="lib">
        <div class="chips">
          {kinds.map((k) => (
            <button
              key={k.label}
              type="button"
              class={`chip${model.filter.kind === k.kind ? ' on' : ''}`}
              aria-pressed={model.filter.kind === k.kind}
              onClick={() => void model.setFilter({ kind: k.kind, q: query })}
            >
              {t(k.label)}
            </button>
          ))}
        </div>
        {sections.map((s) => (
          <section class="lday" key={s.day}>
            <header class="dayh">
              <h2>{formatDay(s.day, now, lang, t('day.today'), t('day.yesterday'))}</h2>
              <p>{tn('library.dayMeta', s.count, { size: formatBytes(s.bytes, lang) })}</p>
            </header>
            <div class="lgrid">
              {s.files.map((f) => (
                <Tile key={f.id} file={f} onOpen={() => setViewing(f.id)} />
              ))}
            </div>
          </section>
        ))}
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
      {viewing && (
        <Viewer
          files={model.files}
          id={viewing}
          more={model.complete ? undefined : () => void model.more()}
          onMove={setViewing}
          onClose={() => setViewing(null)}
        />
      )}
    </Shell>
  );
}
