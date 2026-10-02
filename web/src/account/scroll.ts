// While a dialog is open, the page behind it doesn't scroll. Where the page had a scrollbar,
// padding takes its place, so nothing behind the dialog moves sideways.

let locks = 0;
let saved = { overflow: '', paddingRight: '' };

/** Holds the page still; call the function it returns to let go. Dialogs on dialogs are fine. */
export function lockScroll(): () => void {
  const html = document.documentElement;
  if (locks++ === 0) {
    const bar = innerWidth - html.clientWidth;
    saved = { overflow: html.style.overflow, paddingRight: html.style.paddingRight };
    html.style.overflow = 'hidden';
    if (bar > 0) html.style.paddingRight = `${bar}px`;
  }
  let held = true;
  return () => {
    if (!held) return;
    held = false;
    if (--locks > 0) return;
    html.style.overflow = saved.overflow;
    html.style.paddingRight = saved.paddingRight;
  };
}
