import { describe, expect, it } from 'vitest';
import { isFileDrag, skipJunk, type PathFile } from '../src/drop';

describe('isFileDrag', () => {
  it('knows file drags in Chrome, Safari and Firefox', () => {
    expect(isFileDrag(['Files'])).toBe(true);
    expect(isFileDrag(['application/x-moz-file', 'Files'])).toBe(true);
  });

  it('leaves text and links alone', () => {
    expect(isFileDrag(['text/plain', 'text/uri-list', 'text/html'])).toBe(false);
    expect(isFileDrag([])).toBe(false);
    expect(isFileDrag(null)).toBe(false);
  });
});

describe('skipJunk', () => {
  const names = (files: PathFile[]) => skipJunk(files).map((f) => f.relativePath || f.webkitRelativePath || f.name);

  it('keeps photos, videos and documents from folders and subfolders', () => {
    const files = [
      { name: 'IMG_1.jpg', relativePath: '/Holiday/IMG_1.jpg' },
      { name: 'VID_2.mp4', relativePath: '/Holiday/Day 2/VID_2.mp4' },
      { name: 'Plan.pdf', webkitRelativePath: 'Holiday/Plan.pdf' },
    ];
    expect(names(files)).toEqual(['/Holiday/IMG_1.jpg', '/Holiday/Day 2/VID_2.mp4', 'Holiday/Plan.pdf']);
  });

  it('leaves out hidden files and what systems put in folders', () => {
    const files = [
      { name: '.DS_Store', relativePath: '/Holiday/.DS_Store' },
      { name: '._IMG_1.jpg', relativePath: '/Holiday/._IMG_1.jpg' },
      { name: 'IMG_1.jpg', relativePath: '/Holiday/.thumbnails/IMG_1.jpg' },
      { name: 'Thumbs.db', webkitRelativePath: 'Holiday/Thumbs.db' },
      { name: 'THUMBS.DB', webkitRelativePath: 'Holiday/THUMBS.DB' },
      { name: 'desktop.ini', webkitRelativePath: 'Holiday/desktop.ini' },
      { name: 'Icon\r', relativePath: '/Holiday/Icon\r' },
      { name: 'IMG_1.jpg@SynoEAStream', relativePath: '/Holiday/@eaDir/IMG_1.jpg/IMG_1.jpg@SynoEAStream' },
      { name: '._IMG_1.jpg', relativePath: '/__MACOSX/Holiday/._IMG_1.jpg' },
      { name: 'IMG_9.jpg', relativePath: '/USB/$RECYCLE.BIN/IMG_9.jpg' },
      { name: 'IndexerVolumeGuid', relativePath: '/USB/System Volume Information/IndexerVolumeGuid' },
      { name: 'IMG_2.jpg', relativePath: '/Holiday/IMG_2.jpg' },
    ];
    expect(names(files)).toEqual(['/Holiday/IMG_2.jpg']);
  });

  it('keeps files dropped or picked one by one, whatever their name', () => {
    const files = [
      { name: '.env.example', relativePath: null },
      { name: 'Thumbs.db' },
      { name: 'desktop.ini', relativePath: '/desktop.ini' },
    ];
    expect(skipJunk(files)).toHaveLength(3);
  });
});
