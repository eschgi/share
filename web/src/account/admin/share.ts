// Handing on a link, such as a PIN's or an invite's: phones share it with their share sheet, as
// the app does; computers copy it. Over plain http at home there is no clipboard API, so the
// old way of copying takes over.

/** Whether to offer sharing rather than copying. */
export function sharesLinks(): boolean {
  return typeof navigator.share === 'function' && matchMedia('(pointer: coarse)').matches;
}

/** Copies text; false if the browser wouldn't. */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // try the old way
  }
  const field = document.createElement('textarea');
  field.value = text;
  field.setAttribute('readonly', '');
  field.style.position = 'fixed';
  field.style.opacity = '0';
  document.body.append(field);
  field.select();
  let ok = false;
  try {
    ok = document.execCommand('copy');
  } catch {
    ok = false;
  }
  field.remove();
  return ok;
}

/** Opens the share sheet; false if the person closed it or it couldn't open. */
export async function shareText(text: string): Promise<boolean> {
  try {
    await navigator.share({ text });
    return true;
  } catch {
    return false;
  }
}
