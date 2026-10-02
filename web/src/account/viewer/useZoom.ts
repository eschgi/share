// The viewer's gestures on the stage: swipes to the next or previous file, and for photos
// zooming in and moving around, with two fingers, a double tap or click, the mouse wheel or a
// trackpad's pinch, and the keys + - 0.
import type { RefObject } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { swipeStep } from './view';
import { clamp, clampScale, isDoubleTap, isZoomed, noZoom, pinch, toggle, wheelFactor, zoomAt, type Point, type Size, type Tap, type Zoom } from './zoom';

type Gesture =
  | { kind: 'none' }
  | { kind: 'swipe'; from: Point }
  | { kind: 'pan'; from: Point; z0: Zoom }
  | { kind: 'pinch'; z0: Zoom; d0: number; m0: Point };

interface Options {
  /** The file shown: each one starts without zoom. */
  id: string;
  /** Photos zoom; other files only swipe. */
  zoomable: boolean;
  /** A swipe while not zoomed in: 1 to the next file, -1 to the previous one. */
  onSwipe: (step: -1 | 1) => void;
}

export interface ZoomControl {
  zoom: Zoom;
  /** A finger, the mouse or the wheel is moving the photo: it follows without easing. */
  moving: boolean;
  /** Handles + - 0, and the arrows while zoomed in (they move the photo); true if it did. */
  key: (e: KeyboardEvent) => boolean;
}

/** Where the photo is: the middle of its space, its size at rest, and that space's size. */
function geometry(stage: HTMLElement | null): { center: Point; pic: Size; box: Size } | null {
  const box = stage?.querySelector<HTMLElement>('.vmedia');
  const pic = stage?.querySelector<HTMLElement>('.vpic:not(.loading)');
  if (!box || !pic) return null;
  const r = box.getBoundingClientRect();
  return { center: { x: r.left + r.width / 2, y: r.top + r.height / 2 }, pic: { w: pic.offsetWidth, h: pic.offsetHeight }, box: { w: r.width, h: r.height } };
}

const from = (p: Point, center: Point): Point => ({ x: p.x - center.x, y: p.y - center.y });
const middle = (a: Point, b: Point): Point => ({ x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 });
const distance = (a: Point, b: Point) => Math.hypot(a.x - b.x, a.y - b.y);

