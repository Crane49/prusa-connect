// Prepares the static deploy directory (`out/`) from the Astro build, for any
// static host that serves a directory at a domain root.
//
// The docs are built with base '/docs', which is the base every hardcoded link
// in the content assumes. It is this site's own base on this site's own host,
// and no build of the server serves this site: `/docs` there is the Scalar API
// reference. Astro emits the files at the dist ROOT but references them under
// /docs/*, so serving `dist` directly at a domain root would 404 on every
// /docs/* URL.
//
// This script nests the whole build under `out/docs/` so that /docs/* resolves,
// and drops a root `out/index.html` that redirects the bare domain to /docs/.
// Result at the served root:
//   /              → meta-refresh → /docs/
//   /docs/         → docs home
//   /docs/swagger/ → bundled API reference

import { cp, mkdir, rm, writeFile, copyFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const root = resolve(new URL('..', import.meta.url).pathname);
const distDir = resolve(root, 'dist');
const outDir = resolve(root, 'out');

const redirectHtml = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta http-equiv="refresh" content="0; url=/docs/" />
    <link rel="canonical" href="/docs/" />
    <title>Prusa Connect Documentation</title>
  </head>
  <body>
    <p>Redirecting to <a href="/docs/">/docs/</a>…</p>
  </body>
</html>
`;

async function main() {
  await rm(outDir, { recursive: true, force: true });
  await mkdir(resolve(outDir, 'docs'), { recursive: true });

  // Nest the entire build under /docs so its /docs/* URLs resolve.
  await cp(distDir, resolve(outDir, 'docs'), { recursive: true });

  // Bare-domain redirect to the docs home.
  await writeFile(resolve(outDir, 'index.html'), redirectHtml);

  // Also expose the 404 page at the served root, for a host that serves a
  // root 404.html for a missing path. Its /docs/* asset links still resolve
  // because the assets live under out/docs/.
  await copyFile(resolve(distDir, '404.html'), resolve(outDir, '404.html')).catch(() => {});

  console.log(`✓ Static deploy dir ready at ${outDir} (served under /docs, root → /docs/)`);
}

main().catch((err) => {
  console.error('✗ postbuild-static failed:', err.message);
  process.exit(1);
});
