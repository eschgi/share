import './viewer.css';
import { useContext, useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { contentUrl, thumbUrl, type FileInfo } from '../../api';
import { Icon } from '../../components/Icon';
import { useMedia } from '../../device';
import { formatBytes, formatDay, formatDuration, formatTime } from '../../format';
import { useI18n } from '../../i18n';
import { useOverlay } from '../../router';
import { Modal } from '../components/Modal';
import { Thumb } from '../library/Tile';
import { ToastContext, useLayer } from '../layers';
import { lockScroll } from '../scroll';
import { useZoom } from './useZoom';
import { filmRange, previewOf } from './view';
import { isZoomed, type Zoom } from './zoom';

interface Props {
  /** The library's loaded files, in its order; id is the one shown. */
  files: FileInfo[];
  id: string;
  /** Loads the next page, while the list has more. */
  more?: () => void;
  onMove: (id: string) => void;
  onClose: () => void;
  /** Admins: asks to delete the file shown. */
  onDelete?: (file: FileInfo) => void;
  /** The name of a file's folder, for the details; left out where there is only one folder. */
  folderOf?: (file: FileInfo) => string | undefined;
}

/** Whether the details are open: beside the picture on computers, where there's room, at first. */
let detailsOpen: boolean | null = null;

const wideQuery = '(min-width: 1024px) and (min-height: 540px)';

/**
 * Screens 14 and 28: one file at a time, as big as it goes. Photos show their thumbnail until
 * the original is there, videos and sound play right here. Arrows, swipes and the film strip
 * go to the others; Back and Escape close it.
 */
export function Viewer({ files, id, more, onMove, onClose, onDelete, folderOf }: Props) {
  const { t, lang } = useI18n();
  const ref = useRef<HTMLDialogElement>(null);
  const wide = useMedia(wideQuery);
  const [details, setDetails] = useState(() => detailsOpen ?? matchMedia(wideQuery).matches);
  const stage = useRef<HTMLDivElement>(null);
  const top = useLayer();
  const toast = useContext(ToastContext);
  useOverlay(true, onClose);

  useLayoutEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    const unlock = lockScroll();
    ref.current?.showModal();
    return () => {
      unlock();
      opener?.focus?.();
    };
  }, []);

  const index = files.findIndex((f) => f.id === id);
  const file = files[index] as FileInfo | undefined;
  useEffect(() => {
    if (index < 0) onClose();
    else if (more && index >= files.length - 3) more();
  }, [index, files.length]);
  const go = (step: number) => {
    const next = files[index + step];
    if (next) onMove(next.id);
  };
  const zoomable = file !== undefined && previewOf(file) === 'image';
  const zoom = useZoom(stage, { id, zoomable, onSwipe: go });
  if (!file) return null;
  const toggleDetails = () => {
    detailsOpen = !details;
    setDetails(!details);
  };

  const day = formatDay(file.day, new Date(), lang, t('day.today'), t('day.yesterday'));
  const time = formatTime(new Date(file.uploaded_at), lang);
  const size = [
    formatBytes(file.size, lang),
    file.width && file.height ? `${file.width} × ${file.height}` : '',
    file.duration_ms !== null ? formatDuration(file.duration_ms) : '',
  ]
    .filter(Boolean)
    .join(' · ');
  const [from, to] = filmRange(index, files.length);
  const download = (
    <a class="vbtn" href={contentUrl(file)} download={file.name}>
      <Icon name="download" />
      {t('viewer.download')}
    </a>
  );
  const detailsButton = (
    <button type="button" class={`vbtn${details && wide ? ' on' : ''}`} aria-pressed={details} onClick={toggleDetails}>
      <Icon name="info" />
      {t('viewer.details')}
    </button>
  );
  const folder = folderOf?.(file);
  const facts = (
    <>
      <div class="vrow">
        <Icon name="clock" />
        <b>
          {day}, {time}
        </b>
      </div>
      <div class="vrow">
        <Icon name="user" />
        <b>{file.from === null ? t('viewer.fromPin') : t('viewer.from', { name: file.from })}</b>
      </div>
      {folder && (
        <div class="vrow">
          <Icon name="folder" />
          <b>{t('viewer.inFolder', { folder })}</b>
        </div>
      )}
      <div class="vrow">
        <Icon name="file" />
        <div>
          <b>{size}</b>
          <span>{file.mime}</span>
        </div>
      </div>
    </>
  );

  return (
    <dialog
      ref={ref}
      class={`viewer${details && wide ? ' with-side' : ''}`}
      aria-label={file.name}
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
      onKeyDown={(e) => {
        if ((e.target as Element).closest('input, video, audio')) return;
        if (zoom.key(e)) e.preventDefault();
        else if (e.key === 'ArrowLeft') go(-1);
        else if (e.key === 'ArrowRight') go(1);
        else return;
        e.preventDefault();
      }}
    >
      <header class="vtop">
        <button type="button" class="ib narrow-only" aria-label={t('common.close')} onClick={onClose}>
          <Icon name="back" />
        </button>
        <div class="vt">
          <b>{day}</b>
          <span>{time}</span>
        </div>
        <span class="wide-only">{download}</span>
        <span class="wide-only">{detailsButton}</span>
        {onDelete && (
          <span class="wide-only">
            <button type="button" class="vbtn" onClick={() => onDelete(file)}>
              <Icon name="trash" />
              {t('select.delete')}
            </button>
          </span>
        )}
        <button type="button" class="ib narrow-only" aria-label={t('viewer.details')} onClick={toggleDetails}>
          <Icon name="info" />
        </button>
        <button type="button" class="ib wide-only" aria-label={t('common.close')} onClick={onClose}>
          <Icon name="x" />
        </button>
      </header>
      <div ref={stage} class={`vstage${zoomable ? ' zoomable' : ''}${isZoomed(zoom.zoom) ? ' zoomed' : ''}`}>
        <button type="button" class="varr wide-only" aria-label={t('viewer.previous')} disabled={index === 0} onClick={() => go(-1)}>
          <Icon name="chev-left" />
        </button>
        <Media key={file.id} file={file} zoom={zoom.zoom} moving={zoom.moving} />
        <button type="button" class="varr wide-only" aria-label={t('viewer.next')} disabled={index === files.length - 1} onClick={() => go(1)}>
          <Icon name="chev" />
        </button>
      </div>
      <div class="film">
        {files.slice(from, to + 1).map((f, i) => (
          <button
            key={f.id}
            type="button"
            class={`ftile${from + i === index ? ' cur' : ''}`}
            aria-label={f.name}
            aria-current={from + i === index ? 'true' : undefined}
            onClick={() => onMove(f.id)}
          >
            <Thumb key={f.updated_at} file={f} />
          </button>
        ))}
      </div>
      <p class="vmeta narrow-only">
        {file.name} · {size}
      </p>
      <nav class="vactions narrow-only">
        <a href={contentUrl(file)} download={file.name}>
          <Icon name="download" />
          {t('viewer.download')}
        </a>
        <button type="button" onClick={toggleDetails}>
          <Icon name="info" />
          {t('viewer.details')}
        </button>
        {onDelete && (
          <button type="button" onClick={() => onDelete(file)}>
            <Icon name="trash" />
            {t('select.delete')}
          </button>
        )}
      </nav>
      {details && wide && (
        <aside class="vside">
          <h2>{file.name}</h2>
          {facts}
        </aside>
      )}
      {details && !wide && (
        <Modal title={file.name} onClose={toggleDetails}>
          <div class="vfacts">{facts}</div>
        </Modal>
      )}
      {top && toast}
    </dialog>
  );
}