export function useZoom(stage: RefObject<HTMLElement>, { id, zoomable, onSwipe }: Options): ZoomControl {
  const [state, setState] = useState({ id, zoom: noZoom, moving: false });
  const zoom = state.id === id ? state.zoom : noZoom;
  const current = useRef(zoom);
  current.current = zoom;
  const options = useRef({ id, zoomable, onSwipe });
  options.current = { id, zoomable, onSwipe };

  const set = (z: Zoom, moving: boolean) => {
    current.current = z;
    setState({ id: options.current.id, zoom: z, moving });
  };
  /** Zooms as change says, kept possible; false if there is no photo to zoom. */
  const change = (moving: boolean, next: (z: Zoom, g: NonNullable<ReturnType<typeof geometry>>) => Zoom): boolean => {
    const g = geometry(stage.current);
    if (!g) return false;
    set(clamp(next(current.current, g), g.pic, g.box), moving);
    return true;
  };

  useEffect(() => {
    const el = stage.current;
    if (!el) return;
    const pointers = new Map<number, Point>();
    let gesture: Gesture = { kind: 'none' };
    let tapStart: Point | null = null;
    let lastTap: Tap | null = null;
    let lastType = '';
    const at = (e: { clientX: number; clientY: number }): Point => ({ x: e.clientX, y: e.clientY });
    const skip = (e: Event) => (e.target as Element).closest('video, audio, button, a') !== null;

    const down = (e: PointerEvent) => {
      lastType = e.pointerType;
      if (skip(e) || (e.pointerType === 'mouse' && e.button !== 0)) return;
      pointers.set(e.pointerId, at(e));
      const g = geometry(el);
      if (pointers.size === 1) {
        tapStart = at(e);
        if (options.current.zoomable && isZoomed(current.current)) {
          gesture = { kind: 'pan', from: at(e), z0: current.current };
          el.setPointerCapture(e.pointerId);
        } else {
          gesture = e.pointerType === 'touch' ? { kind: 'swipe', from: at(e) } : { kind: 'none' };
        }
      } else if (pointers.size === 2 && options.current.zoomable && g) {
        const [a, b] = [...pointers.values()];
        gesture = { kind: 'pinch', z0: current.current, d0: distance(a, b), m0: from(middle(a, b), g.center) };
        tapStart = null;
      } else {
        gesture = { kind: 'none' };
      }
    };

    const move = (e: PointerEvent) => {
      if (!pointers.has(e.pointerId)) return;
      pointers.set(e.pointerId, at(e));
      if (tapStart && distance(tapStart, at(e)) > 10) tapStart = null;
      const g = gesture;
      if (g.kind === 'pinch' && pointers.size >= 2) {
        const [a, b] = [...pointers.values()];
        change(true, (_, geo) => pinch(g.z0, g.d0, g.m0, distance(a, b), from(middle(a, b), geo.center)));
      } else if (g.kind === 'pan') {
        change(true, () => ({ ...g.z0, x: g.z0.x + e.clientX - g.from.x, y: g.z0.y + e.clientY - g.from.y }));
      }
    };

    const up = (e: PointerEvent) => {
      if (!pointers.delete(e.pointerId)) return;
      const g = gesture;
      const pageZoomed = visualViewport !== null && visualViewport.scale > 1.01; // the finger moves the page
      if (e.type === 'pointerup' && g.kind === 'swipe' && pointers.size === 0 && !pageZoomed) {
        const step = swipeStep(e.clientX - g.from.x, e.clientY - g.from.y);
        if (step) {
          gesture = { kind: 'none' };
          tapStart = lastTap = null;
          return options.current.onSwipe(step);
        }
      }
      // The mouse has dblclick; fingers count their taps here.
      if (e.type === 'pointerup' && tapStart && e.pointerType === 'touch' && pointers.size === 0 && options.current.zoomable) {
        const tap = { at: e.timeStamp, x: e.clientX, y: e.clientY };
        if (isDoubleTap(lastTap, tap)) {
          lastTap = null;
          change(false, (z, geo) => toggle(z, from(tap, geo.center)));
        } else {
          lastTap = tap;
        }
      }
      tapStart = null;
      if (g.kind === 'pinch' && pointers.size === 1 && isZoomed(current.current)) {
        gesture = { kind: 'pan', from: [...pointers.values()][0], z0: current.current }; // one finger stays on
      } else if (pointers.size === 0) {
        gesture = { kind: 'none' };
        if (g.kind === 'pan' || g.kind === 'pinch') set(current.current, false);
      }
    };

    const dblclick = (e: MouseEvent) => {
      if (lastType === 'touch' || !options.current.zoomable || skip(e)) return;
      change(false, (z, geo) => toggle(z, from(at(e), geo.center)));
    };

    const wheel = (e: WheelEvent) => {
      if (!options.current.zoomable) return;
      const zoomed = change(true, (z, geo) => zoomAt(z, clampScale(z.scale * wheelFactor(e.deltaY, e.deltaMode, e.ctrlKey)), from(at(e), geo.center)));
      if (zoomed) e.preventDefault();
    };

    el.addEventListener('pointerdown', down);
    el.addEventListener('pointermove', move);
    el.addEventListener('pointerup', up);
    el.addEventListener('pointercancel', up);
    el.addEventListener('dblclick', dblclick);
    el.addEventListener('wheel', wheel, { passive: false });
    return () => {
      el.removeEventListener('pointerdown', down);
      el.removeEventListener('pointermove', move);
      el.removeEventListener('pointerup', up);
      el.removeEventListener('pointercancel', up);
      el.removeEventListener('dblclick', dblclick);
      el.removeEventListener('wheel', wheel);
    };
  }, []);

  const key = (e: KeyboardEvent): boolean => {
    if (!zoomable || e.ctrlKey || e.metaKey || e.altKey) return false;
    const center = { x: 0, y: 0 };
    switch (e.key) {
      case '+':
      case '=':
        return change(false, (z) => zoomAt(z, clampScale(z.scale * 1.5), center));
      case '-':
        return change(false, (z) => zoomAt(z, clampScale(z.scale / 1.5), center));
      case '0':
        return change(false, () => noZoom);
    }
    const arrows: Record<string, Point> = { ArrowLeft: { x: 1, y: 0 }, ArrowRight: { x: -1, y: 0 }, ArrowUp: { x: 0, y: 1 }, ArrowDown: { x: 0, y: -1 } };
    const d = arrows[e.key];
    if (!d || !isZoomed(current.current)) return false;
    // Further left shows more of the left: the photo moves right, by a sixth of its space.
    return change(false, (z, g) => ({ ...z, x: z.x + (d.x * g.box.w) / 6, y: z.y + (d.y * g.box.h) / 6 }));
  };

  return { zoom, moving: state.id === id && state.moving, key };
}
