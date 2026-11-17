import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

// The two plugins a module author adds, and nothing else. Tailwind runs as a
// Vite plugin rather than through PostCSS, which is how Tailwind 4 is set up,
// and the library's theme reaches this build through src/app.css.
export default defineConfig({
  plugins: [tailwindcss(), svelte()],
});
