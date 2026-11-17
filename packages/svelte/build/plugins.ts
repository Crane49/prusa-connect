import { svelte, vitePreprocess } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import type { PluginOption } from 'vite';

export interface ModulePluginOptions {
  /**
   * Compiles components for hot replacement. `dev` stays off either way: a
   * module runs on the host's Svelte, and the host ships a production build.
   */
  hmr: boolean;
}

/** How a module's source is compiled, in its build and in its dev server. */
export const modulePlugins = ({ hmr }: ModulePluginOptions): PluginOption[] => [
  svelte({
    // svelte-5.md asks for `<script lang="ts">`, and this is what strips the
    // types before the Svelte compiler reads the block.
    preprocess: vitePreprocess(),
    // The bundle is one file, so a component's `<style>` is compiled into
    // it rather than emitted beside it.
    emitCss: false,
    compilerOptions: { runes: true, dev: false, hmr },
  }),
  // The module's own Tailwind. The stylesheet the module's entry imports
  // references Tailwind's theme and the library's `tokens.css`, neither of
  // which emits a byte, and runs `@tailwind utilities`, the only statement
  // that does. So the bundle
  // carries the utilities this module names and never the preflight, which
  // would restyle the host page
  // (ADR-20260914-244 rules 11 and 12).
  tailwindcss(),
];
