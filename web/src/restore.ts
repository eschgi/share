// Helpers for bringing back an upload queue after the page closed. Golden Retriever saves the
// files and the batches Uppy was running; these fill the gaps it leaves.

export interface GhostLike {
  id: string;
  name: string;
  size: number;
  type: string;
}

export interface PickedLike {
  name: string;
  size: number;
  type: string;
}

/**
 * Pairs picked files with ghosts, the files whose data the browser didn't keep. Android's
 * picker can give a re-picked file a new modification time, which changes Uppy's file id, so
 * files match by name, size and type instead. The ghost's id stays, and with it the tus
 * upload, so the file continues where it stopped. Picked files that match no ghost come back
 * in rest.
 */
export function matchGhosts<F extends PickedLike>(ghosts: GhostLike[], picked: F[]): { matched: [string, F][]; rest: F[] } {
  const open = [...ghosts];
  const matched: [string, F][] = [];
  const rest: F[] = [];
  for (const f of picked) {
    // Some pickers leave the type empty; name and size are enough then.
    const i = open.findIndex((g) => g.name === f.name && g.size === f.size && (g.type === f.type || !g.type || !f.type));
    if (i < 0) {
      rest.push(f);
      continue;
    }
    matched.push([open[i].id, f]);
    open.splice(i, 1);
  }
  return { matched, rest };
}

export interface Batch {
  fileIDs: string[];
}

/**
 * Keeps each file only in the newest batch that lists it. A file retried while its first
 * batch still ran is in both; restoring both would start two uploads of it at once.
 */
export function dedupeBatches<B extends Batch>(batches: Record<string, B>): Record<string, B> {
  const seen = new Set<string>();
  const kept: [string, B][] = [];
  for (const [id, b] of Object.entries(batches).reverse()) {
    const fileIDs = b.fileIDs.filter((f) => !seen.has(f));
    for (const f of fileIDs) seen.add(f);
    if (fileIDs.length > 0) kept.push([id, { ...b, fileIDs }]);
  }
  return Object.fromEntries(kept.reverse());
}

export interface FileLike {
  id: string;
  isGhost?: boolean;
  progress: { uploadStarted?: number | null; uploadComplete?: boolean };
}

/**
 * Files that started, aren't finished, and are in no batch: those that failed before the
 * page closed. Restoring won't run them, so they have to be started again.
 */
export function unbatched(files: FileLike[], batches: Record<string, Batch>): string[] {
  const inBatch = new Set(Object.values(batches).flatMap((b) => b.fileIDs));
  return files
    .filter((f) => !f.isGhost && !f.progress.uploadComplete && f.progress.uploadStarted && !inBatch.has(f.id))
    .map((f) => f.id);
}
