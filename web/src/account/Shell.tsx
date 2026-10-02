import type { ComponentChildren } from 'preact';
import { Icon, type IconName } from '../components/Icon';
import { useI18n } from '../i18n';
import { Avatar, Link } from './components/Bits';
import { useAccount } from './context';

export type Tab = 'library' | 'send' | 'settings';

const tabs: { tab: Tab; icon: IconName; label: string }[] = [
  { tab: 'library', icon: 'images', label: 'nav.library' },
  { tab: 'send', icon: 'upload', label: 'nav.send' },
  { tab: 'settings', icon: 'settings', label: 'nav.settings' },
];

interface Props {
  tab: Tab;
  /** Extra things for the header on bigger screens, such as the library's search. */
  tools?: ComponentChildren;
  children: ComponentChildren;
}

/**
 * The frame of the account's pages: the app's three places. On a phone they are the app's
 * bottom bar, with each page's title bar on top; on bigger screens a header across the window.
 */
export function Shell({ tab, tools, children }: Props) {
  const { t } = useI18n();
  const { info, me } = useAccount();
  const links = (cls: string) =>
    tabs.map((x) => (
      <Link key={x.tab} href={`/${x.tab}`} class={x.tab === tab ? `${cls} on` : cls} aria-current={x.tab === tab ? 'page' : undefined}>
        <span class="pi">
          <Icon name={x.icon} />
        </span>
        {t(x.label)}
      </Link>
    ));
  return (
    <div class="shell">
      <header class="ahd">
        <Link href="/library" class="brand">
          <Icon name="images" />
          <span translate={false}>{info?.name ?? 'Share'}</span>
        </Link>
        <nav class="dtabs" aria-label={info?.name ?? 'Share'}>
          {links('dtab')}
        </nav>
        <span class="ahd-tools">{tools}</span>
        <Link href="/settings" class="ahd-me" aria-label={t('nav.settings')}>
          <Avatar id={me.user.id} name={me.user.name} size="sm" />
        </Link>
      </header>
      <main class="apage">{children}</main>
      <nav class="nav" aria-label={info?.name ?? 'Share'}>
        {links('navtab')}
      </nav>
    </div>
  );
}

/** A page's title bar on a phone: the title, and buttons on the right. Bigger screens have the
 * header instead, and show only what the page asks for (wide). */
export function TitleBar({ title, children, wide }: { title: string; children?: ComponentChildren; wide?: boolean }) {
  return (
    <header class={`ab${wide ? ' wide' : ''}`}>
      <h1>{title}</h1>
      {children}
    </header>
  );
}
