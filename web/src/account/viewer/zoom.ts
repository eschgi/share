// Zooming into a photo in the viewer: the arithmetic, apart from the page, for the tests.
// Points are in CSS pixels from the middle of the space the photo has, which is also the
// middle of the photo at rest.

/** How far in and where: the photo is scaled around its middle, then moved by x and y. */
export interface Zoom {
  scale: number;
  x: number;
  y: number;
}

export interface Point {
  x: number;
  y: number;
}

export interface Size {
  w: number;
  h: number;
}

export const noZoom: Zoom = { scale: 1, x: 0, y: 0 };

/** As far in as the app goes. */
export const maxScale = 6;

/** Where a double tap or double click goes. */
export const doubleScale = 2.5;

export function isZoomed(z: Zoom): boolean {
  return z.scale > 1.01;
}

/**
 * Keeps a zoom possible: between 1 and maxScale, and no further to a side than the photo
 * reaches, so no gap opens between its edge and the edge of its space. pic is the photo's size
 * at rest, box the space it has.
 */
export function clamp(z: Zoom, pic: Size, box: Size): Zoom {
  const scale = clampScale(z.scale);
  const reach = (picLen: number, boxLen: number) => Math.max(0, (picLen * scale - boxLen) / 2);
  const within = (v: number, r: number) => Math.min(r, Math.max(-r, v)) || 0; // not -0
  return { scale, x: within(z.x, reach(pic.w, box.w)), y: within(z.y, reach(pic.h, box.h)) };
}

export function clampScale(scale: number): number {
  return Math.min(maxScale, Math.max(1, scale));
}

/** Zooms to scale, keeping what is under p where it is. */
export function zoomAt(z: Zoom, scale: number, p: Point): Zoom {
  const k = scale / z.scale;
  return { scale, x: p.x - k * (p.x - z.x), y: p.y - k * (p.y - z.y) };
}

/**
 * Two fingers: they started d0 apart around m0 at zoom z0 and are now d apart around m. The
 * photo grows with their distance, and what was between them stays between them.
 */
export function pinch(z0: Zoom, d0: number, m0: Point, d: number, m: Point): Zoom {
  const scale = clampScale(z0.scale * (d / Math.max(d0, 1)));
  const k = scale / z0.scale;
  return { scale, x: m.x - k * (m0.x - z0.x), y: m.y - k * (m0.y - z0.y) };
}

/** A double tap or click: in to doubleScale at p, or back out. */
export function toggle(z: Zoom, p: Point): Zoom {
  return isZoomed(z) ? noZoom : zoomAt(noZoom, doubleScale, p);
}

/**
 * How much one wheel event zooms: a mouse wheel's notch (100 pixels) about 1.25 times, and a
 * trackpad's pinch, which browsers send as a wheel with ctrlKey in small steps, smoothly.
 * deltaMode is the WheelEvent's: 0 pixels, 1 lines, 2 pages.
 */
export function wheelFactor(deltaY: number, deltaMode: number, ctrlKey: boolean): number {
  const px = deltaMode === 1 ? deltaY * 16 : deltaMode === 2 ? deltaY * 400 : deltaY;
  return Math.exp(-px * (ctrlKey ? 0.01 : 0.0022));
}

/** A tap that comes soon enough after the last one, close enough to it, makes a double tap. */
export interface Tap {
  at: number;
  x: number;
  y: number;
}

export function isDoubleTap(last: Tap | null, tap: Tap): boolean {
  return last !== null && tap.at - last.at < 300 && Math.hypot(tap.x - last.x, tap.y - last.y) < 30;
}
