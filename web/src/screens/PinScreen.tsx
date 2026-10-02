import { useEffect, useRef, useState } from 'preact/hooks';
import { Icon } from '../components/Icon';
import { Page } from '../components/Page';
import { formatWait } from '../format';
import { useI18n } from '../i18n';
import { cleanPinInput, pinLength } from '../pin';
import type { PinProblem } from '../state';

interface Props {
  name: string;
  problem: PinProblem | null;
  unlocking: boolean;
  onSubmit: (code: string) => void;
}

/** The boxes turn red only when the PIN itself didn't work, not when the browser or the session is the problem. */
function pinWasWrong(problem: PinProblem | null): boolean {
  switch (problem?.kind) {
    case 'wrong':
    case 'locked':
    case 'ended':
    case 'network':
      return true;
    default:
      return false;
  }
}

/** Screen 1: five boxes, not case-sensitive, unlocks as soon as the fifth character is in. */
export function PinScreen({ name, problem, unlocking, onSubmit }: Props) {
  const { t, tn, lang } = useI18n();
  const [value, setValue] = useState('');
  const [focused, setFocused] = useState(false);
  const [, setTick] = useState(0);
  const input = useRef<HTMLInputElement>(null);

  // Read the clock on every render; the timer only re-renders for the countdown.
  const now = Date.now();
  const lockedUntil = problem?.kind === 'locked' ? problem.until : 0;
  const locked = lockedUntil > now;

  useEffect(() => {
    if (!lockedUntil) return;
    const id = setInterval(() => {
      setTick((n) => n + 1);
      if (Date.now() >= lockedUntil) clearInterval(id);
    }, 1000);
    return () => clearInterval(id);
  }, [lockedUntil]);

  // After a wrong try, start over with empty boxes.
  useEffect(() => {
    if (problem?.kind === 'wrong' || problem?.kind === 'ended' || problem?.kind === 'locked' || problem?.kind === 'noCookie') {
      setValue('');
      if (input.current) input.current.value = '';
      input.current?.focus();
    }
  }, [problem]);

  const submit = (code: string) => {
    if (code.length === pinLength && !unlocking && !locked) onSubmit(code);
  };

  let message: string | null = null;
  switch (problem?.kind) {
    case 'wrong':
      message = problem.attemptsLeft === null ? t('pin.wrongPlain') : tn('pin.wrong', problem.attemptsLeft);
      break;
    case 'locked':
      message = locked ? t('pin.locked', { time: formatWait((lockedUntil - now) / 1000, lang) }) : null;
      break;
    case 'ended':
      message = t('pin.ended');
      break;
    case 'sessionEnded':
      message = t('pin.sessionEnded');
      break;
    case 'sessionLost':
      message = t('pin.sessionLost');
      break;
    case 'noCookie':
      message = t('pin.noCookie');
      break;
    case 'network':
      message = t('pin.network');
      break;
  }

  return (
    <Page name={name} languageSwitch>
      <div class="roundico">
        <Icon name="lock" />
      </div>
      <h1 class="hero">{t('pin.title')}</h1>
      <p class="lead">{t('pin.lead')}</p>
      <label class={`pin ${message && pinWasWrong(problem) ? 'err' : ''}`}>
        <input
          ref={input}
          class="pin-input"
          value={value}
          onInput={(e) => {
            const v = cleanPinInput(e.currentTarget.value);
            e.currentTarget.value = v;
            setValue(v);
            if (v.length === pinLength) submit(v);
          }}
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          autoComplete="one-time-code"
          autoCapitalize="characters"
          autoCorrect="off"
          spellcheck={false}
          inputMode="text"
          enterKeyHint="go"
          maxLength={12}
          aria-label={t('pin.title')}
          disabled={unlocking || locked}
          autoFocus
        />
        {Array.from({ length: pinLength }, (_, i) => (
          <b key={i} class={focused && i === Math.min(value.length, pinLength - 1) ? 'focus' : ''}>
            {value[i] ?? ''}
          </b>
        ))}
      </label>
      {message ? (
        <p class="help err" role="alert">
          <Icon name="alert" />
          {message}
        </p>
      ) : (
        <p class="help">{t('pin.help')}</p>
      )}
      <div class="grow" />
      <button type="button" class="btn primary" disabled={value.length !== pinLength || unlocking || locked} onClick={() => submit(value)}>
        <Icon name="lock-open" />
        {t('pin.unlock')}
      </button>
      <p class="small">{t('pin.link')}</p>
      <a class="small link" href="/sign-in">
        {t('pin.signIn')}
      </a>
    </Page>
  );
}
