// Bundles a self-contained Swagger UI into `public/swagger/` so the static docs
// site can serve the API Reference without the Go backend.
//
// Source of truth is the retired server's committed OpenAPI 3 document, frozen
// as a capture at internal/arch/testdata/retired-openapi3.json. Nothing
// regenerates it: this script only copies it next to the Swagger UI assets from
// `swagger-ui-dist`.
//
// Output lands in public/swagger/, which Astro copies verbatim into the build
// output. With DOCS_BASE=/ the site is served at the domain root and the API
// Reference is reachable at /swagger/ (the sidebar link '/swagger/' resolves
// there because Starlight prepends `base`).

import { copyFile, mkdir, readFile, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { resolve } from 'node:path';

const require = createRequire(import.meta.url);
const root = resolve(new URL('..', import.meta.url).pathname);

const specSrc = resolve(root, '../internal/arch/testdata/retired-openapi3.json');
const outDir = resolve(root, 'public/swagger');
const swaggerUiDir = require('swagger-ui-dist').getAbsoluteFSPath();

// Assets we actually reference from index.html (skip the rest of the package).
const uiAssets = [
  'swagger-ui.css',
  'swagger-ui-bundle.js',
  'swagger-ui-standalone-preset.js',
];

const indexHtml = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Prusa Connect API Reference</title>
    <link rel="stylesheet" href="./swagger-ui.css" />
    <style>
      html { box-sizing: border-box; }
      *, *::before, *::after { box-sizing: inherit; }
      body { margin: 0; background: #fafafa; }
    </style>
  </head>
  <body>
    <div id="swagger-ui"></div>
    <script src="./swagger-ui-bundle.js" charset="UTF-8"></script>
    <script src="./swagger-ui-standalone-preset.js" charset="UTF-8"></script>
    <script>
      window.ui = SwaggerUIBundle({
        url: './openapi.json',
        dom_id: '#swagger-ui',
        deepLinking: true,
        presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
        layout: 'StandaloneLayout',
      });
    </script>
  </body>
</html>
`;

async function main() {
  // Fail loudly if the spec is missing, which beats shipping a broken page.
  await readFile(specSrc);

  await mkdir(outDir, { recursive: true });

  for (const asset of uiAssets) {
    await copyFile(resolve(swaggerUiDir, asset), resolve(outDir, asset));
  }

  await copyFile(specSrc, resolve(outDir, 'openapi.json'));
  await writeFile(resolve(outDir, 'index.html'), indexHtml);

  console.log(`✓ Swagger UI bundled into ${outDir}`);
}

main().catch((err) => {
  console.error('✗ build-swagger failed:', err.message);
  process.exit(1);
});
