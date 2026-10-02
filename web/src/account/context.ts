// What the pages of people with an account share: the server, the person signed in here, and
// how to say something in a toast.
import { createContext } from 'preact';
import { useContext } from 'preact/hooks';
import type { Info, Me } from '../api';
import type { Lang } from '../i18n';

export interface ToastAction {
  label: string;
  run: () => void;
}

export interface Toast {
  text: string;
  action?: ToastAction;
}

export interface Account {
  info: Info | null;
  /** The person signed in here; pages under the shell always have one. */
  me: Me;
  /** Reads the person again, after a change such as a new password. */
  refreshMe: () => Promise<void>;
  toast: (t: Toast) => void;
  /** Whether the language follows the browser, and how to choose: a language, or "auto". */
  languageAuto: boolean;
  chooseLanguage: (l: Lang | 'auto') => void;
}

export const AccountContext = createContext<Account | null>(null);

export function useAccount(): Account {
  const a = useContext(AccountContext);
  if (!a) throw new Error('useAccount outside the account pages');
  return a;
}
