import type { ComponentChildren } from 'preact';
import { useId, useLayoutEffect, useRef } from 'preact/hooks';
import { Icon } from '../../components/Icon';
import { useI18n } from '../../i18n';
import { useOverlay } from '../../router';
import { lockScroll } from '../scroll';

interface Props {
  title: string;
  onClose: () => void;
  wide?: boolean;
  children: ComponentChildren;
}

/**
 * A dialog: a sheet from the bottom on a phone, as in the app, and a box in the middle on
 * bigger screens. Render it only while open. It is a real <dialog>, so the page behind is out
 * of reach and Escape closes it; Back closes it too, and the focus goes back where it was.
 */
export function Modal({ title, onClose, wide, children }: Props) {
  const { t } = useI18n();
  const ref = useRef<HTMLDialogElement>(null);
  const id = useId();
  useOverlay(true, onClose);
  useLayoutEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    const unlock = lockScroll();
    ref.current?.showModal();
    return () => {
      unlock();
      opener?.focus?.();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      class={`modal${wide ? ' wide' : ''}`}
      aria-labelledby={id}
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
    >
      <div class="modal-scrim" onClick={onClose} />
      <div class="modal-panel">
        <div class="grip" />
        <header class="modal-head">
          <h2 id={id}>{title}</h2>
          <button type="button" class="ib" aria-label={t('common.close')} onClick={onClose}>
            <Icon name="x" />
          </button>
        </header>
        {children}
      </div>
    </dialog>
  );
}

interface ConfirmProps {
  title: string;
  body: string;
  confirm: string;
  /** A red button, for what can't be taken back. */
  danger?: boolean;
  busy?: boolean;
  problem?: string | null;
  onConfirm: () => void;
  onClose: () => void;
}

/** Asks before doing something that affects others or can't be undone, as the app does. */
export function Confirm({ title, body, confirm, danger, busy, problem, onConfirm, onClose }: ConfirmProps) {
  const { t } = useI18n();
  return (
    <Modal title={title} onClose={onClose}>
      <p class="modal-text">{body}</p>
      {problem && (
        <p class="help err" role="alert">
          <Icon name="alert" />
          {problem}
        </p>
      )}
      <div class="dbtns">
        <button type="button" class="tbtn" onClick={onClose}>
          {t('common.cancel')}
        </button>
        <button type="button" class={`btn sm ${danger ? 'danger' : 'primary'}`} disabled={busy} onClick={onConfirm}>
          {confirm}
        </button>
      </div>
    </Modal>
  );
}
