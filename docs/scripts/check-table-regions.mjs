// Every table in the built site sits inside a labelled, focusable scroll
// region. ADR-20260826-swm rule 10.
//
// The rule this guards is one line of CSS away from coming back: Starlight
// scrolls a wide table by putting `display: block; overflow: auto` on the table
// itself, which leaves the scroll container unreachable by keyboard and takes
// the table's own box away with it. `rehypeTableScrollRegion` in
// astro.config.mjs moves the overflow onto a wrapper, and this is what notices
// if that wrapper stops being emitted.
//
//   node docs/scripts/check-table-regions.mjs [dist directory]
//
// Reads the built HTML, so it needs `astro build` to have run.

import { readdir, readFile } from 'node:fs/promises';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const dist = process.argv[2] ?? fileURLToPath(new URL('../dist', import.meta.url));

async function htmlFiles(directory) {
  const found = [];

  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);

    if (entry.isDirectory()) found.push(...(await htmlFiles(path)));
    else if (entry.name.endsWith('.html')) found.push(path);
  }

  return found;
}

const files = await htmlFiles(dist);

if (files.length === 0) {
  console.error(`✗ no built pages under ${dist}. Run astro build first.`);
  process.exit(1);
}

// openingTagLength returns how much of a string is the opening tag it starts
// with, counting the closing `>` and ignoring one inside a quoted value.
function openingTagLength(markup) {
  let quoted = false;

  for (let index = 0; index < markup.length; index++) {
    if (markup[index] === '"') quoted = !quoted;
    else if (markup[index] === '>' && !quoted) return index + 1;
  }

  return markup.length;
}

const failures = [];
let tables = 0;
let regions = 0;

for (const file of files) {
  const html = await readFile(file, 'utf8');
  const where = relative(dist, file);

  // The wrapper is the element immediately before the table, so the markup
  // between them is the whole of what has to be checked.
  for (const match of html.matchAll(/<table[\s>]/g)) {
    tables++;

    const opening = html.lastIndexOf('<div class="table-scroll"', match.index);
    const between = opening === -1 ? null : html.slice(opening, match.index);

    if (between === null || between.includes('</div>') || /<table[\s>]/.test(between.slice(1))) {
      failures.push(`${where}: a table is not inside a table-scroll region`);

      continue;
    }

    regions++;

    // A label carries whatever the heading said, and a heading on this site
    // says `Printer -> Host`. Stop at the first `>` outside a quoted value.
    const wrapper = between.slice(0, openingTagLength(between));

    for (const required of ['role="region"', 'tabindex="0"', 'aria-label="']) {
      if (!wrapper.includes(required)) {
        failures.push(`${where}: a table-scroll region is missing ${required}: ${wrapper}`);
      }
    }
  }
}

if (failures.length > 0) {
  console.error(`✗ ${failures.length} table(s) in ${files.length} built pages break ADR-20260826-swm rule 10:`);
  for (const failure of failures.slice(0, 20)) console.error(`  ${failure}`);
  process.exit(1);
}

if (tables === 0) {
  console.error(`✗ ${files.length} built pages carry no table at all, so this check proves nothing`);
  process.exit(1);
}

// The wrapper alone is half the fix. Without the stylesheet the table keeps
// Starlight's `display: block`, and a table that is not a table box is what
// the wrapper exists to make unnecessary.
const stylesheets = (await readdir(join(dist, '_astro')))
  .filter((name) => name.endsWith('.css'))
  .map((name) => join(dist, '_astro', name));

const css = (await Promise.all(stylesheets.map((path) => readFile(path, 'utf8')))).join('\n');

for (const required of ['display:table', '.table-scroll', 'overflow-x:auto']) {
  if (!css.includes(required)) {
    console.error(`✗ no built stylesheet carries ${required}, so the tables keep Starlight's display:block`);
    process.exit(1);
  }
}

console.log(`✓ ${regions} of ${tables} tables across ${files.length} built pages scroll inside a labelled region`);
