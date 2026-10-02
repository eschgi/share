import { describe, expect, it } from 'vitest';
import type { FileInfo } from '../src/api';
import { extOf, filmRange, previewOf, swipeStep } from '../src/account/viewer/view';

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
