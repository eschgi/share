// A small router: the address is the page, pages change with history.pushState, and Back and
// Forward work as on any site. Overlays (a dialog, the viewer, a selection) take a history
// entry of their own without changing the address, so Back closes them first, as on a phone.
import { useEffect, useRef, useState } from 'preact/hooks';

export interface Route {
  path: string;
  search: string;
}

const listeners = new Set<() => void>();
const changed = () => listeners.forEach((l) => l());

function current(): Route {
  return { path: location.pathname, search: location.search };
}

/** The current page; re-renders when it changes. */
export function useRoute(): Route {
  const [route, setRoute] = useState(current);
  useEffect(() => {
    const update = () => setRoute(current());
    listeners.add(update);
    return () => void listeners.delete(update);
  }, []);
  return route;
}

interface Overlay {
  id: number;
  close: () => void;
}

let overlays: Overlay[] = [];
let lastId = 0;
/** Pops this page caused itself (closing an overlay), which change nothing. */
let ownPops = 0;

/** Goes to another page. Open overlays close, and the new page takes their entry's place. */
export function navigate(to: string, { replace = false }: { replace?: boolean } = {}): void {
  if (overlays.length > 0) {
    closeAll();
    replace = true;
  }
  if (replace) history.replaceState({}, '', to);
  else history.pushState({}, '', to);
  scrollTo(0, 0);
  changed();
}

function closeAll(above = 0) {
  const gone = overlays.filter((o) => o.id > above);
  overlays = overlays.filter((o) => o.id <= above);
  for (const o of gone.reverse()) o.close();
}

/**
 * Gives an open overlay a history entry, so Back closes it (by calling close). The function it
 * returns is for when the overlay closes by itself: it takes the entry away again.
 */
export function openOverlay(close: () => void): () => void {
  const id = ++lastId;
  overlays.push({ id, close });
  history.pushState({ overlay: id }, '');
  return () => {
    const i = overlays.findIndex((o) => o.id === id);
    if (i < 0) return; // Back closed it, or a new page did
    const above = overlays.slice(i + 1);
    overlays = overlays.slice(0, i);
    for (const o of above.reverse()) o.close();
    ownPops++;
    history.go(-1 - above.length);
  };
}

/** While open, the overlay closes with Back (see openOverlay). */
export function useOverlay(open: boolean, onClose: () => void): void {
  const close = useRef(onClose);
  close.current = onClose;
  useEffect(() => {
    if (!open) return;
    return openOverlay(() => close.current());
  }, [open]);
}

if (typeof addEventListener === 'function') {
  addEventListener('popstate', (e: PopStateEvent) => {
    if (ownPops > 0) {
      ownPops--;
      return;
    }
    // Back, or Forward: the overlays above the entry we're on now are gone.
    const at = (e.state as { overlay?: number } | null)?.overlay;
    closeAll(typeof at === 'number' && overlays.some((o) => o.id === at) ? at : 0);
    changed();
  });
}

/** Whether a click should stay in this tab and be handled here (not a new tab or window). */
export function plainClick(e: MouseEvent): boolean {
  return e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey && !e.defaultPrevented;
}
