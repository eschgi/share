import './fonts';
import './styles.css';
import { render } from 'preact';
import { JoinPage } from './screens/JoinPage';

render(<JoinPage />, document.getElementById('app')!);
