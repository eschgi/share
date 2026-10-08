import './print.css';
import { render } from 'preact';
import { useLayoutEffect, useMemo, useState } from 'preact/hooks';
import type { PinInfo } from '../../api';
import { Icon } from '../../components/Icon';
import { QrCode } from '../../components/QrCode';
import { dayOf, formatLongDay } from '../../format';
import { isLang, translate, useI18n } from '../../i18n';
import { Switch } from '../components/Bits';
import { Modal } from '../components/Modal';
import { useAccount } from '../context';
import { useFolders } from '../folders/store';
import { longTitle, pageTexts, type Printed, type PrintKind } from './print';

/** Screens 71 and 72: the A4 page, a poster or four cards to cut out, in the server's language.
 * Its sizes follow its width, so the preview and the printed page are the same page. */
export function PrintPage({ p }: { p: Printed }) {
  const texts = pageTexts(p);
  const title = () => <div class={`pt${longTitle(p.title) ? ' long' : ''}`}>{p.title}</div>;
  const line = () => p.line && <div class="pline">{p.line}</div>;
  const qr = () => (
    <div class="pqr">
      <QrCode text={p.link} label={p.link} />
    </div>
  );
  const code = () =>
    p.code && (
      <span class="pcode" translate={false}>
        {[...p.code].map((c, i) => (
          <b key={i}>{c}</b>
        ))}
      </span>
    );
  if (p.kind === 'cards') {
    return (
      <div class="paper cards" lang={p.lang}>
        <div class="psheet">
          {[0, 1, 2, 3].map((i) => (
            <div key={i} class="tcard">
              {title()}
              {line()}
              {qr()}
              <div class="pscan">{texts.scan}</div>
              {code()}
            </div>
          ))}
        </div>
      </div>
    );
  }
  return (
    <div class="paper" lang={p.lang}>
      <div class="psheet">
        <div class="pbrand">
          <Icon name="images" />
          <span translate={false}>{p.brand}</span>
        </div>
        {title()}
        {texts.date && <div class="pdate">{texts.date}</div>}
        {line()}
        {qr()}
        <div class="pscan">{texts.scan}</div>
        <div class="psteps">
          {texts.steps.map((step, i) => (
            <div key={i}>
              <b>{i + 1}</b>
              {step}
            </div>
          ))}
        </div>
        <p class="pnote">{texts.note}</p>
        {p.code && <div class="por">{texts.orType}</div>}
        {code()}
      </div>
    </div>
  );
}

/** The page as it is printed: on its own at the end of the page, which print.css shows alone
 * while printing. */
function PrintSheet({ p }: { p: Printed }) {
  const sheet = useMemo(() => document.createElement('div'), []);
  useLayoutEffect(() => {
    sheet.className = 'print-sheet';
    document.body.append(sheet);
    return () => {
      render(null, sheet);
      sheet.remove();
      document.documentElement.classList.remove('printing');
    };
  }, []);
  useLayoutEffect(() => render(<PrintPage p={p} />, sheet));
  return null;
}

/** Screens 69 and 70: a PIN printed for guests, so nobody has to send them a link, with a preview
 * that follows every change. The page is in the server's language, the one guests see first. */
export function PrintDialog({ pin, link, onClose }: { pin: PinInfo; link: string; onClose: () => void }) {
  const { t, lang: own } = useI18n();
  const { info } = useAccount();
  const folders = useFolders();
  const fromServer = info?.default_language;
  const lang = isLang(fromServer) ? fromServer : own;
  const [kind, setKind] = useState<PrintKind>('poster');
  const [title, setTitle] = useState(() => folders.byId(pin.folder)?.name ?? info?.name ?? '');
  const [line, setLine] = useState(() => translate(lang, 'print.line'));
  const [dated, setDated] = useState(true);
  const [day, setDay] = useState(() => dayOf(new Date()));
  const [withCode, setWithCode] = useState(true);

  const printed: Printed = {
    kind,
    title: title.trim(),
    line: line.trim(),
    day: kind === 'poster' && dated && day ? day : null,
    code: withCode ? pin.code : null,
    link,
    brand: info?.name ?? 'Share',
    lang,
    showsFolder: pin.shows_folder,
  };

  const print = () => {
    // Only the sheet is printed while this is on (print.css); afterprint takes it off.
    document.documentElement.classList.add('printing');
    addEventListener('afterprint', () => document.documentElement.classList.remove('printing'), { once: true });
    window.print();
  };

  return (
    <Modal title={t('print.title', { code: pin.code })} className="print" onClose={onClose}>
      <div class="pedit">
        <div class="pform">
          <div role="radiogroup" aria-label={t('print.what')}>
            {(['poster', 'cards'] as const).map((k) => (
              <label key={k} class={`dopt${kind === k ? ' on' : ''}`}>
                <input type="radio" name="print-kind" class="sr-only" checked={kind === k} onChange={() => setKind(k)} />
                <span class="dt">
                  <b>{t(`print.${k}`)}</b>
                  <span>{t(`print.${k}Detail`)}</span>
                </span>
                <i class="radio" />
              </label>
            ))}
          </div>
          <label class="label" for="print-title">
            {t('print.heading')}
          </label>
          <input id="print-title" class="input" value={title} maxLength={60} onInput={(e) => setTitle(e.currentTarget.value)} />
          <label class="label" for="print-line">
            {t('print.under')}
          </label>
          <input id="print-line" class="input" value={line} maxLength={100} onInput={(e) => setLine(e.currentTarget.value)} />
          {kind === 'poster' && (
            <>
              <button type="button" class="swrow" role="switch" aria-checked={dated} onClick={() => setDated(!dated)}>
                <span class="rt">
                  <b>{t('print.date')}</b>
                  <span>{dated && day ? formatLongDay(day, lang) : t('print.noDate')}</span>
                </span>
                <Switch on={dated} />
              </button>
              {dated && <input type="date" class="input pday" aria-label={t('print.date')} value={day} onInput={(e) => setDay(e.currentTarget.value)} />}
            </>
          )}
          <button type="button" class="swrow" role="switch" aria-checked={withCode} onClick={() => setWithCode(!withCode)}>
            <span class="rt">
              <b>{t('print.code')}</b>
              <span>{t('print.codeDetail')}</span>
            </span>
            <Switch on={withCode} />
          </button>
          {pin.secret && link === pin.link && (
            <p class="help err" role="alert">
              <Icon name="alert" />
              {t('pins.noKey')}
            </p>
          )}
          {lang !== own && <p class="help">{t('print.inLanguage', { language: t(`lang.${lang}`) })}</p>}
        </div>
        <div class="pprev" aria-hidden="true">
          <PrintPage p={printed} />
        </div>
      </div>
      <div class="dbtns">
        <button type="button" class="btn sm primary" onClick={print}>
          <Icon name="printer" />
          {t('print.go')}
        </button>
      </div>
      <PrintSheet p={printed} />
    </Modal>
  );
}
