import { afterAll } from 'vitest';

import { collectChromiumGarbage } from './chromium-gc';

// The theme, compiled by Tailwind through the Vite plugin, so a component
// under test has the same utilities it has in a page. Without it a container
// query test measures nothing.
import '../styles/index.css';

// A setup file runs once for each test file, and this collects Chromium's
// garbage on every tenth call (chromium-gc.ts).
afterAll(collectChromiumGarbage);
