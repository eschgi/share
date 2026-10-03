import { useEffect, useState } from 'preact/hooks';
import { ApiError, createPin, endPin, newPinCode, suggestPin, type PinInfo } from '../../api';
import { Bold } from '../../components/Bits';
import { Icon } from '../../components/Icon';
import { QrCode } from '../../components/QrCode';
import { formatWhen } from '../../format';
import { useI18n, type Lang } from '../../i18n';
import { cleanPinInput, pinLength } from '../../pin';
import { Switch } from '../components/Bits';
import { Confirm, Modal } from '../components/Modal';
import { useAccount } from '../context';
import { hasChoices } from '../folders/folders';
import { FolderField, FolderPicker } from '../folders/Folders';
import { useFolders } from '../folders/store';
import { copyText, sharesLinks, shareText } from './share';

/** "12 Sep", "12. Sept.", "12 set": when a permanent PIN was made, the day first as in the app. */
function shortDate(when: Date, lang: Lang): string {
  const year = when.getFullYear() !== new Date().getFullYear() ? 'numeric' : undefined;
  return new Intl.DateTimeFormat(lang === 'en' ? 'en-GB' : lang, { day: 'numeric', month: 'short', year }).format(when);
}

/** A PIN's code, a box per character. */
export function CodeBoxes({ code }: { code: string }) {
  return (
    <span class="code" translate={false}>
      {[...code].map((c, i) => (
        <b key={i}>{c}</b>
      ))}
    </span>
  );
}

type Asking = { kind: 'newCode' | 'end'; pin: PinInfo } | null;

/** Screens 18 and 33: the PINs that work now, permanent ones first; and a new one (34). bare:
 * only the cards, on a folder's page. */
export function PinsList({ pins, onChanged, bare }: { pins: PinInfo[]; onChanged: () => Promise<void>; bare?: boolean }) {
  const { t } = useI18n();
  const { toast } = useAccount();
  const folders = useFolders();
  // The folder of each card, where there are several, but not on a folder's own page.
  const folderOf = (p: PinInfo) => (!bare && hasChoices(folders.list) ? folders.byId(p.folder)?.name : undefined);
  const [qr, setQr] = useState<PinInfo | null>(null);
  const [asking, setAsking] = useState<Asking>(null);
  const [busy, setBusy] = useState(false);
  const shares = sharesLinks();

  const handOn = async (p: PinInfo) => {
    if (shares) await shareText(t('pins.shareText', { link: p.link }));
    else toast({ text: t((await copyText(p.link)) ? 'common.copied' : 'common.notCopied') });
  };

  const change = async (a: NonNullable<Asking>) => {
    setBusy(true);
    try {
      if (a.kind === 'newCode') await newPinCode(a.pin.id);
      else await endPin(a.pin.id);
    } catch {
      toast({ text: t('common.failed') });
    }
    setBusy(false);
    setAsking(null);
    await onChanged();
  };

  const permanent = pins.filter((p) => p.kind === 'permanent');
  const day = pins.filter((p) => p.kind === 'day');
  const card = (p: PinInfo) => (
    <PinCard
      key={p.id}
      pin={p}
      folder={folderOf(p)}
      shares={shares}
      onHandOn={() => void handOn(p)}
      onQr={() => setQr(p)}
      onAsk={(kind) => setAsking({ kind, pin: p })}
    />
  );
  return (
    <>
      {!bare && (
        <p class="lead sm">
          <Bold text={t(pins.some((p) => p.shows_folder) ? 'pins.leadShows' : 'pins.lead')} />
        </p>
      )}
      {!bare && pins.length === 0 && <p class="help">{t('pins.none')}</p>}
      <div class="pgrid">
        {permanent.length > 0 && (
          <div>
            <p class="glabel">{t('pins.permanent')}</p>
            {permanent.map(card)}
          </div>
        )}
        {day.length > 0 && (
          <div>
            <p class="glabel">{t('pins.day')}</p>
            {day.map(card)}
          </div>
        )}
      </div>
      {qr && <PinQr pin={qr} onClose={() => setQr(null)} />}
      {asking && (
        <Confirm
          title={t(asking.kind === 'newCode' ? 'pins.newCodeTitle' : 'pins.endTitle', { code: asking.pin.code })}
          body={t(asking.kind === 'newCode' ? 'pins.newCodeBody' : 'pins.endBody')}
          confirm={t(asking.kind === 'newCode' ? 'pins.newCode' : 'pins.endNow')}
          danger
          busy={busy}
          onConfirm={() => void change(asking)}
          onClose={() => setAsking(null)}
        />
      )}
    </>
  );
}

