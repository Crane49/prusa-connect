import { fileURLToPath } from 'node:url';

import { build } from 'vite';
import { describe, expect, it } from 'vitest';

import { moduleBuild } from './module';
import { SHARED_SPECIFIERS } from './specifiers';

const FIXTURE = fileURLToPath(new URL('./testdata/module/', import.meta.url));
const FIXTURE_CONFIG = fileURLToPath(
  new URL('./testdata/module/vite.config.ts', import.meta.url),
);

/**
 * The four things this test reads off a built chunk. Vite's own return type
 * comes from `rolldown`, which pnpm does not link into this package, so the
 * bundle crosses into the test as `unknown` and is narrowed here
 * (typescript.md).
 */
interface BuiltChunk {
  fileName: string;
  code: string;
  imports: string[];
  moduleIds: string[];
}

const isStringArray = (value: unknown): value is string[] =>
  Array.isArray(value) && value.every((one) => typeof one === 'string');

const asChunk = (value: unknown): BuiltChunk | undefined => {
  if (typeof value !== 'object' || value === null) return undefined;
  const { type, fileName, code, imports, moduleIds } = value as Record<
    string,
    unknown
  >;
  if (type !== 'chunk') return undefined;
  if (typeof fileName !== 'string' || typeof code !== 'string') {
    return undefined;
  }
  if (!isStringArray(imports) || !isStringArray(moduleIds)) return undefined;

  return { fileName, code, imports, moduleIds };
};

/** Builds the fixture the way its own `vite.config.ts` does, into memory. */
const bundleFixture = async (): Promise<BuiltChunk> => {
  const result: unknown = await build({
    root: FIXTURE,
    configFile: FIXTURE_CONFIG,
    logLevel: 'silent',
    build: { write: false },
  });

  // Vite answers one bundle per build environment, and this config has one.
  for (const bundle of Array.isArray(result) ? result : [result]) {
    if (typeof bundle !== 'object' || bundle === null) continue;
    const { output } = bundle as { output?: unknown };
    if (!Array.isArray(output)) continue;

    const chunk = asChunk(output[0]);
    if (chunk) return chunk;
  }

  throw new Error('the fixture build answered no entry chunk');
};

/**
 * The code with every import statement removed. What is left is the bundle's
 * own text, so a `svelte/internal` in it is carried code and not a specifier
 * the browser resolves against the map.
 */
