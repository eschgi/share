import type { ComponentChildren } from 'preact';
import { Icon } from './Icon';
import type { Tile as TileData } from '../uploader';

/** Renders a translation that marks a phrase with <b>…</b>, without innerHTML. */
export function Bold({ text }: { text: string }) {
  const parts = text.split(/<b>(.*?)<\/b>/);
  return <>{parts.map((p, i) => (i % 2 === 1 ? <b key={i}>{p}</b> : p))}</>;
}

/** The illustration of the orientation mockup: two photo prints and a document. */
export function Stack({ day }: { day?: boolean }) {
  // Other colours for a 24-hour PIN, as in the mockup.
  const [a, b] = day ? ['t6', 't5'] : ['t1', 't2'];
  return (
    <div class="stack" aria-hidden="true">
      <div class={`pc pa ph ${a}`}>
        <Icon name="image" />
      </div>
      <div class={`pc pb ph ${b}`}>
        <Icon name="image" />
      </div>
      <div class="pc pcen">
        <Icon name="file" />
        <span class="ext">PDF</span>
      </div>
    </div>
  );
}

const tones = ['t1', 't2', 't3', 't4', 't5', 't6', 't7', 't8', 't9', 't10'];

function tone(id: string): string {
  let h = 0;
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) | 0;
  return tones[Math.abs(h) % tones.length];
}

/** One file on the sending screen: done, sending (with its own bar), waiting, or failed. */
export function Tile({ tile }: { tile: TileData }) {
  const photoLike = tile.kind !== 'document';
  const pct = tile.size > 0 ? Math.min(100, (tile.uploaded / tile.size) * 100) : 0;
  return (
    <div class={`tile ${photoLike ? `ph ${tone(tile.id)}` : 'doc'} ${tile.state}`} title={tile.name}>
      <Icon name={tile.kind === 'photo' ? 'image' : tile.kind === 'video' ? 'play' : 'file'} class="tico" />
      {!photoLike && <span class="ext">{tile.ext || '···'}</span>}
      {tile.state === 'done' && (
        <span class="okb">
          <Icon name="check" />
        </span>
      )}
      {tile.state === 'error' && (
        <span class="errb">
          <Icon name="alert" />
        </span>
      )}
      {tile.state === 'uploading' && (
        <span class="tprog">
          <i style={{ width: `${pct}%` }} />
        </span>
      )}
    </div>
  );
}

export function Note({ icon, children }: { icon: 'smartphone' | 'monitor' | 'wifi' | 'alert'; children: ComponentChildren }) {
  return (
    <div class="note">
      <Icon name={icon} />
      <p>{children}</p>
    </div>
  );
}
