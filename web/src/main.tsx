import './fonts';
import './styles.css';
import { render } from 'preact';
import { applyTheme, watchTheme } from './theme';
import { Root } from './root';
import { captureInstallPrompt } from './device';

captureInstallPrompt();
// The service worker keeps picked files across a reload, and the page itself for when the
// connection is gone. There is none while developing with Vite.
if (import.meta.env.PROD && 'serviceWorker' in navigator) {
  navigator.serviceWorker.register('/sw.js').catch(() => {});
}

applyTheme();
watchTheme();
render(<Root />, document.getElementById('app')!);
