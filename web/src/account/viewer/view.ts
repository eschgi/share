// The viewer's decisions, apart from the page, for the tests.
import type { FileInfo } from '../../api';

/** How the viewer shows a file: as a picture, playing, or not at all (download it instead). */
export type Preview = 'image' | 'video' | 'audio' | 'none';

export function previewOf(f: FileInfo): Preview {
  if (f.kind === 'photo') return 'image';
  if (f.kind === 'video') return 'video';
  if (f.mime.startsWith('audio/')) return 'audio';
  return 'none';
}

/** "PDF" for report.pdf, as on the app's tiles; empty without an extension. */
export function extOf(name: string): string {
  const dot = name.lastIndexOf('.');
  return dot > 0 && name.length - dot <= 6 ? name.slice(dot + 1).toUpperCase() : '';
}

/**
 * Which way a swipe goes: 1 to the next file (finger to the left), -1 to the previous one, 0 for
 * no swipe. It has to be mostly sideways and long enough, so scrolling and taps don't count.
 */
export function swipeStep(dx: number, dy: number): -1 | 0 | 1 {
  if (Math.abs(dx) < 50 || Math.abs(dx) < Math.abs(dy) * 1.5) return 0;
  return dx < 0 ? 1 : -1;
}

/** The film strip: up to two files on each side of the one shown, as in the app. */
export function filmRange(index: number, count: number): [number, number] {
  return [Math.max(0, index - 2), Math.min(count - 1, index + 2)];
}
