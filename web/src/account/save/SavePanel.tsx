import './save.css';
import { Icon } from '../../components/Icon';
import { NotifyOffer } from '../../components/NotifyOffer';
import { useLeaveWarning, useWakeLock } from '../../device';
import { formatBytes, formatPercent } from '../../format';
import { useI18n } from '../../i18n';
import { hideSave, saveAgain, stopSave, useSaveRun } from './store';

/** Screen 27: saving into a folder, as a panel in the corner, so the library stays usable. */
export function SavePanel() {
  const { t, tn, lang } = useI18n();
  const run = useSaveRun();
  const active = !!run && !run.state.finished;
  // Closing the tab would stop it, so that asks first; and the computer stays awake.
  useLeaveWarning(active);
  useWakeLock(active);
  if (!run) return null;
  const s = run.state;
  const share = s.bytesTotal > 0 ? s.bytesDone / s.bytesTotal : s.total > 0 ? s.done / s.total : 1;

  if (run.hidden && active) {
    return (
      <button type="button" class="savemini" onClick={() => hideSave(false)}>
        <span class="minibar" style={{ '--p': `${Math.round(share * 100)}%` }} />
        {t('save.mini', { done: s.done, total: s.total })}
        <span class="lnk">{t('save.show')}</span>
      </button>
    );
  }

  const saved = s.done - s.skipped;
  let title: string;
  let note: string;
  let icon: 'monitor' | 'wifi' | 'alert' | 'check' = 'monitor';
  if (!s.finished) {
    title = tn('save.saving', s.total);
    if (s.waiting) {
      icon = 'wifi';
      note = t('save.waiting');
    } else {
      note = [t('save.keepOpen'), s.skipped > 0 ? tn('save.skipped', s.skipped) : ''].filter(Boolean).join(' ');
    }
  } else if (s.stopped) {
    title = t('save.stopped');
    icon = 'alert';
    note = s.stopped === 'full' ? t('save.full') : s.stopped === 'signedOut' ? t('signIn.signedOut') : t('save.folderGone');
  } else {
    title = tn('save.done', saved);
    icon = s.failed.length > 0 ? 'alert' : 'check';
    note = [s.skipped > 0 ? tn('save.skippedDone', s.skipped) : '', s.failed.length > 0 ? tn('save.failed', s.failed.length) : '']
      .filter(Boolean)
      .join(' ');
  }

  return (
    <section class="savep" aria-label={title} aria-live="polite">
      <h2>{title}</h2>
      {!s.finished && (
        <>
          <div class="prow">
            <b>{t('save.of', { done: s.done, total: s.total })}</b>
            <span>{t('save.of', { done: formatBytes(s.bytesDone, lang), total: formatBytes(s.bytesTotal, lang) })}</span>
          </div>
          <div class="bar" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(share * 100)} aria-valuetext={formatPercent(share, lang)}>
            <i style={{ width: `${share * 100}%` }} />
          </div>
        </>
      )}
      <div class="drow">
        <span class="ri">
          <Icon name="folder" />
        </span>
        <div>
          <b>{t('save.into', { folder: run.folder })}</b>
          <span>{t('save.perDay')}</span>
        </div>
      </div>
      {note && (
        <div class="note slim">
          <Icon name={icon} />
          <p>{note}</p>
        </div>
      )}
      {active && <NotifyOffer bytes={s.bytesTotal} />}
      <div class="btnrow">
        {active ? (
          <>
            <button type="button" class="btn outline" onClick={stopSave}>
              {t('common.cancel')}
            </button>
            <button type="button" class="btn tonal" onClick={() => hideSave(true)}>
              {t('save.hide')}
            </button>
          </>
        ) : (
          <>
            {(s.stopped || s.failed.length > 0) && (
              <button type="button" class="btn outline" onClick={saveAgain}>
                {t('save.again')}
              </button>
            )}
            <button type="button" class="btn tonal" onClick={stopSave}>
              {t('common.close')}
            </button>
          </>
        )}
      </div>
    </section>
  );
}
