import { useEffect, useRef } from 'preact/hooks';
import { Bold, Note, Tile } from '../components/Bits';
import { GhostCard } from '../components/GhostCard';
import { Icon } from '../components/Icon';
import { NotifyOffer } from '../components/NotifyOffer';
import { Page } from '../components/Page';
import { touchFirst, useMedia, useOnWifi, useWakeLock } from '../device';
import { canEncrypt } from '../e2ee/trust';
import { RateMeter, formatBytes, formatCount, formatETA } from '../format';
import { useI18n } from '../i18n';
import type { Snapshot } from '../uploader';
import { FilePicker } from './FilePicker';

interface Props {
  name: string;
  snapshot: Snapshot;
  online: boolean;
  rejected: string[];
  /** Files weren't sent: what the server says about their folder's keys can't be checked; who
   * to tell depends on who sends. */
  refused?: 'account' | 'guest' | null;
  onFiles: (f: File[]) => void;
  onSkipGhosts: () => void;
  onRetry: () => void;
}

/** Screen 4: progress in files and bytes, and every file as a tile. */
export function SendingScreen({ name, snapshot: s, online, rejected, refused, onFiles, onSkipGhosts, onRetry }: Props) {
  const { t, tn, lang } = useI18n();
  const meter = useRef(new RateMeter());
  useEffect(() => meter.current.add(Date.now(), s.bytesDone), [s.bytesDone]);
  const wifi = useOnWifi();
  const computer = !useMedia(touchFirst);
  useWakeLock(s.done + s.failed + s.ghosts.length < s.total);

  const pct = s.bytesTotal > 0 ? (s.bytesDone / s.bytesTotal) * 100 : 0;
  const eta = online ? meter.current.eta(s.bytesTotal - s.bytesDone) : null;
  return (
    <Page name={name} layout="sending">
      {/* On computers this column stays in view beside the tiles; on phones "Add more files"
          still comes after them (.more in styles.css). */}
      <div class="pane side">
        <h1 class="hero md">{t('sending.title')}</h1>
        <div class="count">
          <span class="big">{formatCount(s.done, lang)}</span>
          <span class="of">{t('sending.of', { total: formatCount(s.total, lang) })}</span>
          <span class="eta">{formatETA(eta, lang)}</span>
        </div>
        <div class="bar" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(pct)}>
          <i style={{ width: `${pct}%` }} />
        </div>
        <div class="bytes">
          <span>{t('sending.bytes', { done: formatBytes(s.bytesDone, lang), total: formatBytes(s.bytesTotal, lang) })}</span>
          {wifi && online && <span>{t('sending.onWifi')}</span>}
        </div>
        {online ? (
          // A phone stops a page in the background after a while; a computer keeps sending.
          <Note icon={computer ? 'monitor' : 'smartphone'}>
            <Bold text={t(computer ? 'sending.keepOpenDesktop' : 'sending.keepOpen')} />
          </Note>
        ) : (
          <Note icon="wifi">{t('sending.offline')}</Note>
        )}
        <NotifyOffer bytes={s.bytesTotal} />
        {s.failed > 0 && (
          <div class="failed" role="alert">
            <Icon name="alert" />
            <span>{tn('sending.failed', s.failed)}</span>
            <button type="button" class="btn tonal xs" onClick={onRetry}>
              {t('sending.retry')}
            </button>
          </div>
        )}
        {s.ghosts.length > 0 && <GhostCard ghosts={s.ghosts} onFiles={onFiles} onSkip={onSkipGhosts} />}
        {rejected.map((n) => (
          <p key={n} class="help err">
            <Icon name="alert" />
            {t('sending.tooLarge', { name: n })}
          </p>
        ))}
        {refused && s.failed > 0 && (
          <p class="help err">
            <Icon name="alert" />
            {t(!canEncrypt() ? 'sending.insecure' : refused === 'guest' ? 'sending.uncheckedGuest' : 'sending.unchecked')}
          </p>
        )}
        <div class="grow" />
        <FilePicker label={t('sending.addMore')} look="outline more" onFiles={onFiles} />
      </div>
      <div class="grid g3 tiles">
        {s.tiles.map((tile) => (
          <Tile key={tile.id} tile={tile} />
        ))}
      </div>
    </Page>
  );
}
