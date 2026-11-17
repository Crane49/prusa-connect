// Chromium keeps memory from every test file it has run, until the browser
// dies and Vitest reports VITEST_BROWSER_CONNECTION_CLOSED
// (vitest-dev/vitest#11702, https://issues.chromium.org/issues/530892387).
// Vitest collects the garbage itself only when the temp directory has less
// than 4 GB free, and a CI runner has more, so the setup file calls this
// after each file. Vitest's own collection after every file made the run
// 40 percent slower, so this collects after every tenth.
//
// A CDP session opened between files makes Vitest lose the next file's
// iframe, so this calls `gc`, which the config exposes with --expose-gc.
// The count lives on the top page, because each test file gets a new iframe.
const EVERY = 10;

export const chromiumGcArgs = ['--js-flags=--expose-gc'];

declare global {
  interface Window {
    gcCalls?: number;
  }
}

export const collectChromiumGarbage = () => {
  const top = window.top;
  if (top === null) return;
  top.gcCalls = (top.gcCalls ?? 0) + 1;
  if (top.gcCalls % EVERY !== 0) return;
  if (globalThis.gc === undefined) {
    throw new Error('Chromium runs without --expose-gc');
  }
  globalThis.gc();
};
