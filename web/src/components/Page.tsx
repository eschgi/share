import type { ComponentChildren } from 'preact';
import { Brand } from './Brand';

interface Props {
  name: string;
  languageSwitch?: boolean;
  /**
   * How the screen uses a tablet's or a computer's space. 'single': one column in a card in the
   * middle. 'split': the same card on tablets, two panes side by side on computers. 'sending':
   * a wider card on tablets, the progress beside the tiles on computers. (Also class names.)
   */
  layout?: 'single' | 'split' | 'sending';
  children: ComponentChildren;
}

/**
 * A screen: the header, then the content. On a phone it is one column filling the screen, as
 * the content's wrappers take no room there; styles.css lays them out on bigger screens.
 */
export function Page({ name, languageSwitch, layout = 'single', children }: Props) {
  return (
    <main class={`screen ${layout}`}>
      <Brand name={name} languageSwitch={languageSwitch} />
      <div class="body">{children}</div>
    </main>
  );
}
