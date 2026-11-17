import { playwright } from "@vitest/browser-playwright";
import { defineConfig, mergeConfig } from "vitest/config";

import viteConfig from "./vite.config.ts";

// One project, in a real Chromium through Playwright, on the configuration the
// library's own component tests use (svelte-testing.md). The stylesheet is the
// setup file, so the primitive under test carries the utilities and the theme
// it carries in the page.
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      include: ["src/**/*.svelte.test.ts"],
      setupFiles: ["./src/app.css"],
      browser: {
        enabled: true,
        headless: true,
        provider: playwright(),
        instances: [{ browser: "chromium" }],
        screenshotFailures: false,
      },
    },
  }),
);
