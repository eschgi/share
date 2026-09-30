import { formatBytes } from '../format';
import { useI18n } from '../i18n';
import { FilePicker } from '../screens/FilePicker';
import type { Tile } from '../uploader';
import { Icon } from './Icon';

interface Props {
  ghosts: Tile[];
  onFiles: (files: File[]) => void;
  /** For files that can't be found any more: send the rest without them. */
  onSkip: () => void;
}

/** The files the browser couldn't keep when the page closed, and a button to pick them again. */
export function GhostCard({ ghosts, onFiles, onSkip }: Props) {
  const { t, tn, lang } = useI18n();
  const n = ghosts.length;
  // Offering their types first opens the phone's videos (or photos) straight away.
  const accept = [...new Set(ghosts.map((g) => g.type).filter(Boolean))].join(',');
  return (
    <div class="ghostcard">
      <div class="gh-t">
        <Icon name="alert" />
        {tn(ghosts.every((g) => g.kind === 'video') ? 'ghosts.videos' : 'ghosts.files', n)}
      </div>
      <p>{tn('ghosts.why', n)}</p>
      {ghosts.map((g) => (
        <div class="ghrow" key={g.id}>
          <div class="ghthumb">
            <Icon name={g.kind === 'video' ? 'video' : g.kind === 'photo' ? 'image' : 'file'} />
          </div>
          <div>
            <b>{g.name}</b>
            <span>
              {g.uploaded > 0 && g.size > 0
                ? t('ghosts.partlySent', { size: formatBytes(g.size, lang), pct: Math.floor((g.uploaded / g.size) * 100) })
                : t('ghosts.notStarted', { size: formatBytes(g.size, lang) })}
            </span>
          </div>
        </div>
      ))}
      <FilePicker label={tn('ghosts.pick', n)} look="tonal sm" accept={accept} onFiles={onFiles} />
      <button type="button" class="small link" onClick={onSkip}>
        {tn('ghosts.skip', n)}
      </button>
    </div>
  );
}
