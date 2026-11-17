import { afterAll } from 'vitest';

import { collectChromiumGarbage } from '../../../packages/svelte/src/test/chromium-gc';

// The theme, compiled by Tailwind through the Vite plugin, so a component
// under test carries the same utilities it carries in a page. Without it a
// container query test measures nothing.
import '../styles/global.css';

// This application's own catalogue, read in the way `main.ts` reads it. A page
// whose catalogue is not registered draws every `app.` key as itself, and
// `translate()` records the miss, which is a write the template is not allowed
// to make (frontend-i18n.md rule 1).
import { startLocale } from '../lib/i18n';

startLocale(['en']);

// A setup file runs once for each test file, and this collects Chromium's
// garbage on every tenth call (packages/svelte/src/test/chromium-gc.ts).
afterAll(collectChromiumGarbage);
