// The stacked shape names an installed module with a link to its detail page.
import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import type { ResourceInstance } from '@prusa/connect-ui/data';

import type { ModuleCatalogue } from '../../stores/module-catalogue.svelte';
import type { ExtensionRow } from '../rows';
import ExtensionsListing from './extensions-listing.svelte';

const rowOf = (installed: boolean): ExtensionRow =>
  ({
    key: installed ? 'github.com/acme/spools' : 'github.com/acme/other',
    module: installed ? 'github.com/acme/spools' : 'github.com/acme/other',
    namespace: installed ? 'com.github.aux.acme.spools' : undefined,
    title: installed ? 'Spools' : 'Other',
    summary: '',
    author: '',
    source: 'local',
    state: installed ? 'installed' : 'available',
    version: '1.0.0',
    platforms: [],
    permissions: [],
    pages: [],
    instance: installed
      ? ({
          kind: 'asset',
          name: 'spools',
          namespace: 'com.github.aux.acme.spools',
          enabled: true,
          source: 'overlay',
          module: { asked: {} },
          status: { state: 'ready' },
        } as unknown as ResourceInstance)
      : undefined,
  }) as unknown as ExtensionRow;

const catalogue = {
  mayConfigure: () => true,
  acting: undefined,
  writing: undefined,
} as unknown as ModuleCatalogue;

describe('the stacked extensions listing', () => {
  it('links an installed row title to its detail page and leaves an available one plain', async () => {
    await render(ExtensionsListing, {
      props: {
        rows: [rowOf(true), rowOf(false)],
        view: 'rows',
        catalogue,
        shows: () => true,
        go: vi.fn(),
        ask: vi.fn(),
      },
    });

    await expect
      .element(page.getByRole('link', { name: 'Spools' }))
      .toHaveAttribute(
        'href',
        expect.stringContaining('com.github.aux.acme.spools'),
      );
    await expect
      .element(page.getByRole('link', { name: 'Other' }))
      .not.toBeInTheDocument();
  });
});
