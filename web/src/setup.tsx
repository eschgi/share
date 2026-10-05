import './fonts';
import './styles.css';
import { render } from 'preact';
import { applyTheme, watchTheme } from './theme';
import { SetupPage } from './screens/SetupPage';

applyTheme();
watchTheme();
render(<SetupPage />, document.getElementById('app')!);
