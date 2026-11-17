// Two gates on the mermaid diagrams, both failing and neither warning.
//
// 1. Every diagram in the built site carries the hook the renderer looks for,
//    and no built page still holds a mermaid code block that nothing renders.
//    Without this, a change to the markdown pipeline puts the diagrams back to
//    plain code blocks and the build stays green.
// 2. Every mermaid block in a page under src/content/docs parses. A diagram
//    that does not parse reaches the reader as an error box, and only the
//    parser can tell.
//
// Usage: node docs/scripts/check-mermaid.mjs [--report]
// --report also parses the mermaid blocks in the markdown outside the site
// (the decision records) and prints what fails there without failing.

import { readdir, readFile } from 'node:fs/promises';
import { relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { JSDOM } from 'jsdom';

// The parser sanitises diagram text through DOMPurify, which needs a document.
// The window has to be global before mermaid is imported, because the library
// reads it while its modules evaluate.
const page = new JSDOM('<!doctype html><html><body></body></html>');

globalThis.window = page.window;
globalThis.document = page.window.document;
globalThis.Element = page.window.Element;
globalThis.Node = page.window.Node;
globalThis.DOMParser = page.window.DOMParser;

const { default: mermaid } = await import('mermaid');

const root = fileURLToPath(new URL('..', import.meta.url));
const dist = resolve(root, 'dist');
const content = resolve(root, 'src/content/docs');

const FENCE = /^([ \t]*)```+[ \t]*mermaid[ \t]*$/;

/** Every file under `dir` whose name ends in one of `extensions`. */
async function filesUnder(dir, extensions) {
  const found = [];

  for (const entry of await readdir(dir, { withFileTypes: true, recursive: true })) {
    if (!entry.isFile()) continue;
    if (!extensions.some((extension) => entry.name.endsWith(extension))) continue;

    found.push(resolve(entry.parentPath, entry.name));
  }

  return found.sort();
}

/**
 * The mermaid blocks in a markdown file, with the line the fence opens on.
 *
 * The fence is matched on its own, without a markdown parser, because the
 * check has to name a line number in the source file.
 */
function blocksIn(body) {
  const lines = body.split(/\r?\n/);
  const blocks = [];

  for (let index = 0; index < lines.length; index++) {
    const open = FENCE.exec(lines[index]);

    if (!open) continue;

    const indent = open[1];
    const source = [];

    let end = index + 1;

    while (end < lines.length && lines[end].trim() !== '```') {
      source.push(lines[end].startsWith(indent) ? lines[end].slice(indent.length) : lines[end]);
      end++;
    }

    blocks.push({ line: index + 1, source: source.join('\n') });
    index = end;
  }

  return blocks;
}

/** The blocks of `files` that the mermaid parser refuses. */
async function parseFailures(files) {
  const failures = [];

  for (const file of files) {
    const body = await readFile(file, 'utf8');

    for (const block of blocksIn(body)) {
      try {
        await mermaid.parse(block.source);
      } catch (error) {
        const message = String(error && error.message ? error.message : error).split('\n')[0];

        failures.push({ file: relative(root, file), line: block.line, message });
      }
    }
  }

  return failures;
}

async function main() {
  const report = process.argv.includes('--report');
  const problems = [];

  // The built pages. `mermaid-figure` is the class the rehype plugin in
  // astro.config.mjs puts on every diagram and the client script reads.
  const pages = await filesUnder(dist, ['.html']);
  let figures = 0;

  for (const page of pages) {
    const html = await readFile(page, 'utf8');

    const emitted = html.split('class="mermaid-figure"').length - 1;
    const sources = html.split('class="mermaid-source"').length - 1;

    figures += emitted;

    if (/data-language="mermaid"|language-mermaid/.test(html)) {
      problems.push(`${relative(root, page)} still holds a mermaid code block nothing renders`);
    }

    // Expressive Code claims a `pre` whose one child is a `code`, and a figure
    // whose source it has claimed carries its markup instead of the class the
    // renderer reads.
    if (emitted !== sources) {
      problems.push(`${relative(root, page)} holds ${emitted} figure(s) and ${sources} diagram source(s)`);
    }
  }

  const sources = await filesUnder(content, ['.md', '.mdx']);
  let expected = 0;

  for (const source of sources) {
    expected += blocksIn(await readFile(source, 'utf8')).length;
  }

  if (figures !== expected) {
    problems.push(`the content holds ${expected} diagram(s) and the build emitted ${figures} figure(s)`);
  }

  for (const failure of await parseFailures(sources)) {
    problems.push(`${failure.file}:${failure.line} does not parse: ${failure.message}`);
  }

  if (report) {
    const outside = (await filesUnder(root, ['.md'])).filter(
      (file) => !file.startsWith(content) && !file.includes('node_modules') && !file.includes(dist),
    );

    for (const failure of await parseFailures(outside)) {
      console.log(`note: ${failure.file}:${failure.line} does not parse: ${failure.message}`);
    }
  }

  if (problems.length > 0) {
    for (const problem of problems) console.error(`FAIL docs-mermaid: ${problem}`);
    process.exit(1);
  }

  console.log(`OK docs-mermaid: ${figures} diagram(s) rendered on ${pages.length} page(s)`);
}

await main();
