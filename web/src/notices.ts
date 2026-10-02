// What a notification says when sending or saving ends while the page is in the background.
// Pure, for the tests: the words come from outside.

export type Notice =
  | { kind: 'sent'; files: number; bytes: number }
  /** Nothing is on its way any more, and these files didn't go. */
  | { kind: 'failed'; failed: number }
  /** The PIN ended in the middle, or the browser lost it. */
  | { kind: 'pinEnded' }
  | { kind: 'pinLost' }
  /** This browser was signed out while sending or saving. */
  | { kind: 'signedOut' }
  | { kind: 'saved'; saved: number; folder: string }
  | { kind: 'saveStopped'; why: 'full' | 'folder' | 'signedOut' };

export interface Words {
  t: (key: string, params?: Record<string, string | number>) => string;
  tn: (key: string, n: number, params?: Record<string, string | number>) => string;
  bytes: (n: number) => string;
}

export interface NoticeText {
  title: string;
  body: string;
  /** A newer notice of the same tag replaces the older one: sending and saving have one each. */
  tag: 'send' | 'save';
}

export function noticeText(n: Notice, w: Words): NoticeText {
  switch (n.kind) {
    case 'sent':
      return { tag: 'send', title: w.t('notice.sent'), body: w.tn('notice.sentBody', n.files, { size: w.bytes(n.bytes) }) };
    case 'failed':
      return { tag: 'send', title: w.t('notice.notAll'), body: `${w.tn('sending.failed', n.failed)} ${w.t('notice.tryAgain')}` };
    case 'pinEnded':
      return { tag: 'send', title: w.t('notice.stopped'), body: w.t('pin.sessionEnded') };
    case 'pinLost':
      return { tag: 'send', title: w.t('notice.stopped'), body: w.t('pin.sessionLost') };
    case 'signedOut':
      return { tag: 'send', title: w.t('notice.stopped'), body: w.t('signIn.signedOut') };
    case 'saved':
      return { tag: 'save', title: w.tn('save.done', n.saved), body: w.t('save.into', { folder: n.folder }) };
    case 'saveStopped': {
      const why = n.why === 'full' ? 'save.full' : n.why === 'signedOut' ? 'signIn.signedOut' : 'save.folderGone';
      return { tag: 'save', title: w.t('save.stopped'), body: w.t(why) };
    }
  }
}
