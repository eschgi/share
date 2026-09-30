// Latin subsets only: they cover English, German and Italian and most names; anything else
// falls back to the phone's own fonts.
import '@fontsource/noto-serif/latin-700.css';
import '@fontsource/noto-serif/latin-ext-700.css';
import '@fontsource/roboto/latin-400.css';
import '@fontsource/roboto/latin-ext-400.css';
import '@fontsource/roboto/latin-500.css';
import '@fontsource/roboto/latin-ext-500.css';
import '@fontsource/roboto/latin-700.css';
import '@fontsource/roboto/latin-ext-700.css';
import '@fontsource/roboto-mono/latin-600.css';
import './styles.css';
import { render } from 'preact';
import { App } from './app';
import { captureInstallPrompt } from './device';

captureInstallPrompt();
// The service worker keeps picked files across a reload, and the page itself for when the
// connection is gone. There is none while developing with Vite.
if (import.meta.env.PROD && 'serviceWorker' in navigator) {
  navigator.serviceWorker.register('/sw.js').catch(() => {});
}

render(<App />, document.getElementById('app')!);
