// Proves the exported tsconfig.json loads as an `extends` target and that
// its compiler options do not conflict with each other, by running the real
// `tsc` binary against a fixture that extends it.

import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { describe, it } from 'node:test';

const packageRoot = fileURLToPath(new URL('..', import.meta.url));
const tsc = fileURLToPath(
  new URL('node_modules/.bin/tsc', `file://${packageRoot}`),
);

describe('the base tsconfig', () => {
  it('type-checks a consumer that extends it', () => {
    // execFileSync throws on a non-zero exit, which is the failure this test
    // is written to catch: it prints tsc's own diagnostics in that case.
    execFileSync(
      tsc,
      ['-p', 'test/fixtures/tsconfig-consumer/tsconfig.json', '--noEmit'],
      { cwd: packageRoot, stdio: 'pipe' },
    );
  });
});