interface CardProps {
  pin: PinInfo;
  /** Its folder's name, where there are several. */
  folder?: string;
  shares: boolean;
  onHandOn: () => void;
  onQr: () => void;
  onAsk: (kind: 'newCode' | 'end') => void;
}

function PinCard({ pin, folder, shares, onHandOn, onQr, onAsk }: CardProps) {
  const { t, tn, lang } = useI18n();
  const permanent = pin.kind === 'permanent';
  return (
    <div class="pincard">
      <div class="pc-top">
        <CodeBoxes code={pin.code} />
        {permanent && <Icon name="infinity" class="pcico" />}
      </div>
      {(folder || pin.shows_folder) && (
        <div class="pc-tags">
          {folder && (
            <span class="ftag">
              <Icon name="folder" />
              {folder}
            </span>
          )}
          {pin.shows_folder && (
            <span class="ftag">
              <Icon name="eye" />
              {t('pins.showsFolder')}
            </span>
          )}
        </div>
      )}
      <div class="pc-meta">
        {permanent ? (
          `${t('pins.since', { date: shortDate(new Date(pin.created_at), lang) })} · ${tn('pins.usedOn', pin.phones)}`
        ) : (
          <>
            {pin.expires_at && (
              <span class="timepill sm">
                <Icon name="clock" />
                {t('pins.ends', { when: formatWhen(new Date(pin.expires_at), new Date(), lang) })}
              </span>
            )}
            {pin.files === 0 ? tn('pins.files', 0) : `${tn('pins.files', pin.files)} ${tn('pins.fromDevices', pin.phones)}`}
          </>
        )}
      </div>
      <div class="pc-actions">
        <button type="button" onClick={onHandOn}>
          <Icon name={shares ? 'share' : 'copy'} />
          {t(shares ? 'pins.share' : 'pins.copy')}
        </button>
        <button type="button" onClick={onQr}>
          <Icon name="qr" />
          {t('pins.qr')}
        </button>
        {permanent && (
          <button type="button" onClick={() => onAsk('newCode')}>
            <Icon name="refresh" />
            {t('pins.newCode')}
          </button>
        )}
        <button type="button" class="danger" onClick={() => onAsk('end')}>
          {t('pins.endNow')}
        </button>
      </div>
    </div>
  );
}

/** A PIN as a QR code, for someone standing next to you. */
function PinQr({ pin, onClose }: { pin: PinInfo; onClose: () => void }) {
  const { t } = useI18n();
  return (
    <Modal title={t('pins.scanToSend')} onClose={onClose}>
      <div class="pinqr">
        <QrCode text={pin.link} label={pin.link} />
        <CodeBoxes code={pin.code} />
        <span class="small">{pin.link}</span>
      </div>
    </Modal>
  );
}

/** Screen 34: how long the PIN works, and its code: made up, or typed. It sends into folder,
 * or the folder sending goes into. */
