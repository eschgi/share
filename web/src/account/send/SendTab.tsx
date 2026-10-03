import { useEffect, useState } from 'preact/hooks';
import { InShell } from '../../components/Page';
import { useI18n } from '../../i18n';
import { shareFailed } from '../../incoming';
import { DoneScreen } from '../../screens/DoneScreen';
import { ReadyScreen } from '../../screens/ReadyScreen';
import { SendingScreen } from '../../screens/SendingScreen';
import { WelcomeScreen } from '../../screens/WelcomeScreen';
import { useAccount } from '../context';
import { hasChoices } from '../folders/folders';
import { useFolders } from '../folders/store';
import { Shell, TitleBar } from '../Shell';
import { continueRestored, retryFailed, sendFiles, sendMore, skipGhosts, startOver, useSender } from './sender';
import { PendingSend, SendInto } from './SendInto';

/** Screen 29: sending without a PIN, with the PIN pages' own screens inside the frame. */
export function SendTab() {
  const { t } = useI18n();
  const { info } = useAccount();
  const { state, snapshot, rejected, pending } = useSender();
  const folders = useFolders();
  const [online, setOnline] = useState(navigator.onLine);
  useEffect(() => {
    const on = () => setOnline(true);
    const off = () => setOnline(false);
    addEventListener('online', on);
    addEventListener('offline', off);
    return () => {
      removeEventListener('online', on);
      removeEventListener('offline', off);
    };
  }, []);

  const name = info?.name ?? 'Share';
  let screen;
  switch (state.screen) {
    case 'ready':
      screen = (
        <ReadyScreen
          name={name}
          session={state.session!}
          onFiles={(files) => sendFiles(files)}
          shareFailed={shareFailed}
          into={pending > 0 ? <PendingSend count={pending} /> : hasChoices(folders.list) ? <SendInto /> : undefined}
        />
      );
      break;
    case 'welcome':
      screen = (
        <WelcomeScreen
          name={name}
          snapshot={snapshot!}
          onFiles={(files) => sendFiles(files)}
          onSkipGhosts={skipGhosts}
          onContinue={continueRestored}
          onStartOver={startOver}
        />
      );
      break;
    case 'sending':
      screen = (
        <SendingScreen
          name={name}
          snapshot={snapshot!}
          online={online}
          rejected={rejected}
          onFiles={(files) => sendFiles(files)}
          onSkipGhosts={skipGhosts}
          onRetry={retryFailed}
        />
      );
      break;
    case 'done':
      screen = <DoneScreen name={name} files={state.done!.files} bytes={state.done!.bytes} offerInstall onMore={sendMore} />;
      break;
    default:
      screen = <p class="lead send-wait">{t('common.loading')}</p>;
  }
  return (
    <Shell tab="send">
      <TitleBar title={t('nav.send')} />
      <InShell.Provider value={true}>{screen}</InShell.Provider>
    </Shell>
  );
}
