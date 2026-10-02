import { describe, expect, it } from 'vitest';
import type { FileInfo } from '../src/api';
import { extOf, filmRange, previewOf, swipeStep } from '../src/account/viewer/view';
import { clamp, doubleScale, isDoubleTap, isZoomed, maxScale, noZoom, pinch, toggle, wheelFactor, zoomAt } from '../src/account/viewer/zoom';

const f = (kind: FileInfo['kind'], mime: string) => ({ kind, mime }) as FileInfo;

describe('previewOf', () => {
  it('shows photos and plays videos and sound', () => {
    expect(previewOf(f('photo', 'image/heic'))).toBe('image');
    expect(previewOf(f('video', 'video/quicktime'))).toBe('video');
    expect(previewOf(f('document', 'audio/mpeg'))).toBe('audio');
  });
  it('shows no other documents', () => {
    expect(previewOf(f('document', 'application/pdf'))).toBe('none');
    expect(previewOf(f('document', 'text/html'))).toBe('none');
  });
});

describe('extOf', () => {
  it('is the extension in capitals, as on the app\'s tiles', () => {
    expect(extOf('Car_insurance.pdf')).toBe('PDF');
    expect(extOf('Recipes.Oma.docx')).toBe('DOCX');
    expect(extOf('README')).toBe('');
    expect(extOf('.hidden')).toBe('');
    expect(extOf('archive.toolongext')).toBe('');
  });
});

describe('swipeStep', () => {
  it('goes on with a swipe to the left, back with one to the right', () => {
    expect(swipeStep(-120, 10)).toBe(1);
    expect(swipeStep(90, -20)).toBe(-1);
  });
  it('ignores taps, short moves and scrolling', () => {
    expect(swipeStep(0, 0)).toBe(0);
    expect(swipeStep(-30, 0)).toBe(0);
    expect(swipeStep(-80, 70)).toBe(0);
  });
});

describe('filmRange', () => {
  it('shows two on each side, fewer at the ends', () => {
    expect(filmRange(5, 10)).toEqual([3, 7]);
    expect(filmRange(0, 10)).toEqual([0, 2]);
    expect(filmRange(9, 10)).toEqual([7, 9]);
    expect(filmRange(0, 1)).toEqual([0, 0]);
  });
});

describe('zoom', () => {
  const pic = { w: 400, h: 300 };
  const box = { w: 400, h: 600 };

  it('stays between the whole photo and six times', () => {
    expect(clamp({ scale: 0.5, x: 0, y: 0 }, pic, box)).toEqual(noZoom);
    expect(clamp({ scale: 9, x: 0, y: 0 }, pic, box).scale).toBe(maxScale);
  });
  it('moves no further than the photo reaches', () => {
    // Twice as big: 800 × 600, in a 400 × 600 space; 200 pixels to either side, none up or down.
    expect(clamp({ scale: 2, x: 500, y: 80 }, pic, box)).toEqual({ scale: 2, x: 200, y: 0 });
    expect(clamp({ scale: 2, x: -500, y: -80 }, pic, box)).toEqual({ scale: 2, x: -200, y: 0 });
    expect(clamp({ scale: 2, x: 120, y: 0 }, pic, box)).toEqual({ scale: 2, x: 120, y: 0 });
  });
  it('keeps what is under the pointer there', () => {
    const p = { x: 100, y: -50 };
    const z = zoomAt(noZoom, 2, p);
    // The photo's point under p at rest is p itself; zoomed, it must be at p again.
    expect(z.x + 2 * p.x).toBeCloseTo(p.x);
    expect(z.y + 2 * p.y).toBeCloseTo(p.y);
    const back = zoomAt(z, 1, p);
    expect(back.x).toBeCloseTo(0);
    expect(back.y).toBeCloseTo(0);
  });
  it('pinches with the fingers', () => {
    // Fingers 100 apart around the middle move to 300 apart: three times.
    expect(pinch(noZoom, 100, { x: 0, y: 0 }, 300, { x: 0, y: 0 })).toEqual({ scale: 3, x: 0, y: 0 });
    // Moving both fingers 40 to the right moves the photo with them.
    const z = pinch(noZoom, 100, { x: 50, y: 0 }, 200, { x: 90, y: 0 });
    expect(z.scale).toBe(2);
    expect(z.x + 2 * 50).toBeCloseTo(90);
    // No further than six times, nor out past the whole photo.
    expect(pinch(noZoom, 10, { x: 0, y: 0 }, 1000, { x: 0, y: 0 }).scale).toBe(maxScale);
    expect(pinch({ scale: 2, x: 0, y: 0 }, 200, { x: 0, y: 0 }, 20, { x: 0, y: 0 }).scale).toBe(1);
  });
  it('goes in and out with a double tap or click', () => {
    const z = toggle(noZoom, { x: 40, y: 20 });
    expect(z.scale).toBe(doubleScale);
    expect(isZoomed(z)).toBe(true);
    expect(toggle(z, { x: 0, y: 0 })).toEqual(noZoom);
  });
  it('zooms a notch of the wheel about a quarter, trackpad pinches gently', () => {
    expect(wheelFactor(-100, 0, false)).toBeCloseTo(1.25, 1);
    expect(wheelFactor(100, 0, false)).toBeCloseTo(0.8, 1);
    expect(wheelFactor(-3, 1, false)).toBeCloseTo(wheelFactor(-48, 0, false));
    expect(wheelFactor(-4, 0, true)).toBeGreaterThan(1);
    expect(wheelFactor(-4, 0, true)).toBeLessThan(1.05);
  });
  it('counts two quick taps close together as a double tap', () => {
    const first = { at: 1000, x: 100, y: 100 };
    expect(isDoubleTap(null, first)).toBe(false);
    expect(isDoubleTap(first, { at: 1200, x: 110, y: 95 })).toBe(true);
    expect(isDoubleTap(first, { at: 1400, x: 100, y: 100 })).toBe(false);
    expect(isDoubleTap(first, { at: 1200, x: 180, y: 100 })).toBe(false);
  });
});