export function NewPinDialog({ folder, onCreated, onClose }: { folder?: string; onCreated: (pin: PinInfo) => void; onClose: () => void }) {
  const { t, lang } = useI18n();
  const { toast } = useAccount();
  const folders = useFolders();
  const [chosen, setChosen] = useState(folder ?? null);
  const into = chosen ?? folders.sendTo?.id;
  const [shows, setShows] = useState(false);
  const [kind, setKind] = useState<PinInfo['kind']>('day');
  const [code, setCode] = useState('');
  const [focused, setFocused] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const shares = sharesLinks();

  const suggest = async () => {
    try {
      setCode((await suggestPin()).code);
      setProblem(null);
    } catch {
      // typing one works too
    }
  };
  useEffect(() => void suggest(), []);

  const create = async () => {
    if (code.length !== pinLength) return setProblem(t('pins.badCode'));
    if (!into) return setProblem(t('common.offline'));
    setBusy(true);
    setProblem(null);
    let pin: PinInfo;
    try {
      pin = await createPin(kind, code, into, shows);
    } catch (e) {
      setBusy(false);
      if (!(e instanceof ApiError) || e.status === 0) return setProblem(t('common.offline'));
      return setProblem(t(e.code === 'pin_taken' ? 'pins.taken' : e.code === 'pin_format' ? 'pins.badCode' : 'common.failed'));
    }
    // Made: its link goes on, as the button says.
    if (shares) await shareText(t('pins.shareText', { link: pin.link }));
    else toast({ text: t((await copyText(pin.link)) ? 'pins.madeCopied' : 'pins.made', { code: pin.code }) });
    onCreated(pin);
  };

  const tomorrow = new Date(Date.now() + 86_400_000);
  return (
    <Modal title={t('pins.newTitle')} onClose={onClose}>
      {hasChoices(folders.list) && (
        <FolderField label={t('pins.sendsInto')}>
          <FolderPicker list={folders.list!} value={folders.byId(into ?? '') ?? null} onChange={setChosen} title={t('pins.sendsInto')} />
        </FolderField>
      )}
      <p class={`label${hasChoices(folders.list) ? '' : ' first'}`}>{t('pins.howLong')}</p>
      <div class="opts" role="radiogroup" aria-label={t('pins.howLong')}>
        {(
          [
            ['permanent', 'infinity', t('pins.permanent'), t('pins.permanentDetail')],
            ['day', 'clock', t('pins.day24'), t('pins.until', { when: formatWhen(tomorrow, new Date(), lang) })],
          ] as const
        ).map(([k, icon, title, detail]) => (
          <button key={k} type="button" role="radio" aria-checked={kind === k} class={`opt${kind === k ? ' on' : ''}`} onClick={() => setKind(k)}>
            <Icon name={icon} />
            <i class="radio" />
            <b>{title}</b>
            <span class="sub">{detail}</span>
          </button>
        ))}
      </div>
      <button type="button" class="swrow" role="switch" aria-checked={shows} onClick={() => setShows(!shows)}>
        <span class="rt">
          <b>{t('pins.guestsSee')}</b>
          <span>{t('pins.guestsSeeHelp')}</span>
        </span>
        <Switch on={shows} />
      </button>
      <label class="label" for="new-pin">
        {t('pins.codeLabel')}
      </label>
      <div class="coderow">
        <span class={`pin code-in${problem ? ' err' : ''}`}>
          <input
            id="new-pin"
            class="pin-input"
            value={code}
            maxLength={12}
            autoComplete="off"
            autoCapitalize="characters"
            autoCorrect="off"
            spellcheck={false}
            enterKeyHint="done"
            onInput={(e) => {
              const v = cleanPinInput(e.currentTarget.value);
              e.currentTarget.value = v;
              setCode(v);
              setProblem(null);
            }}
            onFocus={() => setFocused(true)}
            onBlur={() => setFocused(false)}
            onKeyDown={(e) => e.key === 'Enter' && void create()}
          />
          {Array.from({ length: pinLength }, (_, i) => (
            <b key={i} class={focused && i === Math.min(code.length, pinLength - 1) ? 'focus' : ''}>
              {code[i] ?? ''}
            </b>
          ))}
        </span>
        <button type="button" class="ib tonal" aria-label={t('pins.another')} title={t('pins.another')} onClick={() => void suggest()}>
          <Icon name="refresh" />
        </button>
      </div>
      {problem ? (
        <p class="help err" role="alert">
          <Icon name="alert" />
          {problem}
        </p>
      ) : (
        <p class="help">{t('pins.madeUp')}</p>
      )}
      <div class="dbtns">
        <button type="button" class="tbtn" onClick={onClose}>
          {t('common.cancel')}
        </button>
        <button type="button" class="btn sm primary" disabled={busy} onClick={() => void create()}>
          <Icon name={shares ? 'share' : 'copy'} />
          {t(shares ? 'pins.createShare' : 'pins.createCopy')}
        </button>
      </div>
    </Modal>
  );
}
