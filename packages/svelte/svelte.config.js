import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

export default {
  preprocess: vitePreprocess(),
  // Runes only (svelte-5.md). A legacy component fails to compile rather than
  // running in a mode this library never tests.
  compilerOptions: { runes: true },
  vitePlugin: {
    /**
     * A dependency's components are not ours to write. Storybook's Svelte CSF
     * addon still ships one component on the old API, and forcing runes on it
     * stops the Storybook build before a single story is drawn. A file under
     * node_modules compiles in the mode its author wrote it in; every file in
     * this package stays runes only.
     *
     * @param {{ filename: string }} file
     * @returns {{ runes?: boolean | undefined }}
     */
    dynamicCompileOptions: ({ filename }) =>
      filename.includes('node_modules') ? { runes: undefined } : {},
  },
};
