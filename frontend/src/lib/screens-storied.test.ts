// Every screen the app routes to has a story beside it, with a story for each
// state the app draws while its data loads or fails (ADR-20261023-r3x). A screen
// missing from the Storybook is missing from the design.
import { existsSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { expect, test } from 'vitest';

import { SCREENS } from './screens';

// Matching by identity rather than by name keeps the screens a feature flag
// leaves out of `SCREENS` out of this check too.
const sources = import.meta.glob<unknown>(
  ['./screens/*.svelte', '!./screens/*.stories.svelte'],
  {
    eager: true,
    import: 'default',
  },
);

// They only send the browser to the address that replaced theirs.
const REDIRECTS = [
  './screens/old-file-address.svelte',
  './screens/old-network-address.svelte',
  './screens/old-people-address.svelte',
  './screens/server-address.svelte',
];

const RULE = '.agents/rules/storybook.md';

// Every screen reads data, so every screen has these states.
const STATES = ['Loading', 'Failed', 'Refused'];

// The screens whose page is a list of records, which can come back empty.
const LISTINGS = [
  './screens/credentials.svelte',
  './screens/extensions.svelte',
  './screens/jobs.svelte',
  './screens/people.svelte',
  './screens/printers.svelte',
  './screens/storage.svelte',
];

// A story file names a state it cannot show on a line of its own, in its top
// comment: `Omitted Refused: every signed-in person may read this page.`
const OMITTED = /^\s*Omitted (\w+): \S/gm;

const beside = (source: string): string =>
  fileURLToPath(
    new URL(source.replace(/\.svelte$/, '.stories.svelte'), import.meta.url),
  );

const files = [...new Set(SCREENS.map((route) => route.component))].map(
  (component) =>
    Object.entries(sources).find(([, found]) => found === component)?.[0],
);
const screens = files.filter(
  (file): file is string => file !== undefined && !REDIRECTS.includes(file),
);

test('every routed screen has a story file beside it', () => {
  const missing = screens
    .filter((file) => !existsSync(beside(file)))
    .map(
      (file) =>
        `${file}: add ${file.replace(/^\.\/screens\/|\.svelte$/g, '')}.stories.svelte beside it (see ${RULE})`,
    );

  expect(files).not.toContain(undefined);
  expect(missing).toEqual([]);
});

test('every screen story file shows the loading, failed and refused states', () => {
  const missing = screens
    .filter((file) => existsSync(beside(file)))
    .flatMap((file) => {
      const text = readFileSync(beside(file), 'utf8');
      const named = new Set(
        [...text.matchAll(/<Story\s+name="([^"]+)"/g)].map((m) => m[1]),
      );
      const omitted = new Set([...text.matchAll(OMITTED)].map((m) => m[1]));
      const wanted = LISTINGS.includes(file) ? [...STATES, 'Empty'] : STATES;
      return wanted
        .filter((state) => !named.has(state) && !omitted.has(state))
        .map(
          (state) =>
            `${beside(file).replace(/^.*\/src\//, 'src/')}: add <Story name="${state}">, or a line "Omitted ${state}: <reason>" in its top comment (see ${RULE})`,
        );
    });

  expect(missing).toEqual([]);
});

test('the sign-in and first-run states have a story file beside app.svelte', () => {
  expect(
    existsSync(
      fileURLToPath(new URL('../app.stories.svelte', import.meta.url)),
    ),
  ).toBe(true);
});
