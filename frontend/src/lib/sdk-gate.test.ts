import { readdirSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { join, relative } from 'node:path';
import { expect, test } from 'vitest';

// The mechanical half of frontend-structure.md: this app reaches the core
// through `@prusa/connect` and through nothing else (ADR-20260909-c7x rules 2
// and 8, earlier research). eslint carries the same rule as
// `no-restricted-globals`, and a disable comment turns that off; this reads
// the files from disk, so nothing in a file can excuse the file.
//
// `EventSource` is named because it is the one that looks like it would work:
// it sets no request header, so it cannot carry a bearer token, and it reads
// no HTTP status, so a refusal at connect looks like any other error.
const REFUSED: readonly { pattern: RegExp; why: string }[] = [
  { pattern: /\bfetch\s*\(/, why: 'call the SDK, which owns the request' },
  {
    pattern: /\bEventSource\b/,
    why: "read the stream with the SDK's events()",
  },
  {
    pattern: /\bXMLHttpRequest\b/,
    why: 'call the SDK, which owns the request',
  },
  { pattern: /\bWebSocket\b/, why: 'the core publishes no socket for a page' },
  { pattern: /['"`]\/api\/v1/, why: 'the SDK owns every path the core serves' },
];

const SOURCE = new URL('../', import.meta.url);

// The app's own code. A test and the fetch stub it hands the SDK are neither a
// component, a store nor a service, and the stub exists to build the one fetch
// the transport is given, so both are outside the rule and named here.
const SKIPPED = /(\.test\.ts|^test[\\/]|^lib[\\/]sdk-gate\.test\.ts$)/;

const sources = (dir: string, prefix = ''): string[] =>
  readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const here = prefix === '' ? entry.name : join(prefix, entry.name);
    if (entry.isDirectory()) return sources(join(dir, entry.name), here);
    if (!/\.(ts|svelte)$/.test(entry.name)) return [];
    return SKIPPED.test(here) ? [] : [here];
  });

test('no page, store or service in this app reaches the core itself', () => {
  const root = fileURLToPath(SOURCE);
  const files = sources(root);

  // A gate whose input set is empty prints a tick, and that tick then says
  // "nothing was found" and "nothing was looked at" in one word. Six is what
  // this app is: the root, the entry, the client, the router, the route table
  // and the live-updates store. The screens are in the component library,
  // which carries the same rule as an eslint rule of its own.
  expect(
    files.length,
    'the gate read no source file, so this run proved nothing',
  ).toBeGreaterThanOrEqual(6);

  const hits: string[] = [];
  for (const file of files) {
    const text = readFileSync(join(root, file), 'utf8');
    text.split('\n').forEach((line, at) => {
      for (const { pattern, why } of REFUSED) {
        if (pattern.test(line)) {
          hits.push(`${file}:${String(at + 1)} ${line.trim()} (${why})`);
        }
      }
    });
  }

  expect(hits).toEqual([]);
});

test('the gate refuses what it is named for', () => {
  // A gate nobody has watched reject a violation is not a gate
  // (typescript.md). These are the lines it exists to find.
  const violations = [
    'const answer = await fetch("/api/v1/devices");',
    'const live = new EventSource("/events");',
    'const request = new XMLHttpRequest();',
    'const socket = new WebSocket("wss://core/ws");',
    "const url = '/api/v1/devices';",
  ];

  for (const line of violations) {
    expect(
      REFUSED.some(({ pattern }) => pattern.test(line)),
      line,
    ).toBe(true);
  }

  // And that it passes the lines it must not refuse.
  const allowed = [
    'const page = await client.listDevices();',
    'for await (const event of client.events()) {',
    'import { PrusaConnect } from "@prusa/connect";',
  ];

  for (const line of allowed) {
    expect(
      REFUSED.some(({ pattern }) => pattern.test(line)),
      line,
    ).toBe(false);
  }
});

test('every source the gate skips is named for a reason', () => {
  const root = fileURLToPath(SOURCE);
  const all = readdirSync(root, { recursive: true, withFileTypes: true })
    .filter((entry) => entry.isFile() && /\.(ts|svelte)$/.test(entry.name))
    .map((entry) => relative(root, join(entry.parentPath, entry.name)));

  const skipped = all.filter((file) => SKIPPED.test(file));

  // The skip list covers the tests and the test helpers, and nothing under
  // lib/ that is not a test.
  for (const file of skipped) {
    expect(
      file.endsWith('.test.ts') || file.startsWith('test'),
      `${file} is skipped and is neither a test nor a test helper`,
    ).toBe(true);
  }
});
