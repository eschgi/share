import { useEffect, useState } from 'preact/hooks';
import { getTrash, purgeFiles, restoreFiles, type TrashedFile } from '../../api';
import { Icon } from '../../components/Icon';
import { formatBytes } from '../../format';
import { useI18n } from '../../i18n';
import { Confirm } from '../components/Modal';
import { useAccount } from '../context';
import { inParts } from '../library/actions';
import { Thumb } from '../library/Tile';
import { daysLeft } from './format';

/**
 * Screen 35: what admins deleted in the last days, to bring back or remove for good. Rows toggle
 * on click, as in the app. The page's title and Select all come from where it is shown.
 */
export function useTrash(onChanged: () => void) {
  const [trash, setTrash] = useState<{ files: TrashedFile[]; days: number } | null>(null);
  const [failed, setFailed] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const load = async () => {
    try {
      const t = await getTrash();
      setTrash({ files: t.files, days: t.trash_days });
      setFailed(false);
      setSelected((s) => new Set([...s].filter((id) => t.files.some((f) => f.id === id))));
    } catch {
      setFailed(true);
    }
  };
  useEffect(() => void load(), []);
  return { trash, failed, selected, setSelected, load, onChanged };
}

export type TrashState = ReturnType<typeof useTrash>;

/** "Select all", while some are selected but not all. */
export function SelectAllTrash({ state }: { state: TrashState }) {
  const { t } = useI18n();
  const files = state.trash?.files ?? [];
  if (state.selected.size === 0 || state.selected.size >= files.length) return null;
  return (
    <button type="button" class="tbtn" onClick={() => state.setSelected(new Set(files.map((f) => f.id)))}>
      {t('select.all')}
    </button>
  );
}

export function TrashList({ state }: { state: TrashState }) {
  const { t, tn, lang } = useI18n();
  const { toast } = useAccount();
  const { trash, failed, selected, setSelected, load, onChanged } = state;
  const [busy, setBusy] = useState(false);
  const [asking, setAsking] = useState(false);
  const now = new Date();

  const toggle = (id: string) => {
    const next = new Set(selected);
    if (!next.delete(id)) next.add(id);
    setSelected(next);
  };

  const restore = async () => {
    setBusy(true);
    try {
      const n = await inParts([...selected], restoreFiles);
      toast({ text: tn('trash.restored', n) });
      setSelected(new Set());
    } catch {
      toast({ text: t('common.failed') });
    }
    setBusy(false);
    await load();
    onChanged();
  };

  const purge = async () => {
    setBusy(true);
    try {
      await inParts([...selected], purgeFiles);
      setSelected(new Set());
    } catch {
      toast({ text: t('common.failed') });
    }
    setBusy(false);
    setAsking(false);
    await load();
    onChanged();
  };

  if (failed && !trash) return <p class="help">{t('common.offline')}</p>;
  if (!trash) return <span class="spinner" role="status" aria-label={t('common.loading')} />;
  return (
    <>
      <p class="lead sm">{t('trash.lead', { days: trash.days })}</p>
      {trash.files.length === 0 ? (
        <div class="lempty">
          <Icon name="trash" />
          <p>{t('trash.empty')}</p>
        </div>
      ) : (
        <div class="group">
          {trash.files.map((f) => {
            const on = selected.has(f.id);
            return (
              <button key={f.id} type="button" class="row trow" role="checkbox" aria-checked={on} onClick={() => toggle(f.id)}>
                <span class="tmini">
                  <Thumb key={f.updated_at} file={f} />
                </span>
                <span class="rt">
                  <b>{f.name}</b>
                  <span>
                    {[
                      formatBytes(f.size, lang),
                      tn('trash.daysLeft', daysLeft(new Date(f.purge_at), now)),
                      f.deleted_by ? t('trash.deletedBy', { name: f.deleted_by }) : '',
                    ]
                      .filter(Boolean)
                      .join(' · ')}
                  </span>
                </span>
                <span class={`dsel ${on ? 'all' : 'none'}`}>{on && <Icon name="check" />}</span>
              </button>
            );
          })}
        </div>
      )}
      {selected.size > 0 && (
        <div class="trbar">
          <span>{tn('select.count', selected.size)}</span>
          <span class="gap" />
          <button type="button" class="btn sm dout" disabled={busy} onClick={() => setAsking(true)}>
            {t('trash.purge')}
          </button>
          <button type="button" class="btn sm primary" disabled={busy} onClick={() => void restore()}>
            <Icon name="rotate" />
            {tn('trash.restore', selected.size)}
          </button>
        </div>
      )}
      {asking && (
        <Confirm
          icon="trash"
          title={tn('trash.purgeTitle', selected.size)}
          body={t('trash.purgeBody')}
          confirm={t('trash.purge')}
          danger
          busy={busy}
          onConfirm={() => void purge()}
          onClose={() => setAsking(false)}
        />
      )}
    </>
  );
}
