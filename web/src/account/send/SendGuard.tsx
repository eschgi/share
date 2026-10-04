import { useEffect, useRef } from 'preact/hooks';
import { DropZone } from '../../components/DropZone';
import { useLeaveWarning, useWakeLock } from '../../device';
import { useI18n } from '../../i18n';
import { navigate } from '../../router';
import { useAccount } from '../context';
import { dropTarget } from '../folders/model';
import { foldersNow, useFolders } from '../folders/store';
import { hold, sendFiles, sendMore, unfinished, useSender } from './sender';

/**
 * Sending, on every page of the account: closing the page asks first while files are on their
 * way, the screen stays on, files dropped anywhere are sent, and an upload a closed page
 * interrupted is mentioned where the person is. Apart from the pages, so its redraws while
 * files go out stay small.
 */
export function SendGuard() {
  const { t, tn } = useI18n();
  const { toast } = useAccount();
  const sending = useSender();
  const folders = useFolders();
  const busy = unfinished(sending);
  useLeaveWarning(busy);
  useWakeLock(busy);

  const { screen, restored } = sending.state;
  useEffect(() => {
    if (restored && !location.pathname.startsWith('/send')) {
      toast({ text: t('welcome.lead'), action: { label: t('save.show'), run: () => navigate('/send') } });
    }
  }, [restored]);

  // Files waiting for a folder, while the person is elsewhere; and a folder that went.
  const waiting = sending.pending > 0;
  useEffect(() => {
    if (waiting && !location.pathname.startsWith('/send')) {
      toast({ text: tn('send.waiting', sending.pending), action: { label: t('save.show'), run: () => navigate('/send') } });
    }
  }, [waiting]);
  const gone = useRef(sending.foldersGone);
  useEffect(() => {
    if (sending.foldersGone === gone.current) return;
    gone.current = sending.foldersGone;
    toast({ text: t('send.folderGone') });
  }, [sending.foldersGone]);

  const toSend = (files: File[]) => {
    if (files.length === 0) return;
    if (screen === 'done') sendMore();
    const now = foldersNow();
    const to = dropTarget(location.pathname, folders.list ?? [], now.shown, now.sendTo);
    if (to === 'ask') hold(files, []);
    else sendFiles(files, to);
    if (!location.pathname.startsWith('/send')) navigate('/send');
  };
  let onFiles: ((files: File[]) => void) | null = toSend;
  let label = t('drop.send');
  if (screen === 'sending') label = t('drop.add');
  else if (screen === 'welcome') {
    // Only the files to pick again, as on screen 5.
    if (sending.snapshot?.ghosts.length) label = t('drop.pickAgain');
    else onFiles = null;
  } else if (screen === 'boot' || screen === 'pin') onFiles = null;
  return <DropZone onFiles={onFiles} label={label} />;
}
