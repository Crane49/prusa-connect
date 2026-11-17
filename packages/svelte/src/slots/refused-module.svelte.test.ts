// The installed fixture under test/modules/library-mismatch, read here as the
// files a release carries rather than as a listing written by hand. Both proof
// extensions under examples/ declare a range this library satisfies, so every
// other test of the refusal builds its own `ui` block and none of them reads a
// manifest an operator could install.
//
// scripts/check-example-spools.sh runs this file after it installs that
// fixture and finds its range in the resources listing, so the two halves meet:
// the script proves the range survives the install, and this proves the app
// refuses it and stops before the import.
import { describe, expect, it } from 'vitest';

import { createFleet } from '../data/fleet.svelte';
import { fakeFleetClient, fakeLoaderClient } from '../test/fakes';
import { LIBRARY_VERSION } from '../version';
import { createModuleLoader } from './loader.svelte';
import { SlotRegistry } from './registry.svelte';

import manifest from '../../../../test/modules/library-mismatch/module.yaml?raw';
import entrySource from '../../../../test/modules/library-mismatch/ui/index.js?raw';

const NAMESPACE = 'com.example.library-mismatch';

/**
 * The `library` value out of the fixture's manifest. The manifest is the one
 * place the range is written, so a fixture edited to a range this version
 * satisfies fails here instead of passing quietly.
 */
const declaredRange = (): string => {
  const found = /^\s+library:\s*"(?<range>[^"]+)"/mu.exec(manifest);
  if (!found?.groups?.range) {
    throw new Error('the fixture manifest declares no range');
  }

  return found.groups.range;
};

/** The fixture's own entry bytes, at a URL a real `import()` can reach. */
const entryUrl = (): string =>
  URL.createObjectURL(new Blob([entrySource], { type: 'text/javascript' }));

describe('a module whose library range excludes this app', () => {
  // The tripwire itself. Everything below reads "the entry did not run", which
  // proves nothing unless running it is loud.
  it('has an entry that throws when it is imported', async () => {
    await expect(import(/* @vite-ignore */ entryUrl())).rejects.toThrow(
      /was imported/u,
    );
  });

  // ADR-20260911-4p6 rule 24 and ADR-20260914-244 rule 7. No `importModule` and no `version`
  // here: the loader gets its real dynamic import and this library's real
  // version, so an admission would evaluate the entry above.
  it('is refused for its range, and its entry is never imported', async () => {
    const range = declaredRange();
    const registry = new SlotRegistry();
    const loader = createModuleLoader({
      client: fakeLoaderClient([
        {
          namespace: NAMESPACE,
          ui: { entry_url: entryUrl(), library: range, contributes: {} },
        },
      ]),
      registry,
      fleet: createFleet(fakeFleetClient([]).client),
    });

    await loader.activate();

    expect(loader.refusals).toEqual([
      {
        namespace: NAMESPACE,
        reason: 'library-mismatch',
        range,
        version: LIBRARY_VERSION,
      },
    ]);
    expect(loader.loaded).toEqual([]);
    expect(registry.sections('device-card')).toHaveLength(0);
    expect(loader.ready).toBe(true);
  });
});
