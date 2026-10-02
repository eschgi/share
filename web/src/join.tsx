import './fonts';
import './styles.css';
import { render } from 'preact';
import { applyTheme, watchTheme } from './theme';
import { JoinPage } from './screens/JoinPage';

applyTheme();
watchTheme();
render(<JoinPage />, document.getElementById('app')!);
