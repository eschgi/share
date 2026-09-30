// Empties the Go server's embed folder before a build, keeping .gitkeep (without it,
// `go build` fails when the website hasn't been built).
import { readdirSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const dist = fileURLToPath(new URL('../../server/internal/webui/dist', import.meta.url));
for (const name of readdirSync(dist)) {
  if (name !== '.gitkeep') rmSync(join(dist, name), { recursive: true, force: true });
}
