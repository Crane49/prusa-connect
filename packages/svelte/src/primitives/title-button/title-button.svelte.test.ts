import ChevronsUpDownIcon from '@lucide/svelte/icons/chevrons-up-down';
import { createRawSnippet } from 'svelte';
import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import TitleButton from './title-button.svelte';

const props = {
  icon: ChevronsUpDownIcon,
  label: 'Workshop, switch dashboard',
  children: createRawSnippet(() => ({ render: () => '<span>Workshop</span>' })),
};

describe('TitleButton', () => {
  it('is named by its label and passes rest props through', async () => {
    const { container } = await render(TitleButton, {
      props: { ...props, title: 'Workshop', 'data-part': 'switcher' },
    });

    const button = page.getByRole('button', {
      name: 'Workshop, switch dashboard',
    });
    await expect.element(button).toHaveAttribute('title', 'Workshop');
    await expect.element(button).toHaveAttribute('data-part', 'switcher');
    await expectNoViolations(container);
  });
});
