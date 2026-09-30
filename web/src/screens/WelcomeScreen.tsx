import { Brand } from '../components/Brand';
import { GhostCard } from '../components/GhostCard';
import { Icon } from '../components/Icon';
import { formatBytes, formatCount } from '../format';
import { useI18n } from '../i18n';
import type { Snapshot } from '../uploader';

interface Props {
  name: string;
  snapshot: Snapshot;
  onFiles: (files: File[]) => void;
  onSkipGhosts: () => void;
  onContinue: () => void;
  onStartOver: () => void;
}

/** Screen 5: an upload the closed page interrupted came back and can go on. */
export function WelcomeScreen({ name, snapshot: s, onFiles, onSkipGhosts, onContinue, onStartOver }: Props) {
  const { t, lang } = useI18n();
  const pct = s.bytesTotal > 0 ? (s.bytesDone / s.bytesTotal) * 100 : 0;
  return (
    <main class="screen">
      <Brand name={name} />
      <div class="roundico sm">
        <Icon name="rotate" />
      </div>
      <h1 class="hero md">{t('welcome.title')}</h1>
      <p class="lead">{t('welcome.lead')}</p>
      <div class="card prog">
        <div class="prow">
          <b>{t('welcome.sent', { done: formatCount(s.done, lang), total: formatCount(s.total, lang) })}</b>
          <span>
            {t('welcome.waiting', { n: formatCount(s.total - s.done, lang), bytes: formatBytes(s.bytesTotal - s.bytesDone, lang) })}
          </span>
        </div>
        <div class="bar" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(pct)}>
          <i style={{ width: `${pct}%` }} />
        </div>
      </div>
      {s.ghosts.length > 0 && <GhostCard ghosts={s.ghosts} onFiles={onFiles} onSkip={onSkipGhosts} />}
      <div class="grow" />
      <button type="button" class="btn primary" onClick={onContinue}>
        {t('welcome.continue')}
      </button>
      <button type="button" class="small link" onClick={onStartOver}>
        {t('welcome.startOver')}
      </button>
    </main>
  );
}