/** The file itself: the photo, the video or the sound, or why it isn't shown. */
function Media({ file, zoom, moving }: { file: FileInfo; zoom: Zoom; moving: boolean }) {
  const { t } = useI18n();
  const kind = previewOf(file);
  const [loaded, setLoaded] = useState(false);
  const [failed, setFailed] = useState(false);

  if (kind === 'image') {
    const rest = zoom.scale === 1 && zoom.x === 0 && zoom.y === 0;
    const transform = rest ? undefined : `translate(${zoom.x}px, ${zoom.y}px) scale(${zoom.scale})`;
    const zoomClass = moving ? '' : ' eased';
    return (
      <div class="vmedia">
        {file.has_thumb && !loaded && <img class={`vpic${zoomClass}`} src={thumbUrl(file)} alt="" draggable={false} style={{ transform }} />}
        {!failed && (
          <img
            class={`vpic${zoomClass}${loaded ? '' : ' loading'}`}
            draggable={false}
            style={{ transform }}
            src={contentUrl(file)}
            alt={file.name}
            onLoad={() => setLoaded(true)}
            onError={() => setFailed(true)}
          />
        )}
        {failed && <p class="vnote">{t('viewer.cantShow')}</p>}
      </div>
    );
  }
  if (kind === 'video') {
    return (
      <div class="vmedia">
        {failed ? (
          <>
            {file.has_thumb && <img class="vpic" src={thumbUrl(file)} alt="" />}
            <p class="vnote">{t('viewer.cantPlay')}</p>
          </>
        ) : (
          <video
            class="vpic"
            src={contentUrl(file)}
            poster={file.has_thumb ? thumbUrl(file) : undefined}
            controls
            playsInline
            preload="metadata"
            onError={() => setFailed(true)}
          />
        )}
      </div>
    );
  }
  return (
    <div class="vmedia vdoc">
      <span class="vdoc-thumb">
        <Thumb key={file.updated_at} file={file} />
      </span>
      {kind === 'audio' && !failed ? (
        <audio src={contentUrl(file)} controls preload="metadata" onError={() => setFailed(true)} />
      ) : (
        <p class="vnote">{t('viewer.noPreview')}</p>
      )}
      <a class="btn sm primary" href={contentUrl(file)} download={file.name}>
        <Icon name="download" />
        {t('viewer.download')}
      </a>
    </div>
  );
}
