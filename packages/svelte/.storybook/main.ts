// The Storybook this library's components are looked at in.
//
// One story file sits beside each component, written in Svelte itself
// (`*.stories.svelte`) through `@storybook/addon-svelte-csf`, because a story
// for a component built from snippets cannot be written in plain TypeScript.
//
// This is a plain Vite package and not a SvelteKit one, so the framework is
// `@storybook/svelte-vite`. Storybook 10 is the first release whose peer range
// accepts Vite 8 and `@sveltejs/vite-plugin-svelte` 7, which this package
// already carries.
//
// vite.config.ts is read for its plugins, Tailwind and Svelte. Its `build`
// block describes the library: one chunk per subpath, with Svelte, bits-ui and
// the SDK left external for a consumer to supply. Storybook drops that whole
// block before it builds a page, so nothing here has to undo it.
import { existsSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import type { StorybookConfig } from '@storybook/svelte-vite';

const here = path.dirname(fileURLToPath(import.meta.url));

// The printer silhouettes `PrinterIcon` draws. They are served by the
// application and are not part of this package, so the folder is mounted when
// it is there and left out when it is not, rather than failing the build.
const artwork = path.resolve(here, '../../../frontend/public/img');

// The props table under each story is read out of the component by svelte2tsx.
// The dev server does that one file at a time, as a story is opened, so the
// table is there while somebody is reading it. A build does it for every
// component at once, which measured 5 minutes 37 against 10 seconds without
// it, so the gate builds the pages and leaves the tables out.
const building = process.argv.includes('build');

const config: StorybookConfig = {
  // A self-hosted open-source tool must not phone home on every build.
  core: { disableTelemetry: true },
  stories: ['../src/**/*.stories.svelte'],
  addons: ['@storybook/addon-svelte-csf'],
  framework: {
    name: '@storybook/svelte-vite',
    options: { docgen: !building },
  },
  staticDirs: existsSync(artwork) ? [{ from: artwork, to: '/img' }] : [],
};

export default config;
