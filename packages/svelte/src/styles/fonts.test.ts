import { execFileSync } from 'node:child_process';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { describe, expect, it } from 'vitest';

// ADR-20260915-4an rules 10, 12 and 14. This repository is open source, so a font
// whose licence forbids redistribution may not be tracked in it. `tokens.css`
// names a family and ships no file, and a private package downstream of this
// one redefines `--font-sans` and brings its own `@font-face` rules.
//
// The one font tracked here is Inter, which the SIL Open Font License allows
// anybody to redistribute.

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '../../../..');

/** Every font file in the git index, working tree ignored. */
const trackedFonts = (): string[] =>
  execFileSync('git', ['ls-files'], { cwd: repoRoot, encoding: 'utf8' })
    .split('\n')
    .filter((path) => /\.(woff2?|ttf|otf)$/i.test(path));

describe('the repository tracks one font', () => {
  // The index and not the working tree, so a file somebody downloads without
  // committing does not fail a colleague's run, and a file somebody commits
  // fails the build.
  it('tracks no font file but Inter', () => {
    expect(trackedFonts()).toEqual([
      'frontend/public/fonts/inter/InterVariable.woff2',
    ]);
  });

  it('ships no font file inside the package', () => {
    const inPackage = trackedFonts().filter((path) =>
      path.startsWith('packages/svelte/'),
    );

    expect(inPackage).toEqual([]);
  });
});
