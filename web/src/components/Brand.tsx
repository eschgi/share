import { useEffect, useRef, useState } from 'preact/hooks';
import { useI18n } from '../i18n';
import { Icon } from './Icon';

/** The header: logo and name, and on the first screens the language switch. */
export function Brand({ name, languageSwitch }: { name: string; languageSwitch?: boolean }) {
  return (
    <header class="brand">
      <Icon name="images" />
      <span translate={false}>{name}</span>
      {languageSwitch && <LanguageSwitch />}
    </header>
  );
}

function LanguageSwitch() {
  const { lang, offered, setLang, t } = useI18n();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const close = (e: Event) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('pointerdown', close);
    return () => document.removeEventListener('pointerdown', close);
  }, [open]);

  if (offered.length < 2) return null;
  return (
    <div class="lang" ref={ref}>
      <button type="button" class="langpill" aria-label={t('language')} aria-expanded={open} onClick={() => setOpen(!open)}>
        <Icon name="globe" />
        {lang.toUpperCase()}
      </button>
      {open && (
        <ul class="langmenu" role="menu">
          {offered.map((l) => (
            <li key={l}>
              <button
                type="button"
                role="menuitemradio"
                aria-checked={l === lang}
                lang={l}
                onClick={() => {
                  setLang(l);
                  setOpen(false);
                }}
              >
                {t(`lang.${l}`)}
                {l === lang && <Icon name="check" />}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
