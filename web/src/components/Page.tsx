import { createContext, type ComponentChildren } from 'preact';
import { useContext } from 'preact/hooks';
import { Brand } from './Brand';
import { Icon, type IconName } from './Icon';

/** Inside the frame of the account's pages (account/Shell.tsx), which has its own header. */
export const InShell = createContext(false);

/** The places screens sit between, such as a PIN's Send and See (screen 46): as for the
 * account's pages, tabs in the header on bigger screens and the app's bar at the bottom on a
 * phone. */
export interface Places {
  label: string;
  items: { key: string; icon: IconName; label: string }[];
  on: string;
  choose: (key: string) => void;
}

export const PagePlaces = createContext<Places | null>(null);

function PlaceTabs({ places, cls }: { places: Places; cls: 'dtab' | 'navtab' }) {
  return (
    <>
      {places.items.map((p) => (
        <button
          key={p.key}
          type="button"
          role="tab"
          aria-selected={p.key === places.on}
          class={p.key === places.on ? `${cls} on` : cls}
          onClick={() => places.choose(p.key)}
        >
          <span class="pi">
            <Icon name={p.icon} />
          </span>
          {p.label}
        </button>
      ))}
    </>
  );
}

interface Props {
  name: string;
  languageSwitch?: boolean;
  /**
   * How the screen uses a tablet's or a computer's space. 'single': one column in a card in the
   * middle. 'split': the same card on tablets, two panes side by side on computers. 'sending':
   * a wider card on tablets, the progress beside the tiles on computers. (Also class names.)
   */
  layout?: 'single' | 'split' | 'sending' | 'see';
  children: ComponentChildren;
}

/**
 * A screen: the header, then the content. On a phone it is one column filling the screen, as
 * the content's wrappers take no room there; styles.css lays them out on bigger screens.
 */
export function Page({ name, languageSwitch, layout = 'single', children }: Props) {
  const places = useContext(PagePlaces);
  if (useContext(InShell)) {
    return (
      <div class={`screen in-shell ${layout}`}>
        <div class="body">{children}</div>
      </div>
    );
  }
  return (
    <main class={`screen ${layout}${places ? ' placed' : ''}`}>
      <Brand name={name} languageSwitch={languageSwitch}>
        {places && (
          <div class="dtabs" role="tablist" aria-label={places.label}>
            <PlaceTabs places={places} cls="dtab" />
          </div>
        )}
      </Brand>
      <div class="body">{children}</div>
      {places && (
        <div class="nav" role="tablist" aria-label={places.label}>
          <PlaceTabs places={places} cls="navtab" />
        </div>
      )}
    </main>
  );
}
