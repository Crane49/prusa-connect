// A restricted-import zone whose path resolves to nothing reports clean, so
// each zone is checked against one import it has to refuse.
//
// The probe has to exist on disk. `no-restricted-imports` needs no type
// information, but the parser's project service still resolves every linted
// file against the tsconfig's file list before any rule sees it, and a file
// `lintText` invents is not on that list.
import { rm, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { ESLint } from 'eslint';
import { afterEach, describe, expect, it } from 'vitest';

const ROOT = fileURLToPath(new URL('../..', import.meta.url));

// The project service reads the whole tsconfig file list on the first lint,
// which takes longer than a test's default patience on a cold cache. One
// instance for all three probes pays that once.
const eslint = new ESLint({ cwd: ROOT });

const LINT_TIMEOUT_MS = 60_000;

let probePath: string | undefined;

afterEach(async () => {
  if (probePath === undefined) return;
  await rm(join(ROOT, probePath));
  probePath = undefined;
});

const lintProbe = async (path: string, code: string) => {
  probePath = path;
  await writeFile(join(ROOT, path), code);
  const [result] = await eslint.lintFiles([path]);
  return result?.messages ?? [];
};

describe('the tiers of the library', () => {
  it(
    'refuses an import that goes up from the router',
    async () => {
      const messages = await lintProbe(
        'src/router/__tiers_probe__.ts',
        "import { Button } from '../primitives/button';\n",
      );

      expect(
        messages.some((message) => message.ruleId === 'no-restricted-imports'),
      ).toBe(true);
    },
    LINT_TIMEOUT_MS,
  );

  it(
    'refuses an import of the application from the shell',
    async () => {
      const messages = await lintProbe(
        'src/shell/__tiers_probe__.ts',
        "import { client } from '../../../frontend/src/lib/client';\n",
      );

      expect(
        messages.some((message) => message.ruleId === 'no-restricted-imports'),
      ).toBe(true);
    },
    LINT_TIMEOUT_MS,
  );

  it(
    'refuses the same import from a component nested one level deeper',
    async () => {
      // src/shell/rail/rail.svelte sits one folder below src/shell/probe.ts,
      // so the path to the application carries one more `../`. A pattern
      // written for a single depth would miss this file and every other
      // component folder in src/shell/.
      const messages = await lintProbe(
        'src/shell/app-shell/__tiers_probe__.ts',
        "import { client } from '../../../../frontend/src/lib/client';\n",
      );

      expect(
        messages.some((message) => message.ruleId === 'no-restricted-imports'),
      ).toBe(true);
    },
    LINT_TIMEOUT_MS,
  );
});
