import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import PlainPage from '../../test/plain-page.svelte';
import { SlotRegistry } from '../registry.svelte';
import type { ModuleContributes } from '../types';
import ModulePage from './module-page.svelte';

const contributes: ModuleContributes = {
  pages: [{ path: 'spools', title: 'Spools', hidden: false }],
};

describe('ModulePage', () => {
  it('renders the page the namespace and path name', async () => {
    const registry = new SlotRegistry();
    registry.declare('spools', contributes);
    registry.registerPage('spools', 'spools', PlainPage);

    await render(ModulePage, {
      props: { namespace: 'spools', path: 'spools', registry },
    });

    await expect.element(page.getByTestId('plain-page')).toBeInTheDocument();
  });

  it('renders nothing of its own when no module claims the route', async () => {
    const registry = new SlotRegistry();

    const screen = await render(ModulePage, {
      props: { namespace: 'spools', path: 'spools', registry },
    });

    await expect
      .element(page.getByTestId('plain-page'))
      .not.toBeInTheDocument();
    expect(screen.container.querySelector('[data-slot="page"]')).toBeNull();
  });
});