const withoutImports = (code: string): string =>
  code.replace(
    /(^|[;\n])\s*import\s*(?:[^'"]*?from\s*)?["'][^"']+["']\s*;?/g,
    '$1',
  );

describe('moduleBuild', () => {
  it('marks every specifier the import map answers external', () => {
    const config = moduleBuild({ entry: 'src/index.ts' });

    expect(config.build?.rolldownOptions?.external).toEqual([
      ...SHARED_SPECIFIERS,
    ]);
  });

  it('names the output file the manifest can point at', () => {
    const config = moduleBuild({ entry: 'src/index.ts', fileName: 'ui.js' });
    const lib = config.build?.lib;
    const named = lib !== false && lib !== undefined ? lib.fileName : undefined;

    expect(typeof named === 'function' ? named('es', 'index') : named).toBe(
      'ui.js',
    );
  });
});

describe('a module built with it', () => {
  it('carries no Svelte and imports the runtime bare', async () => {
    const chunk = await bundleFixture();

    expect(chunk.fileName).toBe('index.js');
    expect(chunk.imports).toContain('svelte/internal/client');
    expect(chunk.imports).toContain('@prusa/connect-ui/slots');
    expect(
      chunk.imports.filter((one) => !SHARED_SPECIFIERS.includes(one)),
    ).toEqual([]);

    // Nothing of Svelte's own source is in the output, on either reading: no
    // module of the package, and no `svelte/internal` text outside an import.
    expect(
      chunk.moduleIds.filter((id) => /node_modules[\\/]svelte[\\/]/.test(id)),
    ).toEqual([]);
    expect(withoutImports(chunk.code)).not.toContain('svelte/internal');
  }, 60_000);

  it('stays a bundle the manifest can serve as one small file', async () => {
    const chunk = await bundleFixture();

    // A bundle that grew past this carries something the app already ships.
    // Earlier research measured the same shape at 1.7 kB.
    expect(Buffer.byteLength(chunk.code)).toBeLessThan(4096);
  }, 60_000);

  it('carries the utilities it names, and no preflight', async () => {
    const chunk = await bundleFixture();

    // `gap-x-7` is named nowhere else in this repository, so the app's own
    // Tailwind build never emitted a rule for it. Finding one here says the
    // module built its own (ADR-20260914-244 rule 10), and finding it in the entry
    // says the bytes travel in the one file the manifest names (rule 13).
    expect(chunk.code).toContain('.gap-x-7{column-gap:');

    // `bg-state-idle` is the library's own, and it compiles only because the
    // stylesheet references `tokens.css`. So the library's theme, its
    // utilities and its `dark` variant are in scope (ADR-20260914-244 rule 12).
    expect(chunk.code).toContain('.bg-state-idle{background-color:');

    // The preflight is the trap (ADR-20260914-244 rule 11). It resets the host page's
    // margins, borders and box model, and a module has no business doing that.
    expect(chunk.code).not.toContain('@layer base');
    expect(chunk.code).not.toContain('box-sizing:border-box');
  }, 60_000);

  it('scopes its rules to the markup inside a module slot', async () => {
    const chunk = await bundleFixture();

    // A module's `.flex` in the host's own `utilities` layer, later in the
    // page, beats every host `md:grid` it meets (ADR-20260914-244 rule 14).
    expect(chunk.code).toMatch(
      /@scope \(\[data-module\]\)\{[^]*@layer utilities\{[^]*\.flex\{/,
    );

    // A browser drops `@property` inside `@scope`, and the translate would
    // then have no initial value to animate from.
    expect(chunk.code).toContain('@property --tw-translate-x{');
    expect(chunk.code.lastIndexOf('@property')).toBeLessThan(
      chunk.code.indexOf('@scope'),
    );
  }, 60_000);

  // ADR-20261029-zzg: served under the host's address of a module, the
  // bundle narrows its styles to that module's markup. Served from anywhere
  // else, it keeps the scope of any module.
  it('narrows its rules to the module the host serves it as', async () => {
    const chunk = await bundleFixture();
    const injection = chunk.code.slice(0, chunk.code.indexOf('\n}\n') + 3);
    const styleAt = (url: string) => {
      const appended: { id?: string; textContent?: string }[] = [];
      const fakeDocument = {
        getElementById: () => null,
        createElement: () => ({}),
        head: {
          append: (element: { id?: string; textContent?: string }) => {
            appended.push(element);
          },
        },
      };
      // The bundle's own first statement, run against a fake page.
      // eslint-disable-next-line @typescript-eslint/no-implied-eval
      const run = new Function(
        'document',
        injection.replace('import.meta.url', JSON.stringify(url)),
      ) as (page: typeof fakeDocument) => void;
      run(fakeDocument);

      return appended[0];
    };

    const own = styleAt(
      'http://printers.local/api/v1/modules/com.example.spools/ui/index.js',
    );
    expect(own?.textContent).toContain(
      '@scope ([data-module="com.example.spools"]) to ([data-module]:not([data-module="com.example.spools"]))',
    );
    expect(own?.id).toMatch(/-com\.example\.spools$/);

    const elsewhere = styleAt('http://localhost:5173/src/index.ts');
    expect(elsewhere?.textContent).toContain('@scope ([data-module]){');
  }, 60_000);

  it('refuses a build that carries Svelte itself', async () => {
    const config = moduleBuild({ entry: 'src/index.ts' });

    await expect(
      build({
        ...config,
        root: FIXTURE,
        configFile: false,
        logLevel: 'silent',
        build: {
          ...config.build,
          write: false,
          // Everything but the two @prusa packages is bundled now, so the
          // Svelte runtime lands in the output and the guard has to say so.
          rolldownOptions: { external: [/^@prusa\//] },
        },
      }),
    ).rejects.toThrow(/two runtimes/);
  }, 60_000);
});
